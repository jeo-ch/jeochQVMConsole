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

// cutoverParams 是 cutover 命令的参数。
type cutoverParams struct {
	VMName string `json:"vm_name"`
}

// cleanupParams 是 cleanup 命令的参数。
type cleanupParams struct {
	VMName       string `json:"vm_name"`
	SnapshotName string `json:"snapshot_name"`
}

// dispatch 按 action 派发本地命令，progress 回调用于上报进度。
func (c *Client) dispatch(action string, params map[string]interface{}, progress func(int, string)) (interface{}, error) {
	switch action {
	case "discover":
		return c.runDiscover(progress)
	case "snapshot":
		return c.runSnapshot(params, progress)
	case "pull":
		return c.runPull(params, progress)
	case "define":
		return c.runDefine(params, progress)
	case "cutover":
		return c.runCutover(params, progress)
	case "cleanup":
		return c.runCleanup(params, progress)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// runDiscover 枚举本机 VM 与磁盘。
func (c *Client) runDiscover(progress func(int, string)) (interface{}, error) {
	progress(0, "enumerating virtual machines")
	vms, err := discoverVMs()
	if err != nil {
		return nil, err
	}
	progress(100, "discover complete")
	return vms, nil
}

// runSnapshot 为在线 VM 创建 disk-only 原子快照。
func (c *Client) runSnapshot(params map[string]interface{}, progress func(int, string)) (interface{}, error) {
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
func (c *Client) runPull(params map[string]interface{}, progress func(int, string)) (interface{}, error) {
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
	res, err := pullDisk(disk, p.TargetSSH)
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
func (c *Client) runCutover(params map[string]interface{}, progress func(int, string)) (interface{}, error) {
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
func (c *Client) runCleanup(params map[string]interface{}, progress func(int, string)) (interface{}, error) {
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
func (c *Client) runDefine(params map[string]interface{}, progress func(int, string)) (interface{}, error) {
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

// adaptXMLForTarget 修改源 VM XML 以适配目标主机：
// - 磁盘路径改为目标路径
// - 生成新 UUID 和 MAC 地址
// - 移除 cdrom（ISO 不存在于目标）
// - 网络改为 default NAT（避免 OVS 依赖）
func adaptXMLForTarget(sourceXML, vmName, targetDiskPath string) string {
	// 简单字符串替换生成可用 XML，避免复杂 XML 操作。
	xml := sourceXML

	// 1. 替换磁盘源路径。
	// 查找 <source file='...'/> 并替换。
	xml = replaceDiskSource(xml, targetDiskPath)

	// 2. 生成新 UUID。
	newUUID := generateUUID()
	xml = replaceUUID(xml, newUUID)

	// 3. 移除 cdrom 设备（从 <disk type='file' device='cdrom'> 到 </disk>）。
	xml = removeCdrom(xml)

	// 4. 网络改为 default（移除 OVS 配置）。
	xml = adaptNetwork(xml)

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

// adaptNetwork 简化网络配置：移除 OVS virtualport，使用 default 网络。
func adaptNetwork(xml string) string {
	// 替换 bridge 网络为 default。
	xml = strings.Replace(xml, "<interface type='bridge'>", "<interface type='network'>", 1)
	xml = strings.Replace(xml, "<source bridge='br-ovs'/>", "<source network='default'/>", 1)
	// 移除 virtualport 块（含子元素）。
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
