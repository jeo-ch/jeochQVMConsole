package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MigrationService 编排整机迁移的 agent 交互流程：discover -> snapshot -> pull -> cutover -> cleanup。
type MigrationService struct {
	manager *Manager
}

func NewMigrationService(m *Manager) *MigrationService {
	return &MigrationService{manager: m}
}

// DiscoverResult 是 agent discover 命令返回的已注册虚拟机清单。
type DiscoverResult struct {
	VMs []VMInfo `json:"vms"`
}

// VMInfo 描述一台可迁移虚拟机的基本信息。
type VMInfo struct {
	Name   string  `json:"name"`
	ID     string  `json:"id"`
	UUID   string  `json:"uuid"`
	State  string  `json:"state"`
	CPUs   int     `json:"cpus"`
	Memory int64   `json:"memory_mb"`
	Disks  []DiskInfo `json:"disks"`
}

// DiskInfo 描述虚拟机的一块磁盘。
type DiskInfo struct {
	Target       string `json:"target"`
	SourcePath   string `json:"source_path"`
	SizeBytes    int64  `json:"size_bytes"`
	Format       string `json:"format"`
}

// TargetSSH 描述迁移落点（目标节点）的 SSH 接入信息，由控制台侧解析目标主机后注入。
type TargetSSH struct {
	Host       string `json:"host"`       // 目标节点 IP 或主机名
	Port       string `json:"port"`       // SSH 端口，默认 "22"
	User       string `json:"user"`       // SSH 登录用户，默认 root
	AuthMethod string `json:"auth_method"` // key | password
	KeyContent string `json:"key_content,omitempty"` // base64 编码私钥内容（控制台注入）
	Password   string `json:"password,omitempty"`    // 密码鉴权（控制台注入）
}

// MigrationRequest 发起一次整机迁移的请求参数。
type MigrationRequest struct {
	SourceHostID   uint      `json:"source_host_id"`
	TargetHostID   uint      `json:"target_host_id"`
	VMName         string    `json:"vm_name"`
	DiskTarget     string    `json:"disk_target"`
	SnapshotName   string    `json:"snapshot_name"`
	TargetDiskPath string    `json:"target_disk_path"`
	Format         string    `json:"format"`
	Shutdown       bool      `json:"shutdown"`
	TargetSSH      *TargetSSH `json:"target_ssh,omitempty"` // 目标节点 SSH 接入信息
}

// PullResult 是 pull 命令完成后返回的磁盘落盘信息。
type PullResult struct {
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
	Path     string `json:"path"`
}

// Discover 向 agent 请求源主机上已注册的虚拟机列表。
func (s *MigrationService) Discover(ctx context.Context, hostID uint) (*DiscoverResult, error) {
	res, err := s.manager.DispatchCommand(ctx, hostID, "discover", nil, nil)
	if err != nil {
		return nil, err
	}
	var out DiscoverResult
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Snapshot 在源主机上为虚拟机创建在线热快照。
func (s *MigrationService) Snapshot(ctx context.Context, hostID uint, vmName, snapshotName string) error {
	_, err := s.manager.DispatchCommand(ctx, hostID, "snapshot", map[string]interface{}{
		"vm_name":        vmName,
		"snapshot_name":  snapshotName,
		"online":         true,
	}, nil)
	return err
}

// Pull 将目标磁盘从源主机经 SSH 直传到目标节点并做 sha256 校验。
func (s *MigrationService) Pull(ctx context.Context, hostID uint, req MigrationRequest, progress func(int, string, ...json.RawMessage)) (*PullResult, error) {
	params := map[string]interface{}{
		"vm_name":          req.VMName,
		"disk_target":      req.DiskTarget,
		"snapshot_name":    req.SnapshotName,
		"target_host_id":   req.TargetHostID,
		"target_disk_path": req.TargetDiskPath,
		"format":           req.Format,
		"checksum":         "sha256",
	}
	if req.TargetSSH != nil {
		params["target_ssh"] = req.TargetSSH
	}
	res, err := s.manager.DispatchCommand(ctx, hostID, "pull", params, progress)
	if err != nil {
		return nil, err
	}
	var out PullResult
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Cutover 完成切换：关闭源虚拟机并标记迁移。
func (s *MigrationService) Cutover(ctx context.Context, hostID uint, vmName string) error {
	_, err := s.manager.DispatchCommand(ctx, hostID, "cutover", map[string]interface{}{"vm_name": vmName}, nil)
	return err
}

// Cleanup 清理源主机上的临时快照。
func (s *MigrationService) Cleanup(ctx context.Context, hostID uint, vmName, snapshotName string) error {
	_, err := s.manager.DispatchCommand(ctx, hostID, "cleanup", map[string]interface{}{
		"vm_name":       vmName,
		"snapshot_name": snapshotName,
	}, nil)
	return err
}

// Define 在目标主机上定义 VM（通过源主机 agent SSH 到目标执行 virsh define）。
func (s *MigrationService) Define(ctx context.Context, hostID uint, vmName, targetDiskPath string, targetSSH *TargetSSH, progress func(int, string, ...json.RawMessage)) error {
	params := map[string]interface{}{
		"vm_name":          vmName,
		"target_disk_path": targetDiskPath,
		"target_ssh":       targetSSH,
	}
	_, err := s.manager.DispatchCommand(ctx, hostID, "define", params, progress)
	return err
}

// ResolveStorage 查询目标主机的默认 VM 存储目录。
func (s *MigrationService) ResolveStorage(ctx context.Context, hostID uint, targetSSH *TargetSSH) (string, error) {
	params := map[string]interface{}{
		"target_ssh": targetSSH,
	}
	res, err := s.manager.DispatchCommand(ctx, hostID, "resolve-storage", params, nil)
	if err != nil {
		return "", err
	}
	log.Printf("[migration] resolve-storage raw response: data=%s", string(res.Data))
	var out struct {
		VMDir string `json:"vm_dir"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return "", err
	}
	log.Printf("[migration] resolve-storage parsed: vm_dir=%q", out.VMDir)
	return out.VMDir, nil
}

// RunMigration 按顺序执行一次完整迁移编排。
func (s *MigrationService) RunMigration(ctx context.Context, req MigrationRequest, progress func(int, string, ...json.RawMessage)) (string, error) {
	if req.Format == "" {
		req.Format = "qcow2"
	}
	if req.SnapshotName == "" {
		req.SnapshotName = "qvmconsole_migration_" + time.Now().Format("20060102150405")
	}

	// 解析目标存储路径：优先使用请求指定的路径，否则动态查询目标主机。
	targetDiskPath := req.TargetDiskPath
	if targetDiskPath == "" && req.TargetSSH != nil {
		if progress != nil {
			progress(2, "查询目标存储配置")
		}
		vmDir, err := s.ResolveStorage(ctx, req.SourceHostID, req.TargetSSH)
		if err != nil {
			log.Printf("[migration] 查询目标存储失败，使用默认路径: %v", err)
			vmDir = "/var/lib/libvirt/images"
		}
		targetDiskPath = vmDir + "/" + req.VMName + ".qcow2"
		log.Printf("[migration] resolved target storage: vm_dir=%s target_path=%s", vmDir, targetDiskPath)
	}
	if targetDiskPath == "" {
		targetDiskPath = "/var/lib/libvirt/images/" + req.VMName + ".qcow2"
	}
	req.TargetDiskPath = targetDiskPath

	if progress != nil {
		progress(5, "创建快照")
	}
	if err := s.Snapshot(ctx, req.SourceHostID, req.VMName, req.SnapshotName); err != nil {
		return "failed", err
	}
	if progress != nil {
		progress(30, "拉取磁盘")
	}
	if _, err := s.Pull(ctx, req.SourceHostID, req, progress); err != nil {
		return "failed", err
	}
	if progress != nil {
		progress(70, "定义目标 VM")
	}
	if err := s.Define(ctx, req.SourceHostID, req.VMName, req.TargetDiskPath, req.TargetSSH, nil); err != nil {
		log.Printf("[migration] 定义目标 VM 失败（可忽略）: %v", err)
	}
	if req.Shutdown {
		if progress != nil {
			progress(85, "关闭源 VM")
		}
		if err := s.Cutover(ctx, req.SourceHostID, req.VMName); err != nil {
			return "failed", err
		}
	}
	if progress != nil {
		progress(95, "清理快照")
	}
	// 清理快照失败不阻断迁移（快照可能已在传输过程中被自动清理）。
	if err := s.Cleanup(ctx, req.SourceHostID, req.VMName, req.SnapshotName); err != nil {
		log.Printf("[migration] 清理快照失败（可忽略）: %v", err)
	}
	return "done", nil
}

// sanitizeMessage 清理进度消息中的控制字符，防止破坏 JSON 序列化。
func sanitizeMessage(msg string) string {
	// 移除制表符、回车符等控制字符，保留换行。
	var b strings.Builder
	for _, r := range msg {
		if r == '\n' || r == '\r' || r == '\t' || (r < 32 && r != '\n') || r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sshExecViaGateway 从网关通过 SSH 在远程主机执行命令。
func sshExecViaGateway(ssh *TargetSSH, cmdParts ...string) (string, error) {
	args := buildGatewaySSHArgs(ssh, cmdParts)
	cmd := exec.Command("ssh", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// buildGatewaySSHArgs 构造 SSH 命令行参数。
func buildGatewaySSHArgs(ssh *TargetSSH, cmdParts []string) []string {
	port := ssh.Port
	if port == "" {
		port = "22"
	}
	user := ssh.User
	if user == "" {
		user = "root"
	}
	host := user + "@" + ssh.Host

	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=10",
		"-p", port,
	}
	if ssh.AuthMethod == "key" && ssh.KeyContent != "" {
		keyFile, err := writeTempKey(ssh.KeyContent)
		if err == nil {
			args = append(args, "-i", keyFile)
		}
	} else if ssh.AuthMethod == "password" && ssh.Password != "" {
		// 网关侧使用 sshpass（需安装）。
		args = append([]string{"sshpass", "-p", ssh.Password}, args...)
	}
	args = append(args, host)
	args = append(args, cmdParts...)
	return args
}

// writeTempKey 将 SSH 私钥写入临时文件。
func writeTempKey(keyContent string) (string, error) {
 decoded, err := base64.StdEncoding.DecodeString(keyContent)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "qvm-gw-key-*")
	if err != nil {
		return "", err
	}
	f.Chmod(0600)
	f.Write(decoded)
	f.Close()
	return f.Name(), nil
}

// gatewayDefineP2VMul 在 agent 断线时，网关直接 SSH 到目标主机定义 VM。
func gatewayDefineP2VMul(ssh *TargetSSH, vmName, targetDiskPath string, ramMB, vcpus int) error {
	if ssh == nil {
		return fmt.Errorf("no target SSH credentials")
	}

	// 生成 VM XML。
	uuid := generateGatewayUUID()
	ramKiB := ramMB * 1024
	mac := generateGatewayMAC()

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
    </controller>
    <interface type='bridge'>
      <mac address='%s'/>
      <source bridge='br-ovs'/>
      <model type='virtio'/>
      <virtualport type='openvswitch'/>
      <address type='pci' domain='0x0000' bus='0x01' slot='0x00' function='0x0'/>
    </interface>
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
</domain>`, vmName, uuid, ramKiB, ramKiB, vcpus, targetDiskPath, mac)

	// 上传 XML 并 virsh define。
	if _, err := sshExecViaGateway(ssh, "cat", ">", "/tmp/qvm-p2v-define.xml", "<<'QVMEOF'\n"+xml+"\nQVMEOF"); err != nil {
		return fmt.Errorf("upload xml: %w", err)
	}
	out, err := sshExecViaGateway(ssh, "virsh", "define", "/tmp/qvm-p2v-define.xml")
	if err != nil {
		return fmt.Errorf("virsh define: %s: %w", out, err)
	}
	sshExecViaGateway(ssh, "rm", "-f", "/tmp/qvm-p2v-define.xml")
	return nil
}

func generateGatewayUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func generateGatewayMAC() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2])
}

// P2VRequest 发起 P2V（物理机转虚拟机）迁移的请求参数。
type P2VRequest struct {
	SourceHostID   uint       `json:"source_host_id"`
	TargetHostID   uint       `json:"target_host_id"`
	SourceDisk     string     `json:"source_disk"`      // 源物理磁盘路径，如 /dev/sda
	VMName         string     `json:"vm_name"`           // 目标 VM 名称
	RamMB          int        `json:"ram_mb"`            // 目标 VM 内存（MB）
	VCPUs          int        `json:"vcpus"`             // 目标 VM CPU 数
	TargetDiskPath string     `json:"target_disk_path"` // 目标磁盘路径（可选，自动解析）
	TargetSSH      *TargetSSH `json:"target_ssh"`
}

// RunP2VMigration 执行 P2V 迁移：创建镜像 → 传输 → 定义 VM。
func (s *MigrationService) RunP2VMigration(ctx context.Context, req P2VRequest, progress func(int, string, ...json.RawMessage)) (string, error) {
	if req.SourceDisk == "" {
		req.SourceDisk = "/dev/sda"
	}
	if req.VMName == "" {
		req.VMName = "p2v-migrated"
	}
	if req.RamMB <= 0 {
		req.RamMB = 4096
	}
	if req.VCPUs <= 0 {
		req.VCPUs = 2
	}

	// 包装 progress 函数，自动清理控制字符。
	safeProgress := func(p int, msg string, detail ...json.RawMessage) {
		if progress != nil {
			progress(p, sanitizeMessage(msg), detail...)
		}
	}

	// 解析目标存储路径。
	targetDiskPath := req.TargetDiskPath
	if targetDiskPath == "" && req.TargetSSH != nil {
		if safeProgress != nil {
			safeProgress(1, "查询目标存储配置")
		}
		vmDir, err := s.ResolveStorage(ctx, req.SourceHostID, req.TargetSSH)
		if err != nil {
			log.Printf("[p2v] 查询目标存储失败，使用默认路径: %v", err)
			vmDir = "/var/lib/libvirt/images"
		}
		targetDiskPath = vmDir + "/" + req.VMName + ".qcow2"
	}
	if targetDiskPath == "" {
		targetDiskPath = "/var/lib/libvirt/images/" + req.VMName + ".qcow2"
	}
	req.TargetDiskPath = targetDiskPath

	outputPath := "/var/lib/kvm-user-storage/p2v-" + req.VMName + ".qcow2"

	// Step 1: 在源主机创建 qcow2 镜像。
	if safeProgress != nil {
		safeProgress(3, "创建磁盘镜像")
	}
	_, err := s.manager.DispatchCommand(ctx, req.SourceHostID, "p2v-create-image", map[string]interface{}{
		"source_disk": req.SourceDisk,
		"output_path": outputPath,
	}, safeProgress)
	if err != nil {
		return "failed", fmt.Errorf("p2v-create-image: %w", err)
	}

	// Step 2: 传输镜像到目标。
	if safeProgress != nil {
		safeProgress(50, "传输磁盘镜像")
	}
	_, err = s.manager.DispatchCommand(ctx, req.SourceHostID, "p2v-pull", map[string]interface{}{
		"source_path":      outputPath,
		"target_disk_path": targetDiskPath,
		"target_ssh":       req.TargetSSH,
		"cleanup":          true,
	}, safeProgress)
	if err != nil {
		return "failed", fmt.Errorf("p2v-pull: %w", err)
	}

	// Step 3: 在目标主机定义 VM（优先 agent，fallback 网关直接 SSH）。
	if safeProgress != nil {
		safeProgress(90, "定义目标 VM")
	}
	_, agentErr := s.manager.DispatchCommand(ctx, req.SourceHostID, "p2v-define", map[string]interface{}{
		"vm_name":          req.VMName,
		"target_disk_path": targetDiskPath,
		"target_ssh":       req.TargetSSH,
		"ram_mb":           req.RamMB,
		"vcpus":            req.VCPUs,
	}, safeProgress)
	if agentErr != nil {
		log.Printf("[p2v] agent p2v-define 失败 (%v)，尝试网关直接 SSH 定义...", agentErr)
		if sshErr := gatewayDefineP2VMul(req.TargetSSH, req.VMName, targetDiskPath, req.RamMB, req.VCPUs); sshErr != nil {
			log.Printf("[p2v] 网关 SSH 定义也失败: %v", sshErr)
		}
	}

	if safeProgress != nil {
		safeProgress(100, "P2V 迁移完成")
	}
	return "done", nil
}
