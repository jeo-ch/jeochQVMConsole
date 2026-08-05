package firewall

// ── 后端注册（§4.2 注册机制，与 arch 包 RegisterProfile 同模式） ──
// init() 注册三个实现；探测函数 DetectHostFirewallBackend 在 backend_detect.go。

func init() {
	RegisterFirewallBackend(ufwBackend{})
	RegisterFirewallBackend(firewalldBackend{})
	RegisterFirewallBackend(noneBackend{})
}

// GetHostFirewallBackendName 返回探测到的后端名（handler/API 使用，§5.3）。
func GetHostFirewallBackendName() string {
	return resolveBackend().Name()
}
