package main

import (
	"context"
	"encoding/json"
	"log"
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
func (s *MigrationService) Pull(ctx context.Context, hostID uint, req MigrationRequest, progress func(int, string)) (*PullResult, error) {
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

// RunMigration 按顺序执行一次完整迁移编排。
func (s *MigrationService) RunMigration(ctx context.Context, req MigrationRequest, progress func(int, string)) (string, error) {
	if req.Format == "" {
		req.Format = "qcow2"
	}
	if req.SnapshotName == "" {
		req.SnapshotName = "qvmconsole_migration_" + time.Now().Format("20060102150405")
	}

	if progress != nil {
		progress(10, "创建快照")
	}
	if err := s.Snapshot(ctx, req.SourceHostID, req.VMName, req.SnapshotName); err != nil {
		return "failed", err
	}
	if progress != nil {
		progress(40, "拉取磁盘")
	}
	if _, err := s.Pull(ctx, req.SourceHostID, req, progress); err != nil {
		return "failed", err
	}
	if req.Shutdown {
		if progress != nil {
			progress(80, "切换至目标主机")
		}
		if err := s.Cutover(ctx, req.SourceHostID, req.VMName); err != nil {
			return "failed", err
		}
	}
	if progress != nil {
		progress(100, "清理快照")
	}
	// 清理快照失败不阻断迁移（快照可能已在传输过程中被自动清理）。
	if err := s.Cleanup(ctx, req.SourceHostID, req.VMName, req.SnapshotName); err != nil {
		log.Printf("[migration] 清理快照失败（可忽略）: %v", err)
	}
	return "done", nil
}
