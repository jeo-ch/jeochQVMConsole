package firewall

import (
	"fmt"
	"regexp"
	"strings"
)

// ── ManageHostFirewallRule（§5.4，#S3 注入防护） ──
//
// 替代 network.ManageUFWRule 的 shell 字符串拼接路径：
//   - 禁止 shell 拼接，按后端分发为结构化 argv
//   - rule 输入走白名单校验（^[0-9a-zA-Z:/,. -]+$），拦截 ; | $() 反引号等注入

var manageRuleWhitelist = regexp.MustCompile(`^[0-9a-zA-Z:/,. -]+$`)

// ManageHostFirewallRule 按后端管理单条端口放通/拒绝规则。
// action ∈ {allow, deny, delete}；rule 形如 "8080/tcp"、"5900:5999/tcp"。
// 兼容现网 /network/ufw/rule 与 spice HookManageUFWRule("allow", port+"/tcp")。
func ManageHostFirewallRule(action, rule string) error {
	action = strings.ToLower(strings.TrimSpace(action))
	rule = strings.TrimSpace(rule)
	if rule == "" || len(rule) > 64 {
		return fmt.Errorf("规则参数非法")
	}
	if !manageRuleWhitelist.MatchString(rule) {
		return fmt.Errorf("规则参数含非法字符，已拒绝（仅允许 数字/字母/冒号/斜杠/逗号/点/空格/连字符）")
	}
	var hf HostFirewallRule
	if err := parseManageRuleSpec(rule, &hf); err != nil {
		return err
	}
	backend := resolveBackend()
	if !backend.Available() {
		return fmt.Errorf("宿主机防火墙后端不可用")
	}
	switch action {
	case "allow":
		hf.Action = "allow"
	case "deny":
		hf.Action = "deny"
	case "delete":
		hf.ID = hostFirewallRuleID(hf)
		if err := backend.DeleteRule(hf); err != nil {
			return fmt.Errorf("删除防火墙规则失败: %s", err.Error())
		}
		return nil
	default:
		return fmt.Errorf("不支持的操作: %s", action)
	}
	if err := ensureHostFirewallRule(hf); err != nil {
		return fmt.Errorf("写入防火墙规则失败: %s", err.Error())
	}
	return nil
}

// parseManageRuleSpec 解析 "8080/tcp" / "5900:5999/tcp" 到 HostFirewallRule 端口与协议。
func parseManageRuleSpec(spec string, hf *HostFirewallRule) error {
	spec = strings.TrimSpace(spec)
	if strings.Contains(spec, "/") {
		parts := strings.SplitN(spec, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("规则格式应为 端口[/协议]")
		}
		spec = strings.TrimSpace(parts[0])
		proto := normalizeHostFirewallProtocol(parts[1])
		if proto == "both" {
			proto = "tcp"
		}
		if proto == "" {
			return fmt.Errorf("不支持的协议: %s", parts[1])
		}
		hf.Protocol = proto
	} else {
		hf.Protocol = "tcp"
	}
	start, end, ok := parseHostFirewallPortRange(strings.ReplaceAll(spec, "-", ":"))
	if !ok {
		return fmt.Errorf("无效端口范围: %s", spec)
	}
	hf.PortStart, hf.PortEnd = start, end
	return nil
}
