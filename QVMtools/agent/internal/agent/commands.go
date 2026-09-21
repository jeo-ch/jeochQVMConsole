package agent

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"strings"
)

// snapshotParams 是 snapshot 命令的参数。
type snapshotParams struct {
	VMName       string `json:"vm_name"`
	SnapshotName string `json:"snapshot_name"`
	Online       bool   `json:"online"`
}

// pullParams 是 pull 命令的参数。
type pullParams struct {
	VMName         string    `json:"vm_name"`
	DiskTarget     string    `json:"disk_target"`
	SnapshotName   string    `json:"snapshot_name"`
	TargetDiskPath string    `json:"target_disk_path"`
	TargetSSH      TargetSSH `json:"target_ssh"`
}

// defineParams 是 define 命令的参数：在目标主机上定义 VM。
type defineParams struct {
	VMName         string    `json:"vm_name"`
	TargetDiskPath string    `json:"target_disk_path"`
	TargetSSH      TargetSSH `json:"target_ssh"`
}

// resolveStorageParams 是 resolve-storage 命令的参数：查询目标主机的存储目录。
type resolveStorageParams struct {
	TargetSSH TargetSSH `json:"target_ssh"`
}

// cutoverParams 是 cutover 命令的参数。
type cutoverParams struct {
	VMName string `json:"vm_name"`
}

// cleanupParams 是 cleanup 命令的参数。
type cleanupParams struct {
	VMName       string `json:"vm_name"`
	SnapshotName string `json:"snapshot_name"`
}

// ProgressFunc 是命令执行中的进度回调类型，detail 用于携带结构化传输信息。
type ProgressFunc func(pct int, msg string, detail ...json.RawMessage)

// dispatch 按 action 派发本地命令，progress 回调用于上报进度。
func (c *Client) dispatch(action string, params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	switch action {
	case "discover":
		return c.runDiscover(progress)
	case "snapshot":
		return c.runSnapshot(params, progress)
	case "pull":
		return c.runPull(params, progress)
	case "define":
		return c.runDefine(params, progress)
	case "resolve-storage":
		return c.runResolveStorage(params, progress)
	case "cutover":
		return c.runCutover(params, progress)
	case "cleanup":
		return c.runCleanup(params, progress)
	case "p2v-create-image":
		return c.runP2VCreateImage(params, progress)
	case "p2v-pull":
		return c.runP2VPull(params, progress)
	case "p2v-define":
		return c.runP2VDefine(params, progress)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// runDiscover 枚举本机 VM 与磁盘。
func (c *Client) runDiscover(progress ProgressFunc) (interface{}, error) {
	progress(0, "enumerating virtual machines")
	vms, err := discoverVMs()
	if err != nil {
		return nil, err
	}
	progress(100, "discover complete")
	return vms, nil
}

// runSnapshot 为在线 VM 创建 disk-only 原子快照。
func (c *Client) runSnapshot(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	p, err := parseSnapshotParams(params)
	if err != nil {
		return nil, err
	}
	if p.VMName == "" || p.SnapshotName == "" {
		return nil, fmt.Errorf("vm_name and snapshot_name are required")
	}
	progress(0, "creating snapshot")
	if err := createSnapshot(p.VMName, p.SnapshotName, p.Online); err != nil {
		return nil, err
	}
	progress(100, "snapshot created")
	return nil, nil
}

// runPull 将磁盘经 SSH 直传到目标节点并校验。
func (c *Client) runPull(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	p, err := parsePullParams(params)
	if err != nil {
		return nil, err
	}
	if p.VMName == "" || p.TargetSSH.Host == "" || p.TargetDiskPath == "" {
		return nil, fmt.Errorf("vm_name, target_ssh and target_disk_path are required")
	}

	state, err := domState(p.VMName)
	if err != nil {
		return nil, err
	}

	// 快照处理：网关传入 snapshot_name 表示快照已存在；未传且 VM 运行中则自建并在完成后清理。
	snap := p.SnapshotName
	ownSnapshot := false
	if snap == "" && state == "running" {
		snap = uniqueSnapshotName(p.VMName)
		ownSnapshot = true
	}
	if ownSnapshot {
		progress(0, "creating snapshot")
		if err := createSnapshot(p.VMName, snap, true); err != nil {
			return nil, err
		}
		defer func() {
			if derr := deleteSnapshot(p.VMName, snap); derr != nil {
				log.Printf("cleanup snapshot %s failed: %v", snap, derr)
			}
		}()
	}

	// 快照后当前磁盘源指向 overlay，需重新读取 XML 定位实际传输文件。
	raw, err := dumpXML(p.VMName)
	if err != nil {
		return nil, err
	}
	var d domainXML
	if err := xml.Unmarshal([]byte(raw), &d); err != nil {
		return nil, fmt.Errorf("parse domain xml: %w", err)
	}
	disk := findDisk(parseDisks(d.Devices.Disks), p.DiskTarget)
	if disk.SourcePath == "" {
		return nil, fmt.Errorf("disk %s not found for vm %s", p.DiskTarget, p.VMName)
	}
	// 快照后源指向 overlay，需回溯其底层真实磁盘；离线 VM 的 XML 源即为真实磁盘。
	if snap != "" {
		disk.SourcePath = resolveBackingFile(disk.SourcePath)
	}
	// 填充磁盘大小（qemu-img info 获取虚拟容量）。
	if sz, err := diskSizeBytes(disk.SourcePath); err == nil {
		disk.SizeBytes = sz
	}
	disk.TargetDiskPath = p.TargetDiskPath

	progress(0, "transferring disk")
	res, err := pullDisk(disk, p.TargetSSH, progress)
	if err != nil {
		return nil, err
	}
	progress(100, "transfer verified")
	return res, nil
}

// findDisk 按目标设备名（如 vda）匹配磁盘，未指定时返回第 1 个。
func findDisk(disks []DiskInfo, target string) DiskInfo {
	if target == "" {
		if len(disks) > 0 {
			return disks[0]
		}
		return DiskInfo{}
	}
	for _, disk := range disks {
		if disk.Target == target {
			return disk
		}
	}
	return DiskInfo{}
}

// runCutover 关闭源 VM 完成切流。
func (c *Client) runCutover(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	p, err := parseCutoverParams(params)
	if err != nil {
		return nil, err
	}
	if p.VMName == "" {
		return nil, fmt.Errorf("vm_name is required")
	}
	progress(0, "shutting down vm")
	if err := shutdownVM(p.VMName); err != nil {
		return nil, err
	}
	progress(100, "cutover complete")
	return nil, nil
}

// runCleanup 删除迁移用的临时快照。
func (c *Client) runCleanup(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	p, err := parseCleanupParams(params)
	if err != nil {
		return nil, err
	}
	if p.VMName == "" || p.SnapshotName == "" {
		return nil, fmt.Errorf("vm_name and snapshot_name are required")
	}
	progress(0, "deleting snapshot")
	if err := deleteSnapshot(p.VMName, p.SnapshotName); err != nil {
		return nil, err
	}
	progress(100, "cleanup complete")
	return nil, nil
}

// runDefine 在目标主机上定义 VM：获取源 VM XML，修改磁盘路径，通过 SSH 上传并 virsh define。
func (c *Client) runDefine(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	p, err := parseDefineParams(params)
	if err != nil {
		return nil, err
	}
	if p.VMName == "" || p.TargetSSH.Host == "" || p.TargetDiskPath == "" {
		return nil, fmt.Errorf("vm_name, target_ssh and target_disk_path are required")
	}

	progress(0, "获取源 VM 定义")
	raw, err := dumpXML(p.VMName)
	if err != nil {
		return nil, err
	}

	progress(10, "生成目标 VM 定义")
	newXML := adaptXMLForTarget(raw, p.VMName, p.TargetDiskPath)

	progress(20, "上传 VM 定义到目标")
	keyFile, err := prepareSSHKey(p.TargetSSH)
	if err != nil {
		return nil, err
	}
	defer func() {
		if keyFile != "" {
			os.Remove(keyFile)
		}
	}()

	// 将 XML 写入目标主机临时文件。
	if err := uploadFileViaSSH(p.TargetSSH, keyFile, "/tmp/qvm.define.xml", newXML); err != nil {
		return nil, fmt.Errorf("upload xml: %w", err)
	}

	// 检测目标主机是否有 default 网络，据此决定网络适配策略。
	progress(25, "检测目标网络配置")
	hasDefaultNet := checkNetworkExists(p.TargetSSH, keyFile, "default")
	if hasDefaultNet {
		// 目标有 default 网络，尝试激活。
		sshCommand(p.TargetSSH, keyFile, "virsh", "net-start", "default").Run()
		sshCommand(p.TargetSSH, keyFile, "virsh", "net-autostart", "default").Run()
		newXML = adaptNetworkForTarget(newXML, true)
		log.Printf("[define] target has default network, using NAT")
	} else {
		// 目标无 default 网络，移除 default 网卡，保留 OVS 或无网卡。
		newXML = adaptNetworkForTarget(newXML, false)
		log.Printf("[define] target has no default network, removing NAT interface")
	}

	progress(30, "在目标主机上定义 VM")
	ssh := sshCommand(p.TargetSSH, keyFile, "virsh", "define", "/tmp/qvm.define.xml")
	out, err := ssh.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("virsh define on target: %s: %w", stripSSHWarnings(string(out)), err)
	}

	// 清理临时文件。
	sshCleanup := sshCommand(p.TargetSSH, keyFile, "rm", "-f", "/tmp/qvm.define.xml")
	sshCleanup.Run()

	progress(100, "VM 定义完成")
	return map[string]string{"status": "defined", "vm_name": p.VMName}, nil
}

// runResolveStorage 查询目标主机的默认 VM 存储目录。
// 通过 SSH 在目标主机执行命令获取存储池信息。
func (c *Client) runResolveStorage(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	p, err := parseResolveStorageParams(params)
	if err != nil {
		return nil, err
	}
	if p.TargetSSH.Host == "" {
		return nil, fmt.Errorf("target_ssh is required")
	}

	progress(0, "查询目标存储配置")

	keyFile, err := prepareSSHKey(p.TargetSSH)
	if err != nil {
		return nil, err
	}
	defer func() {
		if keyFile != "" {
			os.Remove(keyFile)
		}
	}()

	// 策略1: 读取 QVMConsole 配置获取 KVM_CLONE_DIR。
	// 通过 grep 直接在远程执行，避免 bash -c 引号问题。
	ssh := sshCommand(p.TargetSSH, keyFile, "grep", "KVM_CLONE_DIR", "/opt/project/QVMConsole/.env")
	out, err := ssh.Output()
	cleaned := strings.TrimSpace(stripSSHWarnings(string(out)))
	// cut -d= -f2: 提取 = 后面的值。
	cloneDir := ""
	if err == nil && cleaned != "" {
		parts := strings.SplitN(cleaned, "=", 2)
		if len(parts) == 2 {
			cloneDir = strings.TrimSpace(parts[1])
		}
	}
	if cloneDir != "" {
		log.Printf("[resolve-storage] strategy1 env: vm_dir=%q", cloneDir)
		progress(100, "storage resolved")
		return map[string]string{"vm_dir": cloneDir}, nil
	}

	// 策略2: 查询 virsh 存储池（vm-disks 池的 target/path）。
	// 先在远程执行 virsh 获取 XML，再本地提取路径。
	ssh2 := sshCommand(p.TargetSSH, keyFile, "virsh", "pool-dumpxml", "vm-disks")
	out2, err2 := ssh2.Output()
	cleaned2 := strings.TrimSpace(stripSSHWarnings(string(out2)))
	poolPath := ""
	if err2 == nil && cleaned2 != "" {
		// 从 XML 中提取 <path>xxx</path>。
		start := strings.Index(cleaned2, "<path>")
		end := strings.Index(cleaned2, "</path>")
		if start != -1 && end != -1 {
			poolPath = strings.TrimSpace(cleaned2[start+6 : end])
		}
	}
	if poolPath != "" {
		// 检查池路径下是否有 vm-disks 子目录。
		ssh3 := sshCommand(p.TargetSSH, keyFile, "ls", "-d", poolPath+"/vm-disks")
		out3, _ := ssh3.Output()
		vmDir := strings.TrimSpace(stripSSHWarnings(string(out3)))
		if vmDir != "" {
			log.Printf("[resolve-storage] strategy2 virsh: vm_dir=%q", vmDir)
			progress(100, "storage resolved")
			return map[string]string{"vm_dir": vmDir}, nil
		}
		// 没有 vm-disks 子目录，直接使用池路径。
		log.Printf("[resolve-storage] strategy2 virsh: vm_dir=%q (pool path)", poolPath)
		progress(100, "storage resolved")
		return map[string]string{"vm_dir": poolPath}, nil
	}

	// 策略3: 检查 /var/lib/kvm-storage/*/vm-disks。
	ssh4 := sshCommand(p.TargetSSH, keyFile, "ls", "-d", "/var/lib/kvm-storage/*/vm-disks")
	out4, err4 := ssh4.Output()
	vmDir4 := strings.TrimSpace(stripSSHWarnings(string(out4)))
	if err4 == nil && vmDir4 != "" {
		log.Printf("[resolve-storage] strategy3 kvm-storage: vm_dir=%q", vmDir4)
		progress(100, "storage resolved")
		return map[string]string{"vm_dir": vmDir4}, nil
	}

	// 策略4: 检查 /vm-disks。
	ssh5 := sshCommand(p.TargetSSH, keyFile, "ls", "-d", "/vm-disks")
	out5, err5 := ssh5.Output()
	vmDir5 := strings.TrimSpace(stripSSHWarnings(string(out5)))
	if err5 == nil && vmDir5 != "" {
		log.Printf("[resolve-storage] strategy4 /vm-disks: vm_dir=%q", vmDir5)
		progress(100, "storage resolved")
		return map[string]string{"vm_dir": vmDir5}, nil
	}

	// 策略5: 回退到默认路径。
	log.Printf("[resolve-storage] strategy5 default: vm_dir=/var/lib/libvirt/images")
	progress(100, "storage resolved (default)")
	return map[string]string{"vm_dir": "/var/lib/libvirt/images"}, nil
}

// adaptXMLForTarget 修改源 VM XML 以适配目标主机：
// - 磁盘路径改为目标路径
// - 生成新 UUID 和 MAC 地址
// - 移除 cdrom（ISO 不存在于目标）
// - 网络改为 default NAT（避免 OVS 依赖）
func adaptXMLForTarget(sourceXML, vmName, targetDiskPath string) string {
	// 简单字符串替换生成可用 XML，避免复杂 XML 操作。
	xml := sourceXML

	// 1. 替换磁盘源路径。
	xml = replaceDiskSource(xml, targetDiskPath)

	// 2. 生成新 UUID。
	newUUID := generateUUID()
	xml = replaceUUID(xml, newUUID)

	// 3. 移除 cdrom 设备。
	xml = removeCdrom(xml)

	// 4. 网络适配由 runDefine 根据目标主机网络情况单独处理。

	return xml
}

// replaceDiskSource 替换第一个磁盘的 source file 路径。
func replaceDiskSource(xml, newPath string) string {
	// 找到第一个 <source file='...'/> 并替换。
	start := strings.Index(xml, "<source file='")
	if start == -1 {
		start = strings.Index(xml, `<source file="`)
		if start == -1 {
			return xml
		}
		startEnd := strings.Index(xml[start:], "/>")
		if startEnd == -1 {
			return xml
		}
		full := xml[start : start+startEnd+2]
		newFull := fmt.Sprintf("<source file='%s'/>", newPath)
		return strings.Replace(xml, full, newFull, 1)
	}
	startEnd := strings.Index(xml[start:], "/>")
	if startEnd == -1 {
		return xml
	}
	full := xml[start : start+startEnd+2]
	newFull := fmt.Sprintf("<source file='%s'/>", newPath)
	return strings.Replace(xml, full, newFull, 1)
}

// replaceUUID 替换 VM UUID。
func replaceUUID(xml, newUUID string) string {
	start := strings.Index(xml, "<uuid>")
	end := strings.Index(xml, "</uuid>")
	if start == -1 || end == -1 {
		return xml
	}
	return xml[:start+6] + newUUID + xml[end:]
}

// removeCdrom 移除 cdrom 磁盘设备。
func removeCdrom(xml string) string {
	// 移除所有 <disk type='file' device='cdrom'>...</disk> 块。
	for {
		start := strings.Index(xml, "device='cdrom'")
		if start == -1 {
			break
		}
		// 向前找到 <disk
		diskStart := strings.LastIndex(xml[:start], "<disk")
		if diskStart == -1 {
			break
		}
		// 向后找到 </disk>
		diskEnd := strings.Index(xml[start:], "</disk>")
		if diskEnd == -1 {
			break
		}
		end := start + diskEnd + 7
		xml = xml[:diskStart] + xml[end:]
	}
	return xml
}

// checkNetworkExists 通过 SSH 在目标主机检查指定 libvirt 网络是否存在（含未激活）。
func checkNetworkExists(tgt TargetSSH, keyFile, networkName string) bool {
	ssh := sshCommand(tgt, keyFile, "virsh", "net-info", networkName)
	out, err := ssh.Output()
	if err != nil {
		log.Printf("[define] net-info %s: %v (output: %s)", networkName, err, strings.TrimSpace(string(out)))
		return false
	}
	return strings.Contains(string(out), "Active:")
}

// adaptNetworkForTarget 根据目标主机网络情况适配 VM 网络接口。
// hasDefault=true：将 OVS 桥接改为 default NAT；hasDefault=false：移除 default 网卡。
func adaptNetworkForTarget(xml string, hasDefault bool) string {
	if hasDefault {
		xml = strings.Replace(xml, "<interface type='bridge'>", "<interface type='network'>", 1)
		xml = strings.Replace(xml, "<source bridge='br-ovs'/>", "<source network='default'/>", 1)
		xml = removeVirtualport(xml)
		return xml
	}
	// 无 default 网络：移除引用 default 网络的接口块，保留 OVS。
	xml = removeNetworkInterface(xml, "default")
	return xml
}

// removeNetworkInterface 移除引用指定网络的 <interface type='network'> 块。
// 仅移除 <source network='networkName'/> 匹配的接口，保留其他网络接口。
func removeNetworkInterface(xml, networkName string) string {
	networkRef := fmt.Sprintf("<source network='%s'/>", networkName)
	for {
		ifaceStart := strings.Index(xml, "<interface type='network'>")
		if ifaceStart == -1 {
			break
		}
		ifaceEnd := strings.Index(xml[ifaceStart:], "</interface>")
		if ifaceEnd == -1 {
			break
		}
		ifaceEnd += ifaceStart + len("</interface>")
		ifaceBlock := xml[ifaceStart:ifaceEnd]
		if strings.Contains(ifaceBlock, networkRef) {
			xml = xml[:ifaceStart] + xml[ifaceEnd:]
		} else {
			// 跳过不匹配的块，从其后继续。
			break
		}
	}
	return xml
}

// removeVirtualport 移除所有 <virtualport> 块。
func removeVirtualport(xml string) string {
	for {
		vpStart := strings.Index(xml, "<virtualport")
		if vpStart == -1 {
			break
		}
		vpEnd := strings.Index(xml[vpStart:], "</virtualport>")
		if vpEnd == -1 {
			break
		}
		end := vpStart + vpEnd + len("</virtualport>")
		xml = xml[:vpStart] + xml[end:]
	}
	return xml
}

// generateUUID 生成一个简单的 UUID v4。
func generateUUID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(i + 1) // 简单递增，非加密安全但足够唯一。
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// uploadFileViaSSH 通过 SSH 将内容写入远程文件。
func uploadFileViaSSH(tgt TargetSSH, keyFile, remotePath, content string) error {
	// 使用 cat heredoc 写入，避免 base64 编码问题。
	cmd := fmt.Sprintf("cat > %s << 'QVMEOF'\n%s\nQVMEOF", remotePath, content)
	ssh := sshCommand(tgt, keyFile, "bash", "-c", cmd)
	out, err := ssh.CombinedOutput()
	if err != nil {
		return fmt.Errorf("upload via ssh: %s: %w", stripSSHWarnings(string(out)), err)
	}
	return nil
}

// parseDefineParams 从通用 map 解析 define 参数。
func parseDefineParams(params map[string]interface{}) (defineParams, error) {
	var p defineParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse define params: %w", err)
	}
	return p, nil
}

// parseResolveStorageParams 从通用 map 解析 resolve-storage 参数。
func parseResolveStorageParams(params map[string]interface{}) (resolveStorageParams, error) {
	var p resolveStorageParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse resolve-storage params: %w", err)
	}
	return p, nil
}

// parseSnapshotParams 从通用 map 解析 snapshot 参数。
func parseSnapshotParams(params map[string]interface{}) (snapshotParams, error) {
	var p snapshotParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse snapshot params: %w", err)
	}
	return p, nil
}

// parsePullParams 从通用 map 解析 pull 参数。
func parsePullParams(params map[string]interface{}) (pullParams, error) {
	var p pullParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse pull params: %w", err)
	}
	return p, nil
}

// parseCutoverParams 从通用 map 解析 cutover 参数。
func parseCutoverParams(params map[string]interface{}) (cutoverParams, error) {
	var p cutoverParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse cutover params: %w", err)
	}
	return p, nil
}

// parseCleanupParams 从通用 map 解析 cleanup 参数。
func parseCleanupParams(params map[string]interface{}) (cleanupParams, error) {
	var p cleanupParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse cleanup params: %w", err)
	}
	return p, nil
}
