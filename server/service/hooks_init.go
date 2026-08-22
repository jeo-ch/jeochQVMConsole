package service

import (
	"kvm_console/model"
	"kvm_console/service/scheduler"
	"kvm_console/service/vm/memory"
)

// hooks_init.go - 将 service 根包的函数注入到 memory 子包的反向依赖 Hook 变量中，
// 供子包通过 Hook 间接调用根包函数，避免循环 import。

func init() {
	// Register memory package functions into service hooks
	// (moved from memory/register.go to avoid import cycle)
	HookApplyPendingVMMemoryConfig = memory.ApplyPendingVMMemoryConfig
	HookGetVMMemoryDynamicInfo = func(name, xmlStr, state string) any {
		return memory.GetVMMemoryDynamicInfo(name, xmlStr, state)
	}

	// Register memory hooks (service root → memory sub-package)
	memory.HookMemoryGetCachedStats = func(name string) *memory.VmStats {
		// memory 包仅用 VmStats 判非 nil，不需要实际字段
		if GetCachedStats(name) != nil {
			return &memory.VmStats{}
		}
		return nil
	}
	memory.HookMemoryGetHostStats = func() (*memory.HostStats, error) {
		stats, err := GetHostStats()
		if err != nil || stats == nil {
			return nil, err
		}
		return &memory.HostStats{
			MemTotal: stats.MemTotal,
			MemFree:  stats.MemFree,
		}, nil
	}
	memory.HookMemoryIsMaintenanceModeEnabled = func() bool {
		return IsMaintenanceModeEnabled()
	}
	memory.HookMemoryInjectMemballoonConfig = func(xmlStr string, enableFPR bool) string {
		return InjectMemballoonConfig(xmlStr, enableFPR)
	}

	// 动态内存调度器事件（scheduler 中心 → memory 子包反向依赖）
	// 直接使用 scheduler 包的共享类型，无需转换
	memory.HookMemoryRegisterScheduler = func(def scheduler.SchedulerDefinition) {
		RegisterScheduler(def)
	}
	memory.HookMemoryStartSchedulerEvent = func(input scheduler.SchedulerEventStartInput) (interface{}, error) {
		return StartSchedulerEvent(input)
	}
	memory.HookMemoryFinishSchedulerEventOk = func(event interface{}, msg string) error {
		ev, ok := event.(*model.SchedulerEvent)
		if !ok || ev == nil {
			return nil
		}
		return FinishSchedulerEventSuccess(ev, msg)
	}
	memory.HookMemoryFinishSchedulerEventFail = func(event interface{}, msg string) error {
		ev, ok := event.(*model.SchedulerEvent)
		if !ok || ev == nil {
			return nil
		}
		return FinishSchedulerEventFailed(ev, msg)
	}
}
