package arch

import (
	"os"
	"os/exec"
)

// ── 麒麟 KYSEC 安全机制探测（Kylin 专项，§5.8 国产化兼容） ──
//
// KYSEC（麒麟内核安全机制）是银河麒麟高级服务器操作系统 V10+ 的内生安全框架，
// 其强制访问控制/可信度量链可能限制内核模块加载（modprobe kvm）与关键路径访问，
// 导致 /dev/kvm 无法启用或 libvirt/QEMU 虚拟机启动异常。
// 探测点防御性多重回退（kysec_ctl 命令 / sysfs / procfs / 配置目录），
// 非麒麟主机任一探测点均不命中，返回 disabled。

const (
	// kysecProbePathSysfs 麒麟 KYSEC sysfs 挂载点
	kysecProbePathSysfs = "/sys/kernel/security/kysec"
	// kysecProbePathProcfs 麒麟 KYSEC procfs 探测点
	kysecProbePathProcfs = "/proc/kysec"
	// kysecProbePathEtc 麒麟 KYSEC 配置目录
	kysecProbePathEtc = "/etc/kysec"
)

// DetectKYSEC 探测麒麟 KYSEC 安全机制是否启用。
// 返回：enabled=是否启用；detail=命中的探测点（便于诊断/展示）。
// 任一探测点命中即视为启用（KYSEC 框架存在即认为安全策略生效中）。
func DetectKYSEC() (enabled bool, detail string) {
	if _, err := exec.LookPath("kysec_ctl"); err == nil {
		return true, "kysec_ctl"
	}
	for _, p := range []string{kysecProbePathSysfs, kysecProbePathProcfs, kysecProbePathEtc} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return true, p
		}
	}
	return false, ""
}

// KysecStatus 返回 KYSEC 状态的可展示串（component_health / /system-info 上报口径）：
// 启用返回 "enabled"，未启用返回 ""（不展示，避免非麒麟主机出现无意义字段）。
func KysecStatus() string {
	enabled, _ := DetectKYSEC()
	if enabled {
		return "enabled"
	}
	return ""
}
