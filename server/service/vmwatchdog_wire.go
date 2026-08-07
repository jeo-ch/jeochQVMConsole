package service

import vmwatchdogpkg "kvm_console/service/vmwatchdog"

// init wires vmwatchdog package hook variables to service root implementations.
// This breaks the circular dependency: vmwatchdog package cannot import service,
// so it exposes hook variables that we set here.
func init() {
	vmwatchdogpkg.HookResetVM = ResetVM
	vmwatchdogpkg.HookIsMaintenanceModeEnabled = IsMaintenanceModeEnabled
}

// ── Exported delegates ──

// StartVMWatchdog 启动 VM 看门狗（M8.9 / §14 P2-9）。
func StartVMWatchdog() {
	vmwatchdogpkg.StartWatchdog()
}

// CheckHugePagesAdvice 检查宿主机大页建议（内存 ≥128GB 且未启用大页）。
func CheckHugePagesAdvice() vmwatchdogpkg.HugePagesInfo {
	return vmwatchdogpkg.CheckHugePagesAdvice()
}
