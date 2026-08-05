package firewall

import (
	"os"
	"strings"
	"sync"

	"kvm_console/logger"
	"kvm_console/utils"
)

// ── 后端探测（§4.2） ──

var (
	detectMu        sync.RWMutex
	detectedBackend Backend
	detected        bool
)

// DetectHostFirewallBackend 探测宿主机防火墙后端，结果缓存（sync.Once 语义）。
// 探测顺序（§4.2）：
//  0. 配置覆盖（.env FW_BACKEND=ufw|firewalld|none，config 已加载到环境变量）
//  1. ufw（Debian/Ubuntu）       → command -v ufw
//  2. firewalld（国产 RPM 默认） → command -v firewall-cmd（仅要求存在，不要求 running，#H）
//  3. 均无 → noneBackend（Available=false，功能降级，与现 ufw_available=false 语义一致）
func DetectHostFirewallBackend() Backend {
	detectMu.RLock()
	if detected {
		b := detectedBackend
		detectMu.RUnlock()
		return b
	}
	detectMu.RUnlock()

	detectMu.Lock()
	defer detectMu.Unlock()
	if detected {
		return detectedBackend
	}
	detectedBackend = detectFirewallBackend()
	detected = true
	return detectedBackend
}

// ResetFirewallBackendCache 清除探测缓存（运维补充项 #E：后端失效自动重测 / 面板手动刷新）。
func ResetFirewallBackendCache() {
	detectMu.Lock()
	detected = false
	detectedBackend = nil
	detectMu.Unlock()
}

// resolveBackend 解析当前后端，带一次过期缓存自愈（#E 规则 1）。
// 运行期运维可能手动安装/卸载 ufw、firewalld：缓存的后端命令不可用时
// （如被卸载），自动 ResetFirewallBackendCache 并重新探测一次，
// 使面板在运行期变更后自动恢复准确后端，而非持续返回过期缓存。
// 仅当后端 Available()=false 才触发；firewalld 服务停止（命令仍存在）不算失效（#H）。
// #A6：none 后端（从未探测到 ufw/firewalld 命令）不触发自愈——重复重置缓存
// 只会浪费两次 LookPath，且其结果不会改变；运行期新装防火墙由漂移巡检与
// 面板手动刷新（#E 规则 2，ResetFirewallBackendCache）负责。
func resolveBackend() Backend {
	backend := DetectHostFirewallBackend()
	if backend == nil || backend.Available() {
		return backend
	}
	if backend.Name() == "none" {
		return backend
	}
	ResetFirewallBackendCache()
	return DetectHostFirewallBackend()
}

func detectFirewallBackend() Backend {
	// 步骤 0：配置覆盖（#M）。值非法时告警回退自动探测。
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("FW_BACKEND"))); v != "" {
		switch v {
		case "ufw", "firewalld", "none":
			if b := registeredBackend(v); b != nil {
				return b
			}
			logger.App.Warn("FW_BACKEND=" + v + " 后端未注册，回退自动探测")
		default:
			logger.App.Warn("FW_BACKEND=" + v + " 非法，回退自动探测（仅支持 ufw/firewalld/none）")
		}
	}

	// 步骤 1：ufw
	if b := registeredBackend("ufw"); b != nil && b.Available() {
		return b
	}

	// 步骤 2：firewalld（仅要求命令存在，是否运行由 Active() 判定，#H）
	if b := registeredBackend("firewalld"); b != nil && commandAvailable("firewall-cmd") {
		return b
	}

	// 步骤 3：none
	if b := registeredBackend("none"); b != nil {
		return b
	}
	return noneBackend{}
}

// DetectIPTablesBackend 探测 IP 防火墙后端（v0.8/#O，可靠性判据）。
// 输出含 (nf_tables) → "nf_tables"；含 (legacy) → "legacy"；否则空串。
// legacy → 面板直写 iptables -I FORWARD 1 可靠；nf_tables → 必须依赖 zone/policy 绑定。
func DetectIPTablesBackend() string {
	result := utils.ExecCommand(firewallCommandPath("iptables"), "-V")
	if result.Error != nil {
		return ""
	}
	out := strings.ToLower(result.Stdout)
	switch {
	case strings.Contains(out, "(nf_tables)"):
		return "nf_tables"
	case strings.Contains(out, "(legacy)"):
		return "legacy"
	default:
		return ""
	}
}

// GetFirewallBackendStatus 组装后端状态（§4.2 BackendStatus，供 /system-info 扩展与前端展示）。
func GetFirewallBackendStatus() BackendStatus {
	backend := resolveBackend()
	status := BackendStatus{
		Backend:     backend.Name(),
		BackendName: backend.DisplayName(),
		Available:   backend.Available(),
		Version:     backend.Version(),
		IPBackend:   DetectIPTablesBackend(),
		NM:          detectNMZoneManaged(),
	}
	if !backend.Available() {
		return status
	}
	// #R：捕获 Active/Defaults 的结构化错误码（如 FIREWALLD_NOT_RUNNING），供前端 hint 分支
	var err error
	status.Active, err = backend.Active()
	if code := errorCodeOf(err); code != "" {
		status.ErrorCode = code
	}
	status.DefaultIncoming, status.DefaultOutgoing, status.DefaultRouted, err = backend.Defaults()
	if code := errorCodeOf(err); code != "" {
		status.ErrorCode = code
	}
	rules, err := backend.ListRules()
	if err != nil {
		status.LastError = err.Error()
	} else {
		status.Rules = rules
	}
	status.DockerCompatible, status.DockerCompatibility = probeDockerCompatibility(backend)
	return status
}

// detectNMZoneManaged 判断是否存在 NetworkManager 且上行物理接口归属 NM（#Q）。
func detectNMZoneManaged() bool {
	if !commandAvailable("nmcli") {
		return false
	}
	result := utils.ExecShellQuiet("nmcli -t -f RUNNING general status 2>/dev/null")
	if result.Error != nil || strings.TrimSpace(result.Stdout) != "running" {
		return false
	}
	uplinks := detectUplinkInterfaces()
	if len(uplinks) == 0 {
		return false
	}
	for _, iface := range uplinks {
		r := utils.ExecShellQuiet("nmcli -t -f connection.interface-name connection show 2>/dev/null | grep '" + iface + "' | cut -d: -f1")
		if strings.TrimSpace(r.Stdout) != "" {
			return true
		}
	}
	return false
}

// probeDockerCompatibility 探测 Docker 与当前后端共存兼容性（#D，§5.1 决策 6）。
// docker -p 发布端口经 PREROUTING DNAT → FORWARD → DOCKER 链，不经 INPUT；
// iptables 后端下 qvm-host zone DROP 通常不冲突（Docker 规则先于 firewalld 链），
// nftables 后端下 firewalld --reload 后链注册顺序不保证，需 docker0 绑 trusted 保证。
func probeDockerCompatibility(backend Backend) (bool, string) {
	// 未检测到 Docker 运行时（无 docker0）→ 不构成实际场景，视为兼容
	if !interfaceExists("docker0") {
		return true, "未检测到 Docker 运行时（无 docker0），不涉及端口映射兼容问题"
	}
	if backend.Name() == "ufw" {
		return true, "UFW 不写入 Docker 链，Docker bridge 模式不受面板防火墙约束"
	}
	ipBackend := DetectIPTablesBackend()
	if ipBackend == "legacy" {
		return true, "iptables 后端下 Docker 规则先于 firewalld 链求值，docker -p 发布端口可达"
	}
	// nftables 后端：依赖 docker0 绑 trusted（ACCEPT）保证转发
	var docker0Trusted bool
	_ = backendExec(func() error {
		docker0Trusted = firewalldInterfaceInTrusted("docker0")
		return nil
	})
	if docker0Trusted {
		return true, "docker0 已绑定 trusted zone，nftables 后端下 Docker 转发不受 qvm-host DROP 影响"
	}
	return false, "nftables 后端下 firewalld reload 后链顺序不保证，建议将 docker0 绑定 trusted zone（firewall-cmd --permanent --zone=trusted --add-interface=docker0 && firewall-cmd --reload）"
}

// interfaceExists 判断网络接口是否存在（docker0 等）。
func interfaceExists(name string) bool {
	result := utils.ExecShellQuiet("ip -o link show " + name + " 2>/dev/null")
	return strings.TrimSpace(result.Stdout) != "" && result.Error == nil
}

// firewalldInterfaceInTrusted 判断接口是否已绑定 trusted zone（读操作，#H 降级空）。
func firewalldInterfaceInTrusted(name string) bool {
	for _, item := range firewalldTrustedInterfaces() {
		if strings.TrimSpace(item) == name {
			return true
		}
	}
	return false
}
