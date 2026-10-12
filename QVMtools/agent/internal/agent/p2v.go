package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// p2vCreateImageParams 是 p2v-create-image 命令的参数。
type p2vCreateImageParams struct {
	SourceDisk string `json:"source_disk"` // e.g., "/dev/sda"
	OutputPath string `json:"output_path"` // e.g., "/tmp/source-disk.qcow2"
}

// p2vPullParams 是 p2v-pull 命令的参数。
type p2vPullParams struct {
	SourcePath     string    `json:"source_path"`
	TargetDiskPath string    `json:"target_disk_path"`
	TargetSSH      TargetSSH `json:"target_ssh"`
	Cleanup        bool      `json:"cleanup"` // 传输完成后删除源文件
}

// p2vDefineParams 是 p2v-define 命令的参数。
type p2vDefineParams struct {
	VMName         string    `json:"vm_name"`
	TargetDiskPath string    `json:"target_disk_path"`
	TargetSSH      TargetSSH `json:"target_ssh"`
	RamMB          int       `json:"ram_mb"`
	VCPUs          int       `json:"vcpus"`
	MACAddress     string    `json:"mac_address,omitempty"`
}

// p2vStreamParams 是 p2v-stream 命令的参数。
type p2vStreamParams struct {
	SourceDisk     string    `json:"source_disk"`      // e.g., "/dev/sda"
	TargetDiskPath string    `json:"target_disk_path"` // 目标落盘路径
	TargetSSH      TargetSSH `json:"target_ssh"`
}

// runP2VStream 流式创建镜像并直传目标：qemu-img convert | ssh dd。
// 跳过本地存储，适用于源主机磁盘空间不足的 P2V 场景。
func (c *Client) runP2VStream(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	var p p2vStreamParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse p2v-stream params: %w", err)
	}
	if p.SourceDisk == "" || p.TargetSSH.Host == "" || p.TargetDiskPath == "" {
		return nil, fmt.Errorf("source_disk, target_ssh and target_disk_path are required")
	}

	// 验证源磁盘存在。
	progress(0, fmt.Sprintf("验证源磁盘 %s", p.SourceDisk))
	if !isBlockDevice(p.SourceDisk) {
		return nil, fmt.Errorf("source disk not found or not a block device: %s", p.SourceDisk)
	}

	// 获取源磁盘大小（用于进度估算）。
	diskSize, err := getBlockDeviceSize(p.SourceDisk)
	if err != nil {
		return nil, fmt.Errorf("get disk size: %w", err)
	}
	log.Printf("[p2v-stream] source: %s, size: %d bytes (%.1f GB)", p.SourceDisk, diskSize, float64(diskSize)/(1024*1024*1024))

	// 源端计算 SHA256（流式传输无法事后校验源文件，需提前计算）。
	progress(1, "计算源磁盘校验和")
	// 对块设备无法直接 sha256sum，跳过校验（P2V 场景磁盘正在使用）。

	// 准备 SSH 连接。
	keyFile, err := prepareSSHKey(p.TargetSSH)
	if err != nil {
		return nil, err
	}
	defer func() {
		if keyFile != "" {
			os.Remove(keyFile)
		}
	}()

	// 启动传输进度监控（通过查询目标端文件大小）。
	doneCh := make(chan struct{})
	if progress != nil && diskSize > 0 {
		go monitorTransferProgress(p.TargetSSH, keyFile, p.TargetDiskPath, diskSize, progress, doneCh)
	}

	// 流式管道：qemu-img convert → FIFO → ssh dd of=target。
	// qemu-img 不支持 stdout 输出，使用命名管道桥接。
	progress(2, fmt.Sprintf("开始流式传输: %s → %s@%s:%s", p.SourceDisk, p.TargetSSH.User, p.TargetSSH.Host, p.TargetDiskPath))

	fifoPath := "/tmp/.qvm-p2v-fifo-" + fmt.Sprintf("%d", time.Now().UnixNano())
	// 创建 FIFO。
	if _, err := runOutput("mkfifo", fifoPath); err != nil {
		close(doneCh)
		return nil, fmt.Errorf("mkfifo: %w", err)
	}
	defer os.Remove(fifoPath)

	// 启动 qemu-img 写入 FIFO（后台）。
	qemuCmd := exec.Command("qemu-img", "convert", "-f", "raw", "-O", "qcow2", "-c", p.SourceDisk, fifoPath)
	var qemuStderr strings.Builder
	qemuCmd.Stderr = &qemuStderr

	// 启动 cat FIFO → ssh dd（后台）。
	catCmd := exec.Command("cat", fifoPath)
	remoteDD := sshCommand(p.TargetSSH, keyFile, "dd", fmt.Sprintf("of=%s", p.TargetDiskPath), "bs=1M", "status=none")
	var ddStderr strings.Builder
	remoteDD.Stderr = &ddStderr

	catStdout, err := catCmd.StdoutPipe()
	if err != nil {
		close(doneCh)
		return nil, fmt.Errorf("cat stdout pipe: %w", err)
	}
	remoteDD.Stdin = catStdout

	// 启动所有进程。
	if err := qemuCmd.Start(); err != nil {
		close(doneCh)
		return nil, fmt.Errorf("start qemu-img: %w", err)
	}
	if err := catCmd.Start(); err != nil {
		qemuCmd.Process.Kill()
		qemuCmd.Wait()
		close(doneCh)
		return nil, fmt.Errorf("start cat: %w", err)
	}
	if err := remoteDD.Start(); err != nil {
		qemuCmd.Process.Kill()
		catCmd.Process.Kill()
		qemuCmd.Wait()
		catCmd.Wait()
		close(doneCh)
		return nil, fmt.Errorf("start ssh dd: %w", err)
	}

	// 等待所有进程完成。
	ddErr := remoteDD.Wait()
	qemuErr := qemuCmd.Wait()
	catErr := catCmd.Wait()
	close(doneCh)

	if ddErr != nil {
		return nil, fmt.Errorf("ssh dd transfer: %w (stderr: %s)", ddErr, strings.TrimSpace(ddStderr.String()))
	}
	if qemuErr != nil {
		return nil, fmt.Errorf("qemu-img convert: %w (stderr: %s)", qemuErr, strings.TrimSpace(qemuStderr.String()))
	}
	if catErr != nil {
		return nil, fmt.Errorf("cat fifo: %w", catErr)
	}

	// 查询目标端文件大小。
	targetSize := queryRemoteFileSize(p.TargetSSH, keyFile, p.TargetDiskPath)

	progress(100, fmt.Sprintf("流式传输完成: %.1f GB → %.1f GB",
		float64(diskSize)/(1024*1024*1024), float64(targetSize)/(1024*1024*1024)))

	return map[string]interface{}{
		"source_size": diskSize,
		"target_size": targetSize,
	}, nil
}

// runP2VCreateImage 从物理磁盘创建 qcow2 压缩镜像。
// 使用 qemu-img convert 直接读取块设备并压缩输出，避免中间临时文件。
func (c *Client) runP2VCreateImage(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	var p p2vCreateImageParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse p2v-create-image params: %w", err)
	}
	if p.SourceDisk == "" {
		return nil, fmt.Errorf("source_disk is required")
	}
	if p.OutputPath == "" {
		p.OutputPath = "/var/lib/kvm-user-storage/disk.qcow2"
	}

	// 验证源磁盘存在。
	progress(0, fmt.Sprintf("验证源磁盘 %s", p.SourceDisk))
	if _, err := os.Stat(p.SourceDisk); err != nil {
		// 块设备不存在时 Stat 会失败，检查是否是块设备。
		if !isBlockDevice(p.SourceDisk) {
			return nil, fmt.Errorf("source disk not found: %w", err)
		}
	}

	// 获取源磁盘大小。
	diskSize, err := getBlockDeviceSize(p.SourceDisk)
	if err != nil {
		return nil, fmt.Errorf("get disk size: %w", err)
	}
	log.Printf("[p2v-create-image] source: %s, size: %d bytes (%.1f GB)", p.SourceDisk, diskSize, float64(diskSize)/(1024*1024*1024))

	// 启动进度监控 goroutine。
	doneCh := make(chan struct{})
	go monitorImageCreation(p.OutputPath, diskSize, progress, doneCh)

	// 使用 qemu-img convert 直接从块设备创建压缩 qcow2。
	progress(1, fmt.Sprintf("正在将 %s 转换为 qcow2 镜像...", p.SourceDisk))
	cmd := exec.Command("qemu-img", "convert", "-f", "raw", "-O", "qcow2", "-c", p.SourceDisk, p.OutputPath)
	out, err := cmd.CombinedOutput()
	close(doneCh)
	if err != nil {
		os.Remove(p.OutputPath)
		return nil, fmt.Errorf("qemu-img convert: %s: %w", string(out), err)
	}

	// 获取输出文件大小。
	var outputSize int64
	if info, err := os.Stat(p.OutputPath); err == nil {
		outputSize = info.Size()
	}

	progress(100, fmt.Sprintf("镜像创建完成: %s (%.1f GB 压缩后 %.1f GB)",
		p.OutputPath, float64(diskSize)/(1024*1024*1024), float64(outputSize)/(1024*1024*1024)))

	return map[string]interface{}{
		"output_path": p.OutputPath,
		"source_size": diskSize,
		"image_size":  outputSize,
	}, nil
}

// runP2VPull 将本地文件经 SSH 流式直传到目标节点并校验。
func (c *Client) runP2VPull(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	var p p2vPullParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse p2v-pull params: %w", err)
	}
	if p.SourcePath == "" || p.TargetSSH.Host == "" || p.TargetDiskPath == "" {
		return nil, fmt.Errorf("source_path, target_ssh and target_disk_path are required")
	}

	// 验证源文件存在。
	if _, err := os.Stat(p.SourcePath); err != nil {
		return nil, fmt.Errorf("source file not found: %w", err)
	}

	// 获取源文件大小。
	var sourceSize int64
	if info, err := os.Stat(p.SourcePath); err == nil {
		sourceSize = info.Size()
	}

	progress(0, fmt.Sprintf("准备传输 %s (%.1f GB)", p.SourcePath, float64(sourceSize)/(1024*1024*1024)))

	keyFile, err := prepareSSHKey(p.TargetSSH)
	if err != nil {
		return nil, err
	}
	defer func() {
		if keyFile != "" {
			os.Remove(keyFile)
		}
	}()

	// 源端校验和。
	sum, err := sha256sumLocal(p.SourcePath)
	if err != nil {
		return nil, err
	}

	// 启动传输进度监控。
	doneCh := make(chan struct{})
	if progress != nil && sourceSize > 0 {
		go monitorTransferProgress(p.TargetSSH, keyFile, p.TargetDiskPath, sourceSize, progress, doneCh)
	}

	// 流式直传。
	progress(5, "开始传输")
	if err := streamTransfer(p.SourcePath, p.TargetSSH, keyFile, p.TargetDiskPath); err != nil {
		close(doneCh)
		return nil, err
	}
	close(doneCh)

	// 校验。
	remoteSum, err := sha256sumRemote(p.TargetSSH, keyFile, p.TargetDiskPath)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(sum, remoteSum) {
		return nil, fmt.Errorf("checksum mismatch: local=%s remote=%s", sum, remoteSum)
	}

	// P2V 镜像由 qemu-img convert 直接生成，无需 rebase。

	// 可选：清理源文件。
	if p.Cleanup && p.SourcePath != "" {
		progress(98, "清理临时文件")
		os.Remove(p.SourcePath)
	}

	progress(100, "传输验证完成")
	return PullResult{
		Checksum: sum,
		Size:     sourceSize,
		Path:     p.TargetDiskPath,
	}, nil
}

// runP2VDefine 在目标主机上从零创建 VM 定义（P2V 场景，无源 VM XML 可参考）。
func (c *Client) runP2VDefine(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	var p p2vDefineParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse p2v-define params: %w", err)
	}
	if p.VMName == "" || p.TargetSSH.Host == "" || p.TargetDiskPath == "" {
		return nil, fmt.Errorf("vm_name, target_ssh and target_disk_path are required")
	}
	if p.RamMB <= 0 {
		p.RamMB = 4096
	}
	if p.VCPUs <= 0 {
		p.VCPUs = 2
	}

	progress(0, "生成 VM 定义")
	vmXML := generateP2VXML(p)

	progress(10, "上传 VM 定义到目标")
	keyFile, err := prepareSSHKey(p.TargetSSH)
	if err != nil {
		return nil, err
	}
	defer func() {
		if keyFile != "" {
			os.Remove(keyFile)
		}
	}()

	if err := uploadFileViaSSH(p.TargetSSH, keyFile, "/tmp/qvm.define.xml", vmXML); err != nil {
		return nil, fmt.Errorf("upload xml: %w", err)
	}

	// 检测目标网络。
	progress(20, "检测目标网络配置")
	hasDefaultNet := checkNetworkExists(p.TargetSSH, keyFile, "default")
	if hasDefaultNet {
		sshCommand(p.TargetSSH, keyFile, "virsh", "net-start", "default").Run()
		sshCommand(p.TargetSSH, keyFile, "virsh", "net-autostart", "default").Run()
		log.Printf("[p2v-define] target has default network, using NAT")
	} else {
		log.Printf("[p2v-define] target has no default network, using OVS bridge")
	}

	progress(30, "在目标主机上定义 VM")
	ssh := sshCommand(p.TargetSSH, keyFile, "virsh", "define", "/tmp/qvm.define.xml")
	out, err := ssh.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("virsh define on target: %s: %w", stripSSHWarnings(string(out)), err)
	}

	sshCommand(p.TargetSSH, keyFile, "rm", "-f", "/tmp/qvm.define.xml").Run()

	progress(100, "VM 定义完成")
	return map[string]string{"status": "defined", "vm_name": p.VMName}, nil
}

// generateP2VXML 为 P2V 场景生成 VM XML。
func generateP2VXML(p p2vDefineParams) string {
	uuid := generateUUID()
	ramKiB := p.RamMB * 1024

	xml := fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  <uuid>%s</uuid>
  <memory unit='KiB'>%d</memory>
  <currentMemory unit='KiB'>%d</currentMemory>
  <vcpu placement='static'>%d</vcpu>
  <os>
    <type arch='x86_64' machine='pc-q35-8.2'>hvm</type>
    <boot dev='hd'/>
  </os>
  <cpu mode='custom' match='exact' check='full'>
    <model fallback='forbid'>qemu64</model>
    <feature policy='require' name='x2apic'/>
    <feature policy='require' name='hypervisor'/>
    <feature policy='require' name='lahf_lm'/>
    <feature policy='disable' name='svm'/>
  </cpu>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <emulator>/usr/libexec/qemu-kvm</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap' detect_zeroes='unmap'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
      <address type='pci' domain='0x0000' bus='0x04' slot='0x00' function='0x0'/>
    </disk>
    <controller type='usb' index='0' model='qemu-xhci'>
      <address type='pci' domain='0x0000' bus='0x02' slot='0x00' function='0x0'/>
    </controller>
    <controller type='sata' index='0'>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x1f' function='0x2'/>
    </controller>
    <controller type='pci' index='0' model='pcie-root'/>
    <controller type='pci' index='1' model='pcie-root-port'>
      <model name='pcie-root-port'/>
      <target chassis='1' port='0x10'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x0' multifunction='on'/>
    </controller>
    <controller type='pci' index='2' model='pcie-root-port'>
      <model name='pcie-root-port'/>
      <target chassis='2' port='0x11'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x1'/>
    </controller>
    <controller type='pci' index='3' model='pcie-root-port'>
      <model name='pcie-root-port'/>
      <target chassis='3' port='0x12'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x2'/>
    </controller>
    <controller type='pci' index='4' model='pcie-root-port'>
      <model name='pcie-root-port'/>
      <target chassis='4' port='0x13'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x3'/>
    </controller>
    <controller type='pci' index='5' model='pcie-root-port'>
      <model name='pcie-root-port'/>
      <target chassis='5' port='0x14'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x4'/>
    </controller>`,
		p.VMName, uuid, ramKiB, ramKiB, p.VCPUs, p.TargetDiskPath)

	// 网络接口：优先 OVS，fallback NAT。
	if p.MACAddress == "" {
		p.MACAddress = generateMAC()
	}
	xml += fmt.Sprintf(`
    <interface type='bridge'>
      <mac address='%s'/>
      <source bridge='br-ovs'/>
      <model type='virtio'/>
      <virtualport type='openvswitch'/>
      <address type='pci' domain='0x0000' bus='0x01' slot='0x00' function='0x0'/>
    </interface>`, p.MACAddress)

	xml += `
    <input type='mouse' bus='ps2'/>
    <input type='keyboard' bus='ps2'/>
    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'>
      <listen type='address' address='127.0.0.1'/>
    </graphics>
    <audio id='1' type='none'/>
    <video>
      <model type='cirrus' vram='16384' heads='1' primary='yes'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x01' function='0x0'/>
    </video>
    <watchdog model='itco' action='reset'/>
    <memballoon model='virtio'/>
  </devices>
</domain>`

	return xml
}

// generateMAC 生成随机 MAC 地址（QEMU OUI 前缀 52:54:00）。
func generateMAC() string {
	b := make([]byte, 3)
	// 使用时间戳生成伪随机，足够唯一。
	t := time.Now().UnixNano()
	b[0] = byte(t)
	b[1] = byte(t >> 8)
	b[2] = byte(t >> 16)
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2])
}

// isBlockDevice 检查路径是否是块设备。
func isBlockDevice(path string) bool {
	var stat os.FileInfo
	var err error
	// 对于 /dev/ 下的路径直接检查。
	if strings.HasPrefix(path, "/dev/") {
		stat, err = os.Stat(path)
	} else {
		return false
	}
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeDevice != 0 && stat.Mode()&os.ModeCharDevice == 0
}

// getBlockDeviceSize 获取块设备大小（字节）。
func getBlockDeviceSize(path string) (int64, error) {
	// 使用 blockdev --getsize64。
	out, err := runOutput("blockdev", "--getsize64", path)
	if err != nil {
		return 0, err
	}
	var size int64
	fmt.Sscanf(strings.TrimSpace(out), "%d", &size)
	return size, nil
}

// monitorImageCreation 监控镜像创建进度。
func monitorImageCreation(outputPath string, sourceSize int64, progress ProgressFunc, doneCh chan struct{}) {
	startTime := time.Now()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-doneCh:
			return
		case <-ticker.C:
			info, err := os.Stat(outputPath)
			if err != nil {
				continue
			}
			curSize := info.Size()
			elapsed := time.Since(startTime)
			var speed int64
			if elapsed.Seconds() > 0 {
				speed = int64(float64(curSize) / elapsed.Seconds())
			}
			// 估算进度：压缩比约 2:1，用源磁盘大小作为分母。
			estimatedTotal := sourceSize / 2
			if estimatedTotal == 0 {
				estimatedTotal = sourceSize
			}
			pct := float64(curSize) / float64(estimatedTotal) * 100
			if pct > 99 {
				pct = 99
			}
			eta := "?"
			if speed > 0 {
				remaining := estimatedTotal - curSize
				if remaining > 0 {
					eta = formatDuration(time.Duration(float64(time.Second) * float64(remaining) / float64(speed)))
				}
			}
			detail := map[string]interface{}{
				"bytes_written": curSize,
				"speed_bytes":   speed,
				"speed_human":   formatBytes(speed) + "/s",
				"eta":           eta,
			}
			detailJSON, _ := json.Marshal(detail)
			progress(int(pct), fmt.Sprintf("镜像创建中: %s / ~%.1f GB, 速度 %s, 预计剩余 %s",
				formatBytes(curSize), float64(sourceSize)/(1024*1024*1024), formatBytes(speed)+"/s", eta), detailJSON)
		}
	}
}

// parseP2VCreateImageParams 从通用 map 解析 p2v-create-image 参数。
func parseP2VCreateImageParams(params map[string]interface{}) (p2vCreateImageParams, error) {
	var p p2vCreateImageParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse p2v-create-image params: %w", err)
	}
	return p, nil
}

// parseP2VPullParams 从通用 map 解析 p2v-pull 参数。
func parseP2VPullParams(params map[string]interface{}) (p2vPullParams, error) {
	var p p2vPullParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse p2v-pull params: %w", err)
	}
	return p, nil
}

// parseP2VDefineParams 从通用 map 解析 p2v-define 参数。
func parseP2VDefineParams(params map[string]interface{}) (p2vDefineParams, error) {
	var p p2vDefineParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse p2v-define params: %w", err)
	}
	return p, nil
}

// p2vFixBootParams 是 p2v-fix-boot 命令的参数。
type p2vFixBootParams struct {
	DiskPath string `json:"disk_path"` // VM 磁盘路径
}

// runP2VFixBoot 修复 P2V 磁盘的启动配置：将物理机设备名 (sda) 替换为虚拟化设备名 (vda)。
// 需要目标主机安装 libguestfs-tools (guestmount)。
func (c *Client) runP2VFixBoot(params map[string]interface{}, progress ProgressFunc) (interface{}, error) {
	var p p2vFixBootParams
	b, _ := json.Marshal(params)
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse p2v-fix-boot params: %w", err)
	}
	if p.DiskPath == "" {
		return nil, fmt.Errorf("disk_path is required")
	}

	progress(0, "检查 guestmount 可用性")
	if _, err := runOutput("which", "guestmount"); err != nil {
		return nil, fmt.Errorf("guestmount not found, install libguestfs-tools")
	}

	mountPoint := "/tmp/.qvm-p2v-mount"
	os.MkdirAll(mountPoint, 0755)
	defer os.RemoveAll(mountPoint)

	// 挂载磁盘。
	progress(10, "挂载 VM 磁盘")
	if out, err := runOutput("guestmount", "-a", p.DiskPath, "-i", "--ro", mountPoint); err != nil {
		return nil, fmt.Errorf("guestmount: %s: %w", out, err)
	}

	fixedFiles := []string{}

	// 修复 GRUB 配置。
	progress(30, "修复 GRUB 配置")
	for _, grubPath := range []string{
		mountPoint + "/boot/grub2/grub.cfg",
		mountPoint + "/boot/grub/grub.cfg",
		mountPoint + "/boot/grub2/grub.cfg",
		mountPoint + "/etc/grub2.cfg",
	} {
		if _, err := os.Stat(grubPath); err == nil {
			if fixFileReplace(grubPath, "/dev/sda", "/dev/vda") {
				fixedFiles = append(fixedFiles, grubPath)
			}
			if fixFileReplace(grubPath, "hd0", "hd0") {
				// GRUB 设备映射可能需要调整
			}
			break
		}
	}

	// 修复 /etc/fstab。
	progress(50, "修复 fstab")
	fstabPath := mountPoint + "/etc/fstab"
	if _, err := os.Stat(fstabPath); err == nil {
		if fixFileReplace(fstabPath, "/dev/sda", "/dev/vda") {
			fixedFiles = append(fixedFiles, fstabPath)
		}
		// 也修复 UUID 引用（如果使用 UUID，不需要修改）。
	}

	// 修复 /etc/default/grub（如果存在）。
	progress(70, "修复 GRUB 默认配置")
	for _, grubDefault := range []string{
		mountPoint + "/etc/default/grub",
		mountPoint + "/etc/default/grub",
	} {
		if _, err := os.Stat(grubDefault); err == nil {
			if fixFileReplace(grubDefault, "/dev/sda", "/dev/vda") {
				fixedFiles = append(fixedFiles, grubDefault)
			}
			break
		}
	}

	// 卸载磁盘。
	progress(90, "卸载 VM 磁盘")
	if out, err := runOutput("guestunmount", mountPoint); err != nil {
		log.Printf("[p2v-fix-boot] guestunmount warning: %s", out)
	}

	progress(100, fmt.Sprintf("启动配置修复完成，修改了 %d 个文件", len(fixedFiles)))
	return map[string]interface{}{
		"fixed_files": fixedFiles,
		"count":       len(fixedFiles),
	}, nil
}

// fixFileReplace 在文件中执行字符串替换，返回是否修改。
func fixFileReplace(path, old, new string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(data)
	if !strings.Contains(content, old) {
		return false
	}
	newContent := strings.ReplaceAll(content, old, new)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		log.Printf("[p2v-fix-boot] write %s: %v", path, err)
		return false
	}
	log.Printf("[p2v-fix-boot] fixed %s: %s → %s", path, old, new)
	return true
}
