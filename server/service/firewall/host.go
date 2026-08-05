package firewall

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"kvm_console/config"
	"kvm_console/utils"
)

// ── 宿主机防火墙编排层（§5.3：命令执行全走后端，本文件仅保留编排逻辑） ──

// GetHostFirewallStatus 宿主机防火墙状态编排（§5.3：命令执行全走后端，本文件仅保留编排逻辑）。
// 探测结果统一复用 GetFirewallBackendStatus（Active/Defaults/规则/IPBackend/Docker 一次探测），
// 避免 status 轮询时对同一后端重复发起子进程。
func GetHostFirewallStatus() (*HostFirewallStatus, error) {
	backendStatus := GetFirewallBackendStatus()
	status := &HostFirewallStatus{
		Backend:             backendStatus.Backend,
		BackendName:         backendStatus.BackendName,
		UFWAvailable:        backendStatus.Available, // 兼容前端旧字段
		Active:              backendStatus.Active,
		DefaultIncoming:     backendStatus.DefaultIncoming,
		DefaultOutgoing:     backendStatus.DefaultOutgoing,
		DefaultRouted:       backendStatus.DefaultRouted,
		IPBackend:           backendStatus.IPBackend,
		ErrorCode:           backendStatus.ErrorCode, // #R：供前端 hint/ip_backend Tooltip 消费
		LastError:           backendStatus.LastError,
		DockerCompatible:    backendStatus.DockerCompatible, // #D：§5.1 决策 6
		DockerCompatibility: backendStatus.DockerCompatibility,
	}
	sshPorts := DetectSSHPorts()
	panelPorts := DetectPanelPorts()
	status.SSHPorts = sshPorts
	status.PanelPorts = panelPorts
	status.ProtectedRules = buildProtectedHostFirewallRules(sshPorts, panelPorts)
	status.RecommendedRules = BuildHostFirewallRecommendedRules()
	// 编排层：解析 + 保护标记 + 排序（§5.3），复用 backendStatus 的原始规则避免二次拉取
	if backendStatus.LastError == "" {
		rules := backendStatus.Rules
		for i := range rules {
			markHostFirewallProtection(&rules[i], sshPorts, panelPorts)
		}
		sortHostFirewallRules(rules)
		status.Rules = rules
	}
	return status, nil
}

func ListHostFirewallRules() ([]HostFirewallRule, error) {
	backend := resolveBackend()
	rules, err := backend.ListRules()
	if err != nil {
		return nil, err
	}
	sshPorts := DetectSSHPorts()
	panelPorts := DetectPanelPorts()
	for i := range rules {
		markHostFirewallProtection(&rules[i], sshPorts, panelPorts)
	}
	sortHostFirewallRules(rules)
	return rules, nil
}

func PreviewEnableHostFirewall(req HostFirewallEnableRequest) (*HostFirewallStatus, error) {
	status, err := GetHostFirewallStatus()
	if err != nil {
		return nil, err
	}
	status.RecommendedRules = mergeHostFirewallRules(BuildHostFirewallRecommendedRules(), normalizeHostFirewallRuleRequests(req.Rules))
	return status, nil
}

func EnableHostFirewall(req HostFirewallEnableRequest, progress func(int, string)) error {
	backend := resolveBackend()
	if !backend.Available() {
		return fmt.Errorf("宿主机防火墙后端不可用")
	}
	if progress != nil {
		progress(10, "正在探测 SSH 和面板端口...")
	}
	allRules := mergeHostFirewallRules(buildProtectedHostFirewallRules(DetectSSHPorts(), DetectPanelPorts()), normalizeHostFirewallRuleRequests(req.Rules))
	if len(allRules) == 0 {
		return fmt.Errorf("未检测到需要保护的 SSH 或面板端口，已取消启用防火墙")
	}
	if progress != nil {
		progress(50, "正在写入确认后的放通规则...")
	}
	for _, rule := range allRules {
		if err := ensureHostFirewallRule(rule); err != nil {
			return err
		}
	}
	return backend.Enable(progress)
}

func DisableHostFirewall(progress func(int, string)) error {
	backend := resolveBackend()
	if !backend.Available() {
		return fmt.Errorf("宿主机防火墙后端不可用")
	}
	if progress != nil {
		progress(20, "正在关闭宿主机防火墙...")
	}
	return backend.Disable()
}

func DetectSSHPorts() []int {
	ports := map[int]bool{}
	result := utils.ExecCommand(firewallCommandPath("sshd"), "-T")
	if result.Error == nil {
		for _, line := range strings.Split(result.Stdout, "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) == 2 && fields[0] == "port" {
				if port, err := strconv.Atoi(fields[1]); err == nil && port > 0 && port <= 65535 {
					ports[port] = true
				}
			}
		}
	}
	if len(ports) == 0 {
		result = utils.ExecShellQuiet(`ss -tlnp 2>/dev/null | grep -E 'sshd|/ssh' | awk '{print $4}' | grep -oE '[0-9]+$' | sort -un`)
		for _, line := range strings.Split(result.Stdout, "\n") {
			if port, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && port > 0 && port <= 65535 {
				ports[port] = true
			}
		}
	}
	if len(ports) == 0 {
		ports[22] = true
	}
	return sortedPorts(ports)
}

func DetectPanelPorts() []int {
	ports := map[int]bool{}
	if config.GlobalConfig != nil && config.GlobalConfig.Port > 0 {
		ports[config.GlobalConfig.Port] = true
	}
	result := utils.ExecShellQuiet(`ss -tlnp 2>/dev/null | grep -E 'kvm-console|server' | awk '{print $4}' | grep -oE '[0-9]+$' | sort -un`)
	for _, line := range strings.Split(result.Stdout, "\n") {
		if port, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && port > 0 && port <= 65535 {
			ports[port] = true
		}
	}
	return sortedPorts(ports)
}

func sortedPorts(values map[int]bool) []int {
	ports := make([]int, 0, len(values))
	for port := range values {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

// ── Host firewall rule manipulation helpers（与后端无关的编排/归一化） ──

func normalizeHostFirewallRuleRequests(requests []HostFirewallRuleRequest) []HostFirewallRule {
	var rules []HostFirewallRule
	for _, req := range requests {
		action := normalizeHostFirewallAction(req.Action)
		proto := normalizeHostFirewallProtocol(req.Protocol)
		if action == "" {
			action = "allow"
		}
		if proto == "" {
			proto = "tcp"
		}
		start, end := req.PortStart, req.PortEnd
		if end == 0 {
			end = start
		}
		if start < 1 || start > 65535 || end < start || end > 65535 {
			continue
		}
		source := strings.TrimSpace(req.SourceCIDR)
		if source != "" {
			if _, err := netip.ParsePrefix(source); err != nil {
				if addr, addrErr := netip.ParseAddr(source); addrErr == nil {
					if addr.Is4() {
						source = addr.String() + "/32"
					} else {
						source = addr.String() + "/128"
					}
				} else {
					continue
				}
			}
		}
		base := HostFirewallRule{
			Action:     action,
			Protocol:   proto,
			PortStart:  start,
			PortEnd:    end,
			SourceCIDR: source,
			Comment:    strings.TrimSpace(req.Comment),
		}
		if strings.HasPrefix(base.Comment, hostFirewallPanelPrefix) {
			base.ManagedByPanel = true
		}
		if proto == "both" {
			tcpRule := base
			tcpRule.Protocol = "tcp"
			udpRule := base
			udpRule.Protocol = "udp"
			tcpRule.ID = hostFirewallRuleID(tcpRule)
			udpRule.ID = hostFirewallRuleID(udpRule)
			rules = append(rules, tcpRule, udpRule)
			continue
		}
		base.ID = hostFirewallRuleID(base)
		rules = append(rules, base)
	}
	return mergeHostFirewallRules(rules)
}

func buildProtectedHostFirewallRules(sshPorts, panelPorts []int) []HostFirewallRule {
	var rules []HostFirewallRule
	for _, port := range sshPorts {
		rules = append(rules, HostFirewallRule{
			Action:          "allow",
			Protocol:        "tcp",
			PortStart:       port,
			PortEnd:         port,
			Comment:         hostFirewallProtectedSSHPrefix,
			Protected:       true,
			ProtectedReason: "SSH 端口",
			ManagedByPanel:  true,
		})
	}
	for _, port := range panelPorts {
		rules = append(rules, HostFirewallRule{
			Action:          "allow",
			Protocol:        "tcp",
			PortStart:       port,
			PortEnd:         port,
			Comment:         hostFirewallProtectedPanelPrefix,
			Protected:       true,
			ProtectedReason: "面板服务端口",
			ManagedByPanel:  true,
		})
	}
	return mergeHostFirewallRules(rules)
}

func markHostFirewallProtection(rule *HostFirewallRule, sshPorts, panelPorts []int) {
	rule.ManagedByPanel = rule.ManagedByPanel || strings.HasPrefix(rule.Comment, hostFirewallPanelPrefix)
	if strings.HasPrefix(rule.Comment, hostFirewallProtectedSSHPrefix) {
		rule.Protected = true
		rule.ProtectedReason = "SSH 端口"
		return
	}
	if strings.HasPrefix(rule.Comment, hostFirewallProtectedPanelPrefix) {
		rule.Protected = true
		rule.ProtectedReason = "面板服务端口"
		return
	}
	if rule.Action != "allow" || rule.SourceCIDR != "" || rule.Protocol != "tcp" {
		return
	}
	for _, port := range sshPorts {
		if rule.PortStart == port && rule.PortEnd == port {
			rule.Protected = true
			rule.ProtectedReason = "SSH 端口"
			return
		}
	}
	for _, port := range panelPorts {
		if rule.PortStart == port && rule.PortEnd == port {
			rule.Protected = true
			rule.ProtectedReason = "面板服务端口"
			return
		}
	}
}

// ensureHostFirewallRule 编排：去重后委托后端 EnsureRule（§4.2 统一规则表示）。
func ensureHostFirewallRule(rule HostFirewallRule) error {
	if err := validateHostFirewallRule(rule); err != nil {
		return err
	}
	existing, _ := ListHostFirewallRules()
	for _, item := range existing {
		if hostFirewallRuleEquivalent(item, rule) {
			return nil
		}
	}
	return resolveBackend().EnsureRule(rule)
}

// deleteHostFirewallRuleBySpec 委托后端 DeleteRule。
func deleteHostFirewallRuleBySpec(rule HostFirewallRule) error {
	return resolveBackend().DeleteRule(rule)
}

func hostFirewallPortSpec(rule HostFirewallRule) string {
	if rule.PortStart == rule.PortEnd {
		return strconv.Itoa(rule.PortStart)
	}
	return fmt.Sprintf("%d:%d", rule.PortStart, rule.PortEnd)
}

func validateHostFirewallRule(rule HostFirewallRule) error {
	if normalizeHostFirewallAction(rule.Action) == "" {
		return fmt.Errorf("规则动作只支持 allow 或 deny")
	}
	if rule.Protocol != "tcp" && rule.Protocol != "udp" {
		return fmt.Errorf("协议只支持 tcp 或 udp")
	}
	if rule.PortStart < 1 || rule.PortStart > 65535 || rule.PortEnd < rule.PortStart || rule.PortEnd > 65535 {
		return fmt.Errorf("端口范围无效")
	}
	if strings.TrimSpace(rule.SourceCIDR) != "" {
		if _, err := netip.ParsePrefix(strings.TrimSpace(rule.SourceCIDR)); err != nil {
			return fmt.Errorf("来源 CIDR 无效")
		}
	}
	return nil
}

func hostFirewallRuleEquivalent(a, b HostFirewallRule) bool {
	return a.Action == b.Action &&
		a.Protocol == b.Protocol &&
		a.PortStart == b.PortStart &&
		a.PortEnd == b.PortEnd &&
		strings.TrimSpace(a.SourceCIDR) == strings.TrimSpace(b.SourceCIDR)
}

// hostFirewallRuleID 规则稳定 ID（M3：与 mergeHostFirewallRules 去重键同口径，仅含规格、不含备注）。
// 规格等价（action/proto/ports/source）即视为同一条规则，备注为元数据不参与身份判定。
func hostFirewallRuleID(rule HostFirewallRule) string {
	base := fmt.Sprintf("%s|%s|%d|%d|%s", rule.Action, rule.Protocol, rule.PortStart, rule.PortEnd, strings.TrimSpace(rule.SourceCIDR))
	sum := sha1.Sum([]byte(base))
	return hex.EncodeToString(sum[:])[:16]
}

func mergeHostFirewallRules(groups ...[]HostFirewallRule) []HostFirewallRule {
	seen := map[string]HostFirewallRule{}
	for _, group := range groups {
		for _, rule := range group {
			if rule.ID == "" {
				rule.ID = hostFirewallRuleID(rule)
			}
			key := fmt.Sprintf("%s|%s|%d|%d|%s", rule.Action, rule.Protocol, rule.PortStart, rule.PortEnd, strings.TrimSpace(rule.SourceCIDR))
			if old, ok := seen[key]; ok {
				if old.Protected {
					continue
				}
				if rule.Comment == "" {
					rule.Comment = old.Comment
				}
			}
			seen[key] = rule
		}
	}
	result := make([]HostFirewallRule, 0, len(seen))
	for _, rule := range seen {
		rule.ID = hostFirewallRuleID(rule)
		result = append(result, rule)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Protected != result[j].Protected {
			return result[i].Protected
		}
		if result[i].PortStart != result[j].PortStart {
			return result[i].PortStart < result[j].PortStart
		}
		return result[i].Protocol < result[j].Protocol
	})
	return result
}

func normalizeHostFirewallAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "allow", "deny":
		return strings.ToLower(strings.TrimSpace(action))
	default:
		return ""
	}
}

func normalizeHostFirewallProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "tcp":
		return "tcp"
	case "udp":
		return "udp"
	case "both", "any":
		return "both"
	default:
		return ""
	}
}

func shellLikeFields(line string) []string {
	var fields []string
	var b strings.Builder
	inSingle := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '\'':
			inSingle = !inSingle
		case !inSingle && (ch == ' ' || ch == '\t'):
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(ch)
		}
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}

func indexOfString(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

// sortHostFirewallRules 按保护优先 → 端口 → ID 排序（原 ListHostFirewallRules 逻辑）。
func sortHostFirewallRules(rules []HostFirewallRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Protected != rules[j].Protected {
			return rules[i].Protected
		}
		if rules[i].PortStart != rules[j].PortStart {
			return rules[i].PortStart < rules[j].PortStart
		}
		return rules[i].ID < rules[j].ID
	})
}
