// Package vmwatchdog 提供 VM 看门狗（M8.9 / §14 P2-9）。
//
// 看门狗周期性探测运行中 VM 的 QEMU Guest Agent 可达性：连续 N 次（默认 3 次）
// 无响应时自动硬重置 VM（virsh reset 语义），并记录审计事件到 DB；同时周期性
// 检测宿主机大页配置（内存 ≥128GB 且 HugePages_Total=0 → 建议开启大页）。
//
// 设计要点：
//   - 探测走 guest_agent.Ping（QEMU guest-ping，超时短），不依赖面板数据库；
//   - 重置动作通过 HookResetVM 注入（service 根包实现，复用既有 ResetVM 完整链路），
//     避免 vmwatchdog ↔ vm 循环依赖；
//   - 每个 VM 的连续失联计数保存在内存，VM 恢复响应或关机后自动清零；
//   - 维护模式/无 libvirt 连接时静默跳过，不打扰运维操作。
package vmwatchdog

import (
	"os"
	"strings"
	"sync"
	"time"

	"kvm_console/config"
	"kvm_console/logger"
	"kvm_console/model"
	"kvm_console/service/guest_agent"
	"kvm_console/service/libvirt_rpc"
	"kvm_console/utils"
)

// HookResetVM 由 service 根包注入：完整重置虚拟机（复用 vm.ResetVM 链路）。
var HookResetVM func(vmName string) error

// HookIsMaintenanceModeEnabled 由 service 根包注入：判断当前是否维护模式。
var HookIsMaintenanceModeEnabled func() bool

// 默认参数（可经 .env 覆盖，见 config.Init）
const (
	defaultWatchdogInterval = 60 * time.Second
	defaultMaxMisses        = 3
)

// 大页建议阈值：内存 ≥ 128GB 且 HugePages_Total == 0
const hugepagesMemThresholdGB = 128

// 各 VM 连续失联计数（内存态，服务重启清零）
var (
	missCountsMu sync.Mutex
	missCounts   = make(map[string]int)
)

// resetMissCount 清零指定 VM 的连续失联计数。
func resetMissCount(vmName string) {
	missCountsMu.Lock()
	defer missCountsMu.Unlock()
	delete(missCounts, vmName)
}

// bumpMissCount 递增连续失联计数并返回最新值。
func bumpMissCount(vmName string) int {
	missCountsMu.Lock()
	defer missCountsMu.Unlock()
	missCounts[vmName]++
	return missCounts[vmName]
}

// WatchdogConfig 看门狗运行参数（从 config.GlobalConfig 读取，可经 .env 覆盖）。
type WatchdogConfig struct {
	Enabled   bool
	Interval  time.Duration
	MaxMisses int
}

func loadWatchdogConfig() WatchdogConfig {
	cfg := WatchdogConfig{
		Enabled:   true,
		Interval:  defaultWatchdogInterval,
		MaxMisses: defaultMaxMisses,
	}
	if config.GlobalConfig != nil {
		cfg.Enabled = config.GlobalConfig.VMWatchdogEnabled
		if config.GlobalConfig.VMWatchdogIntervalSeconds > 0 {
			cfg.Interval = time.Duration(config.GlobalConfig.VMWatchdogIntervalSeconds) * time.Second
		}
		if config.GlobalConfig.VMWatchdogMaxMisses > 0 {
			cfg.MaxMisses = config.GlobalConfig.VMWatchdogMaxMisses
		}
	}
	return cfg
}

// StartWatchdog 启动看门狗后台协程（main.go 启动阶段调用）。
// 每轮循环重新读取配置，使设置页/.env 变更无需重启即生效（M8.9/§14 P2-9 + 设置页联动）。
func StartWatchdog() {
	cfg := loadWatchdogConfig()
	if !cfg.Enabled {
		logger.App.Info("VM 看门狗已禁用（KVM_VM_WATCHDOG_ENABLED=false）")
		return
	}
	logger.App.Info("VM 看门狗已启动", "interval", cfg.Interval.String(), "maxMisses", cfg.MaxMisses)
	go func() {
		defer utils.RecoverAndLog("vm-watchdog")
		runWatchdogOnce(cfg) // 启动后立即执行一次
		for {
			cfg = loadWatchdogConfig()
			if !cfg.Enabled {
				logger.App.Info("VM 看门狗已禁用（设置已变更，本轮起停止探测）")
				return
			}
			time.Sleep(cfg.Interval)
			runWatchdogOnce(cfg)
		}
	}()
}

// runWatchdogOnce 单轮看门狗探测：轮询运行中 VM 的 Guest Agent，触发硬重置或大页建议。
func runWatchdogOnce(cfg WatchdogConfig) {
	if HookIsMaintenanceModeEnabled != nil && HookIsMaintenanceModeEnabled() {
		return
	}

	// 大页建议（宿主机内存 ≥128GB 且 HugePages_Total=0 → 建议开启大页）
	checkHugePagesAdvice()

	if !libvirt_rpc.IsLibvirtRPCAvailable() {
		logger.App.Debug("libvirt 不可用，看门狗跳过本轮")
		return
	}
	domains, err := libvirt_rpc.ListAllDomainsRPC()
	if err != nil {
		logger.App.Warn("看门狗获取域列表失败", "error", err)
		return
	}

	// 当前存在的域集合：用于清理已删除 VM 的计数
	alive := make(map[string]bool, len(domains))
	for _, dom := range domains {
		alive[dom.Name] = true
		watchSingleVM(dom.Name, cfg)
	}

	// 清理已不存在的域计数
	missCountsMu.Lock()
	for name := range missCounts {
		if !alive[name] {
			delete(missCounts, name)
		}
	}
	missCountsMu.Unlock()
}

// watchSingleVM 探测单台 VM：仅对运行中且配置了 GA 的 VM 执行 ping。
func watchSingleVM(vmName string, cfg WatchdogConfig) {
	state, err := libvirt_rpc.GetDomainStateRPC(vmName)
	if err != nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(state), "running") {
		resetMissCount(vmName)
		return
	}

	// 仅对配置了 GA 通道的 VM 探测；未配置 GA 的 VM 不纳入看门狗
	status := guest_agent.CheckVMGuestAgentStatus(vmName)
	if !status.Configured {
		resetMissCount(vmName)
		return
	}
	if status.Connected {
		resetMissCount(vmName)
		return
	}

	misses := bumpMissCount(vmName)
	logger.App.Warn("VM 看门狗: Guest Agent 无响应", "vm", vmName, "misses", misses, "max", cfg.MaxMisses)
	if misses < cfg.MaxMisses {
		return
	}

	resetMissCount(vmName)
	if HookResetVM == nil {
		logger.App.Warn("VM 看门狗: HookResetVM 未注入，跳过自动重置", "vm", vmName)
		return
	}
	if err := HookResetVM(vmName); err != nil {
		logger.App.Error("VM 看门狗: 自动重置失败", "vm", vmName, "error", err)
		_ = model.CreateVMWatchdogEvent(&model.VMWatchdogEvent{
			VMName:        vmName,
			Status:        "reset",
			Reason:        "Guest Agent 连续失联，自动重置失败",
			ResultMessage: err.Error(),
		})
		return
	}
	logger.App.Warn("VM 看门狗: Guest Agent 连续失联，已自动硬重置", "vm", vmName)
	_ = model.CreateVMWatchdogEvent(&model.VMWatchdogEvent{
		VMName:        vmName,
		Status:        "reset",
		Reason:        "Guest Agent 连续失联，看门狗自动硬重置",
		ResultMessage: "virsh reset 已执行",
	})
}

// HugePagesInfo 大页配置信息（供 component_health / 诊断页展示）。
type HugePagesInfo struct {
	Enabled          bool   `json:"enabled"`            // 大页是否已启用（HugePages_Total > 0）
	TotalPages       int64  `json:"total_pages"`        // HugePages_Total
	MemTotalGB       int64  `json:"mem_total_gb"`       // 总内存（GB，向下取整）
	MemOverThreshold bool   `json:"mem_over_threshold"` // 总内存 ≥ 128GB
	Suggested        bool   `json:"suggested"`          // 是否建议开启大页
	Message          string `json:"message"`
}

// CheckHugePagesAdvice 检查宿主机大页配置（内存 ≥128GB 且 HugePages_Total=0 → 建议开启大页）。
// 读 /proc/meminfo，无 root 权限也可读；不可读时返回 disabled 结构体。
func CheckHugePagesAdvice() HugePagesInfo {
	info := HugePagesInfo{}
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		info.Message = "无法读取 /proc/meminfo，跳过内存大页检查"
		return info
	}
	for _, line := range strings.Split(string(meminfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			// 单位 kB
			totalKB := parseKBytes(fields[1])
			info.MemTotalGB = totalKB / (1024 * 1024)
		case "HugePages_Total:":
			info.TotalPages = parseInt64(fields[1])
		}
	}
	info.Enabled = info.TotalPages > 0
	info.MemOverThreshold = info.MemTotalGB >= hugepagesMemThresholdGB
	info.Suggested = info.MemOverThreshold && !info.Enabled
	switch {
	case info.Suggested:
		info.Message = "宿主机内存 ≥128GB 且未启用大页，建议开启 HugePages 以提升虚拟化内存性能"
	case info.MemOverThreshold:
		info.Message = "宿主机内存 ≥128GB 且已启用大页"
	case !info.Enabled:
		info.Message = "宿主机未启用大页（内存未达建议阈值）"
	default:
		info.Message = "宿主机已启用大页"
	}
	if info.Suggested {
		logger.App.Warn("建议开启大页: " + info.Message)
	}
	return info
}

// checkHugePagesAdvice 周期性调用（内部简写，避免每次启动多写一条事件）。
func checkHugePagesAdvice() {
	CheckHugePagesAdvice()
}

func parseInt64(s string) int64 {
	var n int64
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int64(r-'0')
		} else {
			break
		}
	}
	return n
}

func parseKBytes(s string) int64 {
	return parseInt64(s)
}
