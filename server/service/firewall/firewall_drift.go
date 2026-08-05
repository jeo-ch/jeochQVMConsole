package firewall

import (
	"os"
	"sync"
	"time"

	"kvm_console/logger"
	"kvm_console/utils"
)

// ── 防火墙后端低频巡检（M1） ──
// resolveBackend 只在后端命令不可用（Available()=false）时自愈重测，无法感知
// 「命令存在但 firewalld 服务长期停止」的漂移（#H 有意为之：命令存在即视为已探测）。
// StartFirewallDriftMonitor 每日强制重测一次后端状态，发现服务停止且面板防火墙曾启用时
// 记录日志告警，供运维恢复；不改变后端解析规则。

const (
	firewallDriftInitialDelay = time.Minute // 启动后 1 分钟首次巡检，尽早暴露漂移
	firewallDriftInterval     = 24 * time.Hour
)

var firewallDriftOnce sync.Once

// StartFirewallDriftMonitor 启动防火墙后端低频巡检协程（M1，main 启动时调用）。
func StartFirewallDriftMonitor() {
	firewallDriftOnce.Do(func() {
		go func() {
			defer utils.RecoverAndLog("firewall-drift-monitor")
			time.Sleep(firewallDriftInitialDelay)
			for {
				checkFirewallDrift()
				time.Sleep(firewallDriftInterval)
			}
		}()
	})
}

// checkFirewallDrift 执行一次巡检。仅针对 firewalld（国产系统后端）：
// 面板曾启用 qvm-host 防火墙但服务当前未运行 → 防护策略已失效，记录告警。
// ufw 无独立服务（规则直挂 iptables），不存在同类漂移，跳过。
func checkFirewallDrift() {
	backend := resolveBackend()
	if backend == nil || backend.Name() != "firewalld" {
		return
	}
	if !backend.Available() {
		// 命令缺失：resolveBackend 已自愈重测，此处无需重复告警
		return
	}
	active, err := backend.Active()
	if err != nil || active {
		return
	}
	// 面板未启用过防火墙（无 qvm-host zone）时服务停止属正常，不告警
	if _, statErr := os.Stat(firewalldZoneFile); statErr != nil {
		return
	}
	logger.App.Warn("防火墙后端巡检：firewalld 服务未运行，宿主机防火墙策略已失效，建议尽快恢复（systemctl start firewalld）", "backend", "firewalld")
}
