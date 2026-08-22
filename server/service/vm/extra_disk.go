package vm

import (
	"context"
	"fmt"
	"strings"

	"kvm_console/logger"
	"kvm_console/model"
	"kvm_console/taskqueue"
)

// VMExtraDiskCreateParams 创建虚拟机额外磁盘的任务参数
type VMExtraDiskCreateParams struct {
	VMName      string               `json:"vm_name"`
	ExtraDisks  []ExtraDiskParam     `json:"extra_disks"`
	CloneDir    string               `json:"clone_dir"`
	IsAdmin     bool                 `json:"is_admin"`
	DiskBus     string               `json:"disk_bus"`      // 默认总线类型（系统盘总线类型）
	Owner       string               `json:"owner"`         // 操作人（用于审计/权限）
}

// VMExtraDiskCreateResult 额外磁盘创建任务的结果
type VMExtraDiskCreateResult struct {
	VMName      string   `json:"vm_name"`
	TotalDisks  int      `json:"total_disks"`
	Success     int      `json:"success"`
	Failed      int      `json:"failed"`
	Failures    []string `json:"failures,omitempty"`
	Devices     []string `json:"devices,omitempty"` // 成功创建的设备名
}

// CreateVMExtraDisks 创建虚拟机额外磁盘（异步任务执行器）
// 在 VM 已创建且定义完成后调用，顺序创建每块额外磁盘
func CreateVMExtraDisks(ctx context.Context, params VMExtraDiskCreateParams, progress func(int, string)) (*VMExtraDiskCreateResult, error) {
	if len(params.ExtraDisks) == 0 {
		return &VMExtraDiskCreateResult{
			VMName:     params.VMName,
			TotalDisks: 0,
			Success:    0,
			Failed:     0,
		}, nil
	}

	total := len(params.ExtraDisks)
	result := &VMExtraDiskCreateResult{
		VMName:     params.VMName,
		TotalDisks: total,
		Success:    0,
		Failed:     0,
		Failures:   make([]string, 0),
		Devices:    make([]string, 0),
	}

	for i, ed := range params.ExtraDisks {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		progressPercent := 10 + (i * 80 / total)
		progress(progressPercent, fmt.Sprintf("创建额外磁盘 %d/%d", i+1, total))

		format := ed.Format
		if format == "" {
			format = "qcow2"
		}
		bus := ed.Bus
		if bus == "" {
			bus = params.DiskBus
		}

		diskDir := params.CloneDir
		if strings.TrimSpace(ed.StoragePoolID) != "" {
			resolvedDir, _, resolveErr := D.ResolveVMStorageDir(ed.StoragePoolID, params.IsAdmin)
			if resolveErr != nil {
				errMsg := fmt.Sprintf("磁盘%d: 解析存储位置失败: %s", i+1, resolveErr.Error())
				result.Failures = append(result.Failures, errMsg)
				result.Failed++
				progress(progressPercent, errMsg)
				continue
			}
			diskDir = resolvedDir
		}

		dev, err := D.AddDiskWithBusInDir(params.VMName, ed.Size, format, bus, diskDir)
		if err != nil {
			errMsg := fmt.Sprintf("磁盘%d: 挂载失败: %s", i+1, err.Error())
			result.Failures = append(result.Failures, errMsg)
			result.Failed++
			progress(progressPercent, errMsg)
			continue
		}

		result.Success++
		result.Devices = append(result.Devices, dev)
		progress(progressPercent, fmt.Sprintf("额外磁盘 %d/%d 创建成功: %s", i+1, total, dev))
	}

	// 最终进度
	progress(95, fmt.Sprintf("额外磁盘创建完成: 成功 %d, 失败 %d", result.Success, result.Failed))

	if result.Failed > 0 {
		logger.App.Warn("虚拟机额外磁盘部分失败", "vm", params.VMName, "failures", strings.Join(result.Failures, "; "))
	}

	return result, nil
}

// CreateVMExtraDisksForHandler 供 handler 调用，用于提交额外磁盘创建任务
func CreateVMExtraDisksForHandler(vmName string, extraDisks []ExtraDiskParam, cloneDir string, isAdmin bool, diskBus string, owner string) (string, error) {
	if len(extraDisks) == 0 {
		return "", nil
	}

	params := VMExtraDiskCreateParams{
		VMName:     vmName,
		ExtraDisks: extraDisks,
		CloneDir:   cloneDir,
		IsAdmin:    isAdmin,
		DiskBus:    diskBus,
		Owner:      owner,
	}

	task, err := taskqueue.SubmitWithStruct(model.TaskTypeVMExtraDiskCreate, params, owner)
	if err != nil {
		return "", fmt.Errorf("提交额外磁盘创建任务失败: %w", err)
	}

	return fmt.Sprintf("%d", task.ID), nil
}