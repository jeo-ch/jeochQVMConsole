package network

import (
	"fmt"
	"strings"

	"kvm_console/config"
	"kvm_console/utils"
)

// GetUFWStatus 获取 UFW 状态（行为兼容：后端不可用时返回明确错误文案，§5.6）
func GetUFWStatus() (string, error) {
	if HookGetFirewallBackendAvailable == nil || !HookGetFirewallBackendAvailable() {
		return "", fmt.Errorf("宿主机防火墙后端不可用")
	}
	result := utils.ExecCommand(firewallStatusCommand(), "status", "numbered")
	if result.Error != nil {
		return "", fmt.Errorf("获取防火墙状态失败: %s", result.Stderr)
	}
	return result.Stdout, nil
}

// ManageUFWRule 管理防火墙规则（§5.4：#S3 修复命令注入，改走结构化 argv + 白名单）
func ManageUFWRule(action, rule string) error {
	if HookManageHostFirewallRule == nil {
		return fmt.Errorf("防火墙规则管理服务未就绪")
	}
	return HookManageHostFirewallRule(action, rule)
}

// firewallStatusCommand 返回当前后端状态命令（ufw/firewalld 兼容）。兼容版旧路径保留 ufw。
func firewallStatusCommand() string {
	// 探测后端后按名返回命令；无 hook 时默认 ufw（旧行为）
	if HookGetFirewallBackendName != nil {
		switch HookGetFirewallBackendName() {
		case "firewalld":
			return "firewall-cmd"
		case "ufw":
			return "ufw"
		}
	}
	return "ufw"
}

// getHostIP 获取宿主机外网 IP
func getHostIP() string {
	// 优先使用配置的固定 IP
	if config.GlobalConfig.HostIP != "" {
		return config.GlobalConfig.HostIP
	}

	// 使用配置的外网网卡名称
	nic := config.GlobalConfig.ExternalNIC
	if nic != "" {
		result := utils.ExecShell(fmt.Sprintf(
			"ip -4 addr show %s 2>/dev/null | grep -oP '(?<=inet\\s)\\d+\\.\\d+\\.\\d+\\.\\d+'", utils.ShellSingleQuote(nic)))
		if result.Error == nil && result.Stdout != "" {
			return strings.TrimSpace(result.Stdout)
		}
	}

	// 自动检测：通过默认路由获取外网网卡 IP
	result := utils.ExecShell(
		"ip -4 route get 8.8.8.8 2>/dev/null | grep -oP 'src \\K\\S+'")
	if result.Error == nil && result.Stdout != "" {
		return strings.TrimSpace(result.Stdout)
	}

	return "0.0.0.0"
}
