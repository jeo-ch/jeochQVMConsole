package firewall

import "fmt"

// ── none 后端（§4.2 探测顺序 3：均无 → none，功能降级，不 panic） ──

type noneBackend struct{}

func (noneBackend) Name() string        { return "none" }
func (noneBackend) DisplayName() string { return "不可用" }
func (noneBackend) Available() bool     { return false }
func (noneBackend) Version() string     { return "" }

func (noneBackend) Active() (bool, error) {
	return false, fmt.Errorf("宿主机防火墙后端不可用")
}

func (noneBackend) Defaults() (string, string, string, error) {
	return "", "", "", fmt.Errorf("宿主机防火墙后端不可用")
}

func (noneBackend) ListRules() ([]HostFirewallRule, error) {
	return []HostFirewallRule{}, nil
}

func (noneBackend) EnsureRule(HostFirewallRule) error {
	return fmt.Errorf("宿主机防火墙后端不可用")
}

func (noneBackend) DeleteRule(HostFirewallRule) error {
	return fmt.Errorf("宿主机防火墙后端不可用")
}

func (noneBackend) Enable(func(int, string)) error {
	return fmt.Errorf("宿主机防火墙后端不可用")
}

func (noneBackend) Disable() error {
	return fmt.Errorf("宿主机防火墙后端不可用")
}
