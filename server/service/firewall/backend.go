package firewall

import (
	"os/exec"
	"strings"
	"sync"

	"kvm_console/utils"
)

// ── Backend 接口 ──

// Backend 定义宿主机防火墙后端抽象（§4.2，M0 骨架）。
// ufw / firewalld / none 三个实现经 RegisterFirewallBackend 注册，
// DetectHostFirewallBackend 按探测顺序选择（backend_detect.go）。
type Backend interface {
	Name() string        // "ufw" | "firewalld" | "none"
	DisplayName() string // "UFW" | "Firewalld" | "不可用"
	Available() bool     // 命令/服务存在
	Active() (bool, error)
	Defaults() (incoming, outgoing, routed string, err error)
	ListRules() ([]HostFirewallRule, error)
	EnsureRule(rule HostFirewallRule) error
	DeleteRule(rule HostFirewallRule) error
	Enable(progress func(int, string)) error
	Disable() error
	Version() string // #Q：ufw --version / firewall-cmd --version，供 /system-info 上报
}

// BackendStatus 后端探测结果与运行状态（§4.2）。
type BackendStatus struct {
	Backend             string             `json:"backend"`
	BackendName         string             `json:"backend_name"`
	Available           bool               `json:"available"`
	Active              bool               `json:"active"`
	Version             string             `json:"version"` // #Q：firewall-cmd --version / ufw --version
	DefaultIncoming     string             `json:"default_incoming"`
	DefaultOutgoing     string             `json:"default_outgoing"`
	DefaultRouted       string             `json:"default_routed"`
	Rules               []HostFirewallRule `json:"rules"`
	IPBackend           string             `json:"ip_backend"`           // v0.8/#O: legacy | nf_tables | ""
	NM                  bool               `json:"nm_managed"`           // v0.8/#Q：存在 NM 且上行接口归属 NM
	DockerCompatible    bool               `json:"docker_compatible"`    // #D：docker -p 在 qvm-host DROP 下可达性
	DockerCompatibility string             `json:"docker_compatibility"` // #D：探测结论文案 / 放行方案
	ErrorCode           string             `json:"error_code"`           // v0.8/#R: FIREWALLD_NOT_RUNNING 等
	LastError           string             `json:"last_error"`
}

// ── 注册表 ──

var (
	backendMu   sync.Mutex
	backendRegs = map[string]Backend{}
)

// RegisterFirewallBackend 注册后端实现，供 init() 调用（与 arch 包 RegisterProfile 同模式）。
func RegisterFirewallBackend(b Backend) {
	backendMu.Lock()
	defer backendMu.Unlock()
	backendRegs[b.Name()] = b
}

// registeredBackend 返回已注册的后端；未注册返回 nil。
func registeredBackend(name string) Backend {
	backendMu.Lock()
	defer backendMu.Unlock()
	return backendRegs[name]
}

// ── 命令路径固定（v0.8/#N：防 PATH 劫持；§2.3 评审：复用 utils.LookupCmdPath 单一来源缓存） ──

// firewallCommandPath 解析并缓存命令绝对路径；解析失败返回原名（由执行结果判定可用性）。
func firewallCommandPath(name string) string {
	return utils.LookupCmdPath(name)
}

// commandAvailable 判断命令是否存在于 PATH。
func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ── 并发互斥（运维补充项 #B） ──

// backendExec 包裹单条子进程命令执行。
// 锁粒度 = 单次子进程，sync.Mutex 不可重入——严禁在回调内调用其他经 backendExec 加锁的公共方法
// （否则 Enable→EnsureRule 同 goroutine 二次加锁死锁，见 §4.2 锁粒度）。
//
// 回调内【可安全调用】的无锁原语（直接执行子进程，不持有 backendExec 锁）：
//   - execFirewalld / execSystemctl（单条命令执行）
//   - firewalld 内部 helper：active()、firewalldEnsureZoneExists()、firewalldAddRuleToZone()、
//     firewalldDeleteRuleFromZone()、firewalldReload()、firewalldCheckConfig()、firewalldStart()、
//     writeFirewalldZoneAtomically()、restoreFirewalldZone()、captureFirewalldZone()、
//     restoreFirewalldZoneContent()、firewalldBindTrustedInterfaces()、firewalldEnsureForwardPolicy()
//   - ufw 内部 helper：execUFW()
//   - 只读探测：detectUplinkInterfaces()、detectVMBridgeInterfaces()、interfaceExists()、
//     DetectSSHPorts()、DetectPanelPorts()、firewalldZoneTarget()、firewalldVersionAtLeast()
//
// 回调内【严禁调用】的公共方法（均经 backendExec 加锁，重入即死锁）：
//
//	Active()、Defaults()、ListRules()、Enable()、Disable()、PreviewEnable()、EnsureRule()、
//	DeleteRule()，以及 backend_detect.go 中经 backendExec 的探测入口。
//
// 需要锁内复用状态时使用同名小写无锁版（如 firewalldBackend.active()），或拆出纯函数。
func backendExec(fn func() error) error {
	backendMu.Lock()
	defer backendMu.Unlock()
	return fn()
}

// stringsContainsFold 大小写不敏感包含判断。
func stringsContainsFold(text, sub string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(sub))
}
