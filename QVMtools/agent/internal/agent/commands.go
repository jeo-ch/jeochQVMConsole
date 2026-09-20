package agent

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
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
