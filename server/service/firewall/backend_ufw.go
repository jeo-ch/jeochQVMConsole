package firewall

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kvm_console/utils"
)

// ── ufw 后端（§5.2：host.go 现有逻辑原样迁移，行为零变化） ──

type ufwBackend struct{}

func (ufwBackend) Name() string        { return "ufw" }
func (ufwBackend) DisplayName() string { return "UFW" }

func (ufwBackend) Available() bool {
	return commandAvailable("ufw")
}

func (ufwBackend) Version() string {
	result := utils.ExecCommand(firewallCommandPath("ufw"), "--version")
	if result.Error != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(result.Stdout, "\n", 2)[0])
}

func (ufwBackend) Active() (bool, error) {
	result := execUFW("status", "verbose")
	if result.Error != nil {
		return false, result.Error
	}
	return stringsContainsFold(result.Stdout, "status: active"), nil
}

func (ufwBackend) Defaults() (string, string, string, error) {
	result := execUFW("status", "verbose")
	if result.Error != nil {
		return "", "", "", result.Error
	}
	incoming, outgoing, routed := parseUFWDefaults(result.Stdout)
	return incoming, outgoing, routed, nil
}

func (ufwBackend) ListRules() ([]HostFirewallRule, error) {
	result := execUFW("show", "added")
	if result.Error != nil {
		return nil, fmt.Errorf("读取 UFW 规则失败: %s", result.Stderr)
	}
	return parseUFWAddedRules(result.Stdout), nil
}

func (ufwBackend) EnsureRule(rule HostFirewallRule) error {
	return backendExec(func() error {
		args := buildUFWRuleArgs(rule, false)
		result := utils.ExecCommand(firewallCommandPath("ufw"), args...)
		if result.Error != nil {
			return fmt.Errorf("写入 UFW 规则失败: %s", result.Stderr)
		}
		return nil
	})
}

func (ufwBackend) DeleteRule(rule HostFirewallRule) error {
	return backendExec(func() error {
		args := buildUFWRuleArgs(rule, true)
		result := utils.ExecCommand(firewallCommandPath("ufw"), args...)
		if result.Error != nil {
			return fmt.Errorf("删除 UFW 规则失败: %s", result.Stderr)
		}
		return nil
	})
}

// Enable 原子序列：默认策略 + --force enable，一次锁持有（§4.2 锁粒度）。
func (ufwBackend) Enable(progress func(int, string)) error {
	return backendExec(func() error {
		if progress != nil {
			progress(25, "正在补齐 UFW 基础策略...")
		}
		commands := [][]string{
			{"default", "deny", "incoming"},
			{"default", "allow", "outgoing"},
			{"default", "allow", "routed"},
		}
		for _, args := range commands {
			result := utils.ExecCommand(firewallCommandPath("ufw"), args...)
			if result.Error != nil {
				return fmt.Errorf("设置 UFW 默认策略失败: %s", result.Stderr)
			}
		}
		if progress != nil {
			progress(80, "正在启用宿主机防火墙...")
		}
		result := utils.ExecCommandWithTimeout(firewallCommandPath("ufw"), 2*time.Minute, "--force", "enable")
		if result.Error != nil {
			return fmt.Errorf("启用 UFW 失败: %s", result.Stderr)
		}
		if progress != nil {
			progress(100, "宿主机防火墙已启用")
		}
		return nil
	})
}

func (ufwBackend) Disable() error {
	return backendExec(func() error {
		result := utils.ExecCommandWithTimeout(firewallCommandPath("ufw"), 2*time.Minute, "--force", "disable")
		if result.Error != nil {
			return fmt.Errorf("关闭 UFW 失败: %s", result.Stderr)
		}
		return nil
	})
}

// ── 命令执行（锁包裹单条子进程，防并发踩踏 #B） ──

func execUFW(args ...string) *utils.CmdResult {
	var result *utils.CmdResult
	_ = backendExec(func() error {
		result = utils.ExecCommand(firewallCommandPath("ufw"), args...)
		return nil
	})
	return result
}

// ── UFW output parsing helpers（host.go 原样迁移） ──

func parseUFWDefaults(text string) (string, string, string) {
	incoming, outgoing, routed := "", "", ""
	re := regexp.MustCompile(`(?i)default:\s*([^,]+)\s*\(incoming\),\s*([^,]+)\s*\(outgoing\)(?:,\s*([^\n]+)\s*\(routed\))?`)
	if m := re.FindStringSubmatch(text); len(m) > 0 {
		incoming = strings.TrimSpace(m[1])
		outgoing = strings.TrimSpace(m[2])
		if len(m) > 3 {
			routed = strings.TrimSpace(m[3])
		}
	}
	return incoming, outgoing, routed
}

func parseUFWAddedRules(text string) []HostFirewallRule {
	var rules []HostFirewallRule
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "ufw ") {
			continue
		}
		rule, ok := parseUFWAddedRuleLine(line)
		if ok {
			rules = append(rules, rule)
		}
	}
	return rules
}

func parseUFWAddedRuleLine(line string) (HostFirewallRule, bool) {
	fields := shellLikeFields(line)
	if len(fields) < 3 || fields[0] != "ufw" {
		return HostFirewallRule{}, false
	}
	rule := HostFirewallRule{Raw: line, Action: normalizeHostFirewallAction(fields[1]), SourceCIDR: ""}
	if rule.Action == "" {
		return HostFirewallRule{}, false
	}
	commentIndex := indexOfString(fields, "comment")
	if commentIndex >= 0 && commentIndex+1 < len(fields) {
		rule.Comment = fields[commentIndex+1]
		fields = fields[:commentIndex]
	}
	if len(fields) >= 6 && fields[2] == "from" {
		rule.SourceCIDR = strings.TrimSpace(fields[3])
		portIndex := indexOfString(fields, "port")
		protoIndex := indexOfString(fields, "proto")
		if portIndex < 0 || portIndex+1 >= len(fields) {
			return HostFirewallRule{}, false
		}
		start, end, proto, ok := parseHostFirewallPortSpec(fields[portIndex+1])
		if !ok {
			return HostFirewallRule{}, false
		}
		if protoIndex >= 0 && protoIndex+1 < len(fields) {
			proto = normalizeHostFirewallProtocol(fields[protoIndex+1])
		}
		rule.PortStart, rule.PortEnd, rule.Protocol = start, end, proto
	} else {
		start, end, proto, ok := parseHostFirewallPortSpec(fields[2])
		if !ok {
			return HostFirewallRule{}, false
		}
		rule.PortStart, rule.PortEnd, rule.Protocol = start, end, proto
	}
	if rule.Protocol == "" {
		rule.Protocol = "both"
	}
	rule.ManagedByPanel = strings.HasPrefix(rule.Comment, hostFirewallPanelPrefix)
	rule.ID = hostFirewallRuleID(rule)
	return rule, true
}

func parseHostFirewallPortSpec(spec string) (int, int, string, bool) {
	spec = strings.TrimSpace(spec)
	proto := ""
	if strings.Contains(spec, "/") {
		parts := strings.SplitN(spec, "/", 2)
		spec = parts[0]
		proto = normalizeHostFirewallProtocol(parts[1])
	}
	start, end, ok := parseHostFirewallPortRange(spec)
	return start, end, proto, ok
}

func parseHostFirewallPortRange(text string) (int, int, bool) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "-", ":"))
	if text == "" {
		return 0, 0, false
	}
	parts := strings.Split(text, ":")
	if len(parts) > 2 {
		return 0, 0, false
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	if start < 1 || start > 65535 || end < start || end > 65535 {
		return 0, 0, false
	}
	return start, end, true
}

func buildUFWRuleArgs(rule HostFirewallRule, delete bool) []string {
	portSpec := hostFirewallPortSpec(rule)
	args := []string{}
	if delete {
		args = append(args, "delete")
	}
	args = append(args, rule.Action)
	if strings.TrimSpace(rule.SourceCIDR) != "" {
		args = append(args, "from", strings.TrimSpace(rule.SourceCIDR), "to", "any", "port", portSpec, "proto", rule.Protocol)
	} else {
		args = append(args, portSpec+"/"+rule.Protocol)
	}
	if !delete && strings.TrimSpace(rule.Comment) != "" {
		args = append(args, "comment", strings.TrimSpace(rule.Comment))
	}
	return args
}
