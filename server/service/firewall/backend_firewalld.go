package firewall

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kvm_console/logger"
	"kvm_console/utils"
)

// ── firewalld 后端（§5.1，M1 里程碑） ──
//
// 语义映射：
//   - 专用 zone `qvm-host`（DROP）只绑定上行接口/源（宿主机入站防护）
//   - VM 桥接口（br-ovs、vpcsw*）绑 trusted（ACCEPT）放行 VM 转发与 dnsmasq 入站（#F）
//   - firewalld ≥0.9 建 policy `qvm-host-forward` 放行 uplink→VM 转发
//   - 持久化：一律 --permanent + --reload；zone 文件原子写入（#P）

const (
	firewalldZoneName      = "qvm-host"
	firewalldZoneFile      = "/etc/firewalld/zones/qvm-host.xml"
	firewalldPolicyName    = "qvm-host-forward"
	firewalldPolicyFile    = "/etc/firewalld/zones/qvm-host-forward.xml"
	firewalldTrustedZone   = "trusted"
	firewalldMinPolicyVer  = "0.9"
	firewalldMinEnableVer  = "0.6"
	firewalldVersionNotSet = ""
)

type firewalldBackend struct{}

func (firewalldBackend) Name() string        { return "firewalld" }
func (firewalldBackend) DisplayName() string { return "Firewalld" }

func (firewalldBackend) Available() bool {
	return commandAvailable("firewall-cmd")
}

func (firewalldBackend) Version() string {
	result := execFirewalld("--version")
	if result.Error != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(result.Stdout, "\n", 2)[0])
}

// Active 服务是否运行（firewall-cmd --state == running，#H）。
func (firewalldBackend) Active() (bool, error) {
	var active bool
	err := backendExec(func() error {
		result := execFirewalld("--state")
		if result.Error != nil {
			return classifyFirewalldExecError(result, &FirewallError{Code: FirewalldNotRunning, Message: "firewalld 服务未运行", Hint: "systemctl start firewalld"})
		}
		active = strings.TrimSpace(result.Stdout) == "running"
		return nil
	})
	if err != nil {
		return false, err
	}
	return active, nil
}

func (firewalldBackend) Defaults() (string, string, string, error) {
	var incoming, outgoing, routed string
	err := backendExec(func() error {
		active, err := firewalldBackend{}.active()
		if err != nil || !active {
			// #H：服务未运行时读操作返回空，不报错
			return nil
		}
		incoming = "deny"
		if firewalldZoneManaged() {
			// 面板曾启用过 qvm-host zone：直接读取其 target 判定入站姿态。
			target := firewalldZoneTarget(firewalldZoneName)
			if target == "ACCEPT" {
				incoming = "allow"
			}
		} else {
			// #A7b：面板从未启用宿主机防火墙（无 qvm-host zone 文件），跳过对
			// qvm-host 的 --get-target（避免 INVALID_ZONE 噪声与 200ms 级延迟），
			// 回退查系统默认 zone 的 target 判定入站，避免误报「allow/deny」。
			defZone := firewalldDefaultZone()
			if defZone != "" {
				if t := firewalldZoneTarget(defZone); t == "ACCEPT" {
					incoming = "allow"
				}
			}
		}
		// 出站默认放行；转发由 trusted 绑定 / policy 保证
		outgoing, routed = "allow", "allow"
		return nil
	})
	return incoming, outgoing, routed, err
}

func (firewalldBackend) ListRules() ([]HostFirewallRule, error) {
	var rules []HostFirewallRule
	err := backendExec(func() error {
		active, err := firewalldBackend{}.active()
		if err != nil || !active {
			return nil // #H
		}
		if !firewalldZoneManaged() {
			return nil // #A7b：未启用过则无面板规则
		}
		for _, line := range firewalldListZonePorts(firewalldZoneName) {
			if rule, ok := parseFirewalldPortLine(line); ok {
				rule.ManagedByPanel = strings.HasPrefix(rule.Comment, hostFirewallPanelPrefix)
				rule.ID = hostFirewallRuleID(rule)
				rules = append(rules, rule)
			}
		}
		for _, line := range firewalldListRichRules(firewalldZoneName) {
			if rule, ok := parseFirewalldRichRuleLine(line); ok {
				rule.ManagedByPanel = strings.HasPrefix(rule.Comment, hostFirewallPanelPrefix)
				rule.ID = hostFirewallRuleID(rule)
				rules = append(rules, rule)
			}
		}
		sshPorts := DetectSSHPorts()
		panelPorts := DetectPanelPorts()
		for i := range rules {
			markHostFirewallProtection(&rules[i], sshPorts, panelPorts)
		}
		sortHostFirewallRules(rules)
		return nil
	})
	return rules, err
}

// active 无锁版 Active 内部实现（供 Defaults/ListRules 在锁内调用，避免重入）。
func (firewalldBackend) active() (bool, error) {
	result := execFirewalld("--state")
	if result.Error != nil {
		return false, classifyFirewalldExecError(result, &FirewallError{Code: FirewalldNotRunning, Message: "firewalld 服务未运行", Hint: "systemctl start firewalld"})
	}
	return strings.TrimSpace(result.Stdout) == "running", nil
}

// classifyFirewalldExecError 将 firewall-cmd 执行结果映射为结构化错误码（#N/#O）：
//   - 挂死超时 → DBUS_ERROR（firewall-cmd 依赖 firewalld D-Bus，timeout 时服务端卡死）
//   - 权限不足 → PERMISSION_DENIED
//   - 其余（服务未运行/命令失败）→ fallback（默认 FIREWALLD_NOT_RUNNING）
func classifyFirewalldExecError(result *utils.CmdResult, fallback error) error {
	if result == nil || result.Error == nil {
		return nil
	}
	if result.ExitCode == -1 && strings.Contains(result.Error.Error(), "执行超时") {
		return &FirewallError{Code: DBUSError, Message: "firewall-cmd 响应超时，firewalld D-Bus 连接可能异常", Hint: "systemctl restart firewalld"}
	}
	if isFirewalldPermissionDenied(result) {
		return &FirewallError{Code: PermissionDenied, Message: "运行 firewall-cmd 权限不足", Hint: "请检查面板运行账户权限（如 root / sudo 配置）"}
	}
	return fallback
}

func isFirewalldPermissionDenied(result *utils.CmdResult) bool {
	text := strings.ToLower(result.Stderr + " " + result.Error.Error())
	return strings.Contains(text, "permission denied") ||
		strings.Contains(text, "operation not permitted") ||
		strings.Contains(text, "access denied") ||
		strings.Contains(text, "eacces")
}

func (firewalldBackend) EnsureRule(rule HostFirewallRule) error {
	return backendExec(func() error {
		if err := validateHostFirewallRule(rule); err != nil {
			return err
		}
		if err := firewalldEnsureZoneExists(); err != nil {
			return err
		}
		if err := firewalldAddRuleToZone(rule); err != nil {
			return err
		}
		return firewalldReload()
	})
}

func (firewalldBackend) DeleteRule(rule HostFirewallRule) error {
	return backendExec(func() error {
		if err := firewalldDeleteRuleFromZone(rule); err != nil {
			return err
		}
		return firewalldReload()
	})
}

// Enable 原子序列（§5.1 决策 5/#C/#P/#L/#F）：
//  1. 渲染完整 qvm-host.xml（默认 DROP + 端口/rich-rule + 绑定上行接口/源 + VM 桥 trusted）
//  2. --check-config 预检语法
//  3. 原子替换 zone 文件 → --reload
//  4. ≥0.9 建 policy qvm-host-forward
//  5. 自检清单（#L）
//
// 任一环节失败整体回滚旧 zone。
func (firewalldBackend) Enable(progress func(int, string)) error {
	return backendExec(func() error {
		// <0.6 显式降级（§5.1 决策 3，P0-2）：不写 zone、不进入绑定序列；读操作仍可用
		if !firewalldVersionAtLeast(firewalldMinEnableVer) {
			return &FirewallError{Code: FirewalldOldVersion, Message: "firewalld 版本过低，面板不启用宿主机防火墙统一管理", Hint: "请升级 firewalld ≥ 0.6 或使用发行版 iptables-service"}
		}
		if progress != nil {
			progress(10, "正在探测上行接口与 VM 桥...")
		}
		uplinks := detectUplinkInterfaces()
		vmBridges := detectVMBridgeInterfaces()
		// 重建 zone 前先捕获已有端口/服务/来源/富规则（C1：防止重建骨架擦除已放行规则）
		preserved := captureFirewalldZone()

		if progress != nil {
			progress(30, "正在渲染 qvm-host zone...")
		}
		// 备份旧 zone 用于回滚
		hadOld := backupFirewalldZone(firewalldZoneFile)
		if err := writeFirewalldZoneAtomically(uplinks, vmBridges); err != nil {
			restoreFirewalldZone(firewalldZoneFile, hadOld)
			return err
		}
		// 恢复捕获的规则到永久配置（写回 zone 文件，随后一次 reload 生效）
		if err := restoreFirewalldZoneContent(preserved); err != nil {
			restoreFirewalldZone(firewalldZoneFile, hadOld)
			return err
		}

		if progress != nil {
			progress(55, "正在校验 zone 配置语法...")
		}
		if err := firewalldCheckConfig(); err != nil {
			restoreFirewalldZone(firewalldZoneFile, hadOld)
			return err
		}

		if progress != nil {
			progress(70, "正在启动并应用 zone...")
		}
		if err := firewalldStart(); err != nil {
			restoreFirewalldZone(firewalldZoneFile, hadOld)
			return err
		}
		if err := firewalldReload(); err != nil {
			restoreFirewalldZone(firewalldZoneFile, hadOld)
			return err
		}

		// VM 桥 + docker0 绑 trusted 无条件执行（§5.1 决策 4/6，0.8.x 同样依赖）
		bindList := append([]string{}, vmBridges...)
		if interfaceExists("docker0") {
			bindList = append(bindList, "docker0")
		}
		if err := firewalldBindTrustedInterfaces(bindList); err != nil {
			logger.App.Warn("绑定接口到 trusted zone 失败", "error", err)
		}
		// 同步 NM 连接 zone（#J）
		for _, uplink := range uplinks {
			syncNMConnectionZone(uplink)
		}

		// ≥0.9 建 policy 放行 uplink→VM 转发（#F）；旧版本返回 FIREWALLD_OLD_VERSION 仅告警（iptables 兜底）
		if err := firewalldEnsureForwardPolicy(); err != nil {
			// policy 失败仅告警，不阻塞（zone 已生效，0.8 路径由 iptables 兜底）
			logger.App.Warn("创建 qvm-host-forward policy 失败", "error", err)
		}

		if progress != nil {
			progress(90, "正在执行启用后自检...")
		}
		if failures := firewalldSelfCheck(uplinks, vmBridges); len(failures) > 0 {
			return fmt.Errorf("启用后自检失败: %s", strings.Join(failures, "; "))
		}

		if progress != nil {
			progress(100, "宿主机防火墙已启用")
		}
		return nil
	})
}

func (firewalldBackend) Disable() error {
	return backendExec(func() error {
		// 只移除面板自己的 zone/policy，不 touch 系统默认 zone（#S2 边界）
		if err := firewalldDeleteZone(firewalldZoneName); err != nil {
			return err
		}
		// policy 仅 ≥0.9 创建过（firewalldEnsureForwardPolicy 版本门控 #H3），
		// <0.9 无 --delete-policy，跳过避免 Disable 半失败（zone 已删但未 reload）。
		if firewalldVersionAtLeast(firewalldMinPolicyVer) {
			if err := firewalldDeletePolicy(firewalldPolicyName); err != nil {
				return err
			}
		}
		if err := firewalldReload(); err != nil {
			return err
		}
		return nil
	})
}

// ── 命令执行 ──
//
// execFirewalld / execSystemctl 为裸执行（不加锁）：锁只在 Backend 公共方法边界
// 由 backendExec 持有一次，内部 helper 禁止再取锁（§4.2 锁粒度，防重入死锁）。

func execFirewalld(args ...string) *utils.CmdResult {
	return utils.ExecCommand(firewallCommandPath("firewall-cmd"), args...)
}

func execSystemctl(args ...string) *utils.CmdResult {
	return utils.ExecCommandWithTimeout(firewallCommandPath("systemctl"), 60*time.Second, args...)
}

// ── zone 文件渲染 ──

type firewalldZoneXML struct {
	XMLName     xml.Name             `xml:"zone"`
	Short       string               `xml:"short"`
	Description string               `xml:"description,omitempty"`
	Target      string               `xml:"target"`
	Interfaces  []firewalldInterface `xml:"interface"`
	Sources     []firewalldSource    `xml:"source"`
	Ports       []firewalldPort      `xml:"port"`
	Services    []string             `xml:"service"`
}

type firewalldInterface struct {
	Name string `xml:"name,attr"`
}

type firewalldSource struct {
	Address string `xml:"address,attr"`
}

type firewalldPort struct {
	Port     string `xml:"port,attr"`
	Protocol string `xml:"protocol,attr"`
}

// writeFirewalldZoneAtomically 渲染并原子写入 qvm-host.xml（#P：tmp → sync → rename）。
func writeFirewalldZoneAtomically(uplinks, vmBridges []string) error {
	xmlDoc := firewalldZoneXML{
		Short:       firewalldZoneName,
		Description: "QVMConsole 宿主机防火墙策略",
		Target:      "DROP",
	}
	for _, iface := range uplinks {
		xmlDoc.Interfaces = append(xmlDoc.Interfaces, firewalldInterface{Name: iface})
	}
	// VM 桥绑 trusted（#F）：通过 firewalld-cmd 在运行时绑定，zone 文件仅记录 qvm-host 自身
	_ = vmBridges
	content := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n" + mustMarshalZoneXML(xmlDoc)
	if err := os.MkdirAll(filepath.Dir(firewalldZoneFile), 0o755); err != nil {
		return err
	}
	tmp := firewalldZoneFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := syncFile(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, firewalldZoneFile); err != nil {
		os.Remove(tmp)
		return err
	}
	restoreconFile(firewalldZoneFile) // #S1：单文件 restorecon（etc_t）
	return nil
}

func mustMarshalZoneXML(z firewalldZoneXML) string {
	data, err := xml.MarshalIndent(z, "", "  ")
	if err != nil {
		return "<zone><target>DROP</target></zone>"
	}
	return string(data)
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func backupFirewalldZone(zoneFile string) bool {
	data, err := os.ReadFile(zoneFile)
	if err != nil {
		return false
	}
	if err := os.WriteFile(zoneFile+".bak", data, 0o644); err != nil {
		return false
	}
	return true
}

func restoreFirewalldZone(zoneFile string, hadOld bool) {
	if !hadOld {
		os.Remove(zoneFile)
		return
	}
	_ = os.Rename(zoneFile+".bak", zoneFile)
}

// ── 基础命令 ──

func firewalldEnsureZoneExists() error {
	// firewalld < 0.7 无 --new-zone（#H2）：直接原子写 zone 文件（Enable 路径同构），
	// 避免 CentOS 7 / Kylin V10（0.6.3）上加规则/启用流程整体失败。
	if !firewalldVersionAtLeast("0.7") {
		if _, err := os.Stat(firewalldZoneFile); err == nil {
			return nil
		}
		if err := writeFirewalldZoneAtomically(nil, nil); err != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "创建 qvm-host zone 失败: " + err.Error(), Hint: "检查 firewalld zone 目录写入权限"}
		}
		// 0.6 起 --permanent 支持离线写入；仅守护进程运行时需 reload 应用，未运行则由启动流程加载
		if active, _ := (firewalldBackend{}).active(); active {
			return firewalldReload()
		}
		return nil
	}
	result := execFirewalld("--permanent", "--new-zone", firewalldZoneName)
	if result.Error != nil {
		if strings.Contains(result.Stderr, "exists") || strings.Contains(result.Stderr, "already") {
			return nil
		}
		return &FirewallError{Code: FirewalldCommandFailed, Message: "创建 qvm-host zone 失败: " + result.Stderr, Hint: "firewall-cmd --get-zones"}
	}
	return nil
}

func firewalldZoneTarget(zone string) string {
	result := execFirewalld("--permanent", "--zone", zone, "--get-target")
	if result.Error != nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// firewalldZoneManaged 判断面板是否曾启用宿主机防火墙（存在 qvm-host zone 文件）。
// 未启用时 zone 查询会触发 INVALID_ZONE，读操作应跳过（#A7b）。
func firewalldZoneManaged() bool {
	_, err := os.Stat(firewalldZoneFile)
	return err == nil
}

// firewalldDefaultZone 返回系统默认 zone 名（#A7：qvm-host 未创建时判定实际入站姿态）。
func firewalldDefaultZone() string {
	result := execFirewalld("--get-default-zone")
	if result.Error != nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

func firewalldListZonePorts(zone string) []string {
	result := execFirewalld("--permanent", "--zone", zone, "--list-ports")
	if result.Error != nil {
		return nil
	}
	var lines []string
	for _, item := range strings.Fields(result.Stdout) {
		lines = append(lines, item)
	}
	return lines
}

func firewalldListRichRules(zone string) []string {
	result := execFirewalld("--permanent", "--zone", zone, "--list-rich-rules")
	if result.Error != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func firewalldAddRuleToZone(rule HostFirewallRule) error {
	// deny 规则（无论有无来源）都必须走 rich-rule：--add-port 只能放行，
	// 若无来源 deny 落 --add-port 会把 deny 变成放行（#H1，语义与 ufw deny 对齐）
	if strings.TrimSpace(rule.SourceCIDR) != "" || rule.Action == "deny" {
		// rich-rule（来源限定 + description 面板标记），单个 argv（防注入 #S3）
		rich := buildFirewalldRichRule(rule)
		result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--add-rich-rule", rich)
		if result.Error != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "添加 rich-rule 失败: " + result.Stderr, Hint: "检查 CIDR 与端口参数"}
		}
		return nil
	}
	port := firewalldPortSpec(rule)
	result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--add-port", port+"/"+rule.Protocol)
	if result.Error != nil {
		return &FirewallError{Code: FirewalldCommandFailed, Message: "添加端口失败: " + result.Stderr, Hint: "检查端口参数"}
	}
	return nil
}

func firewalldDeleteRuleFromZone(rule HostFirewallRule) error {
	if strings.TrimSpace(rule.SourceCIDR) != "" || rule.Action == "deny" {
		rich := buildFirewalldRichRule(rule)
		result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--remove-rich-rule", rich)
		if result.Error != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "删除 rich-rule 失败: " + result.Stderr, Hint: "规则可能已不存在"}
		}
		return nil
	}
	port := firewalldPortSpec(rule)
	result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--remove-port", port+"/"+rule.Protocol)
	if result.Error != nil {
		return &FirewallError{Code: FirewalldCommandFailed, Message: "删除端口失败: " + result.Stderr, Hint: "规则可能已不存在"}
	}
	return nil
}

func firewalldPortSpec(rule HostFirewallRule) string {
	if rule.PortStart == rule.PortEnd {
		return strconv.Itoa(rule.PortStart)
	}
	return fmt.Sprintf("%d-%d", rule.PortStart, rule.PortEnd)
}

// ── 启用前捕获与恢复 zone 内容（C1：重建 qvm-host 骨架时保留已有规则） ──

type firewalldZonePreserved struct {
	ports    []string
	services []string
	sources  []string
	rich     []string
}

// captureFirewalldZone 读取 qvm-host 永久配置中已有的端口/服务/来源/富规则。
// zone 尚不存在时命令报错 → 返回空结构（首次启用场景）。
func captureFirewalldZone() firewalldZonePreserved {
	var p firewalldZonePreserved
	if r := execFirewalld("--permanent", "--zone", firewalldZoneName, "--list-ports"); r.Error == nil {
		p.ports = strings.Fields(r.Stdout)
	}
	if r := execFirewalld("--permanent", "--zone", firewalldZoneName, "--list-services"); r.Error == nil {
		p.services = strings.Fields(r.Stdout)
	}
	if r := execFirewalld("--permanent", "--zone", firewalldZoneName, "--list-sources"); r.Error == nil {
		p.sources = strings.Fields(r.Stdout)
	}
	if r := execFirewalld("--permanent", "--zone", firewalldZoneName, "--list-rich-rules"); r.Error == nil {
		for _, line := range strings.Split(r.Stdout, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				p.rich = append(p.rich, line)
			}
		}
	}
	return p
}

// restoreFirewalldZoneContent 将捕获的规则写回 qvm-host 永久配置（仅修改 zone 文件，未 reload）。
func restoreFirewalldZoneContent(p firewalldZonePreserved) error {
	for _, item := range p.ports {
		result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--add-port", item)
		if result.Error != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "恢复端口规则失败 " + item + ": " + result.Stderr, Hint: "检查端口格式"}
		}
	}
	for _, s := range p.services {
		result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--add-service", s)
		if result.Error != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "恢复服务规则失败 " + s + ": " + result.Stderr, Hint: "检查服务名"}
		}
	}
	for _, s := range p.sources {
		result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--add-source", s)
		if result.Error != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "恢复来源规则失败 " + s + ": " + result.Stderr, Hint: "检查来源地址"}
		}
	}
	for _, r := range p.rich {
		result := execFirewalld("--permanent", "--zone", firewalldZoneName, "--add-rich-rule", r)
		if result.Error != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "恢复富规则失败: " + r + ": " + result.Stderr, Hint: "规则格式无效"}
		}
	}
	return nil
}

// buildFirewalldRichRule 构造 rich-rule 字符串；source/description/port 来自用户输入，
// 整体作为单个 argv 传入（#S3），内部转义单引号。来源可为空（无来源 deny 规则，#H1）。
func buildFirewalldRichRule(rule HostFirewallRule) string {
	action := "accept"
	if rule.Action == "deny" {
		action = "reject"
	}
	port := firewalldPortSpec(rule)
	parts := []string{"rule family=ipv4"}
	if src := strings.TrimSpace(rule.SourceCIDR); src != "" {
		parts = append(parts, "source address="+utils.ShellSingleQuote(src))
	}
	parts = append(parts,
		"port port="+port,
		"protocol="+rule.Protocol,
		action,
	)
	if strings.TrimSpace(rule.Comment) != "" {
		parts = append(parts, "description='"+escapeFirewalldSingleQuote(strings.TrimSpace(rule.Comment))+"'")
	}
	return strings.Join(parts, " ")
}

func escapeFirewalldSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", `'\''`)
}

func firewalldCheckConfig() error {
	result := execFirewalld("--check-config")
	if result.Error != nil {
		return &FirewallError{Code: FirewalldCommandFailed, Message: "zone 配置校验失败: " + result.Stderr, Hint: "检查 qvm-host.xml 语法"}
	}
	return nil
}

func firewalldStart() error {
	result := execSystemctl("start", "firewalld")
	if result.Error != nil {
		return &FirewallError{Code: FirewalldNotRunning, Message: "启动 firewalld 失败: " + result.Stderr, Hint: "systemctl status firewalld"}
	}
	result = execSystemctl("enable", "firewalld")
	if result.Error != nil {
		logger.App.Warn("enable firewalld 失败（不影响本次启用）", "error", result.Error)
	}
	return nil
}

// firewalldReload 执行 --reload 应用永久配置；M2：临时性失败（dbus/daemon 抖动）自动重试，
// 避免 --add-port 已写入永久配置但 reload 失败导致运行态与持久态不一致。
func firewalldReload() error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
		result := execFirewalld("--reload")
		if result.Error == nil {
			return nil
		}
		lastErr = result.Error
		logger.App.Warn("firewalld reload 失败（重试中）", "attempt", attempt+1, "error", result.Error)
	}
	return &FirewallError{Code: FirewalldCommandFailed, Message: "firewalld reload 失败: " + lastErr.Error(), Hint: "systemctl restart firewalld"}
}

func firewalldDeleteZone(zone string) error {
	// firewalld < 0.7 无 --delete-zone（#H2 对称）：直接删除 zone 文件（Enable 亦走文件写入路径）。
	if !firewalldVersionAtLeast("0.7") {
		if _, err := os.Stat(firewalldZoneFile); os.IsNotExist(err) {
			return nil
		}
		if err := os.Remove(firewalldZoneFile); err != nil {
			return &FirewallError{Code: FirewalldCommandFailed, Message: "删除 zone 文件失败: " + err.Error(), Hint: "检查 zone 目录写入权限"}
		}
		return nil
	}
	result := execFirewalld("--permanent", "--delete-zone", zone)
	if result.Error != nil {
		if strings.Contains(result.Stderr, "does not exist") || strings.Contains(result.Stderr, "No zone") {
			return nil
		}
		return &FirewallError{Code: FirewalldCommandFailed, Message: "删除 zone " + zone + " 失败: " + result.Stderr, Hint: "firewall-cmd --get-zones"}
	}
	return nil
}

func firewalldDeletePolicy(policy string) error {
	result := execFirewalld("--permanent", "--delete-policy", policy)
	if result.Error != nil {
		if strings.Contains(result.Stderr, "does not exist") || strings.Contains(result.Stderr, "No policy") {
			return nil
		}
		return &FirewallError{Code: FirewalldCommandFailed, Message: "删除 policy " + policy + " 失败: " + result.Stderr, Hint: "firewall-cmd --get-policies"}
	}
	return nil
}

// ── policy 放行 uplink→VM 转发（#F，≥0.9） ──

// firewalldEnsureForwardPolicy 建 policy qvm-host-forward：ingress=qvm-host、egress=trusted（§5.1 决策 9）。
// 版本 < 0.9 无 policy 能力，返回 FIREWALLD_OLD_VERSION（#O，由调用方按告警处理）。
func firewalldEnsureForwardPolicy() error {
	if !firewalldVersionAtLeast(firewalldMinPolicyVer) {
		return &FirewallError{Code: FirewalldOldVersion, Message: "firewalld 版本低于 0.9，无法创建转发 policy qvm-host-forward", Hint: "升级 firewalld 至 0.9+ 后重试"}
	}
	result := execFirewalld("--permanent", "--new-policy", firewalldPolicyName)
	if result.Error != nil && !strings.Contains(result.Stderr, "exists") && !strings.Contains(result.Stderr, "already") {
		return &FirewallError{Code: FirewalldCommandFailed, Message: "创建 policy 失败: " + result.Stderr, Hint: "firewalld >= 0.9 支持 policy"}
	}
	if err := execFirewalld("--permanent", "--policy", firewalldPolicyName, "--add-ingress-zone", firewalldZoneName); err.Error != nil {
		return &FirewallError{Code: FirewalldCommandFailed, Message: "绑定 ingress zone 失败: " + err.Stderr, Hint: "policy ingress zone"}
	}
	if err := execFirewalld("--permanent", "--policy", firewalldPolicyName, "--add-egress-zone", firewalldTrustedZone); err.Error != nil {
		return &FirewallError{Code: FirewalldCommandFailed, Message: "绑定 egress zone 失败: " + err.Stderr, Hint: "policy egress zone"}
	}
	if err := execFirewalld("--permanent", "--policy", firewalldPolicyName, "--set-target", "ACCEPT"); err.Error != nil {
		return &FirewallError{Code: FirewalldCommandFailed, Message: "设置 policy target 失败: " + err.Stderr, Hint: "policy target"}
	}
	return firewalldReload()
}

// firewalldBindTrustedInterfaces 将 VM 桥 / docker0 绑定到 trusted zone（持久化，§5.1 决策 4/6）。
func firewalldBindTrustedInterfaces(ifaces []string) error {
	var failed []string
	for _, iface := range ifaces {
		if iface == "" {
			continue
		}
		result := execFirewalld("--permanent", "--zone", firewalldTrustedZone, "--add-interface", iface)
		if result.Error != nil && !strings.Contains(result.Stderr, "already") {
			failed = append(failed, iface)
			logger.App.Warn("绑定接口到 trusted zone 失败", "interface", iface, "error", result.Error)
		}
	}
	if len(failed) > 0 {
		return &FirewallError{Code: ZoneNotBound, Message: "接口绑定 trusted zone 失败: " + strings.Join(failed, ", "), Hint: "firewall-cmd --zone=trusted --list-interfaces"}
	}
	return nil
}

// ── 接口探测 ──

// detectUplinkInterfaces 返回默认路由出口接口（ip route show default 口径，与 install.sh detect_default_uplink 一致）。
func detectUplinkInterfaces() []string {
	result := utils.ExecShellQuiet(`ip -o route show default 2>/dev/null | awk '{print $5}' | sort -u`)
	var ifaces []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			ifaces = append(ifaces, name)
		}
	}
	return ifaces
}

// detectVMBridgeInterfaces 返回 VM 桥接口（br-ovs、vpcsw*）。
func detectVMBridgeInterfaces() []string {
	result := utils.ExecShellQuiet(`ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep -E '^(br-ovs|vpcsw)' | sort -u`)
	var ifaces []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			ifaces = append(ifaces, name)
		}
	}
	return ifaces
}

// syncNMConnectionZone 若接口归属 NetworkManager，同步 connection.zone（#J）。
func syncNMConnectionZone(iface string) {
	if !commandAvailable("nmcli") {
		return
	}
	result := utils.ExecShellQuiet(`nmcli -t -f connection.interface-name connection show 2>/dev/null | grep '` + iface + `' | cut -d: -f1 | head -1`)
	conn := strings.TrimSpace(result.Stdout)
	if conn == "" {
		return
	}
	utils.ExecCommand(firewallCommandPath("nmcli"), "connection", "modify", conn, "connection.zone", firewalldZoneName)
}

// ── 自检（#L） ──

func firewalldSelfCheck(uplinks, vmBridges []string) []string {
	var failures []string
	if result := execFirewalld("--state"); result.Error != nil || strings.TrimSpace(result.Stdout) != "running" {
		failures = append(failures, "firewalld 未运行")
	}
	if target := firewalldZoneTarget(firewalldZoneName); target != "DROP" {
		failures = append(failures, "qvm-host zone target 不是 DROP")
	}
	if len(uplinks) == 0 {
		failures = append(failures, "未检测到上行接口")
	}
	trusted := firewalldTrustedInterfaces()
	checkList := append([]string{}, vmBridges...)
	if interfaceExists("docker0") {
		checkList = append(checkList, "docker0")
	}
	for _, iface := range checkList {
		if iface == "" {
			continue
		}
		if !stringsContainsFold(strings.Join(trusted, " "), iface) {
			failures = append(failures, iface+" 未绑定 trusted zone")
		}
	}
	// 受保护端口（SSH/面板）必须在启用后仍放行（C1：防止重建 zone 时规则丢失）
	zoneRules := ""
	if r := execFirewalld("--permanent", "--zone", firewalldZoneName, "--list-ports"); r.Error == nil {
		zoneRules += r.Stdout
	}
	if r := execFirewalld("--permanent", "--zone", firewalldZoneName, "--list-rich-rules"); r.Error == nil {
		zoneRules += " " + r.Stdout
	}
	for _, port := range append(DetectSSHPorts(), DetectPanelPorts()...) {
		tcpPort := strconv.Itoa(port) + "/tcp"
		if !strings.Contains(zoneRules, tcpPort) && !strings.Contains(zoneRules, `port="`+strconv.Itoa(port)+`"`) {
			failures = append(failures, "受保护端口 "+tcpPort+" 未在 qvm-host 放行")
		}
	}
	return failures
}

func firewalldTrustedInterfaces() []string {
	result := execFirewalld("--permanent", "--zone", firewalldTrustedZone, "--list-interfaces")
	if result.Error != nil {
		return nil
	}
	return strings.Fields(result.Stdout)
}

// ── firewalld 版本 ──

func firewalldVersionAtLeast(min string) bool {
	result := execFirewalld("--version")
	if result.Error != nil {
		return false
	}
	version := strings.TrimSpace(strings.SplitN(result.Stdout, "\n", 2)[0])
	return compareFirewalldVersion(version, min) >= 0
}

func compareFirewalldVersion(a, b string) int {
	parse := func(s string) []int {
		var parts []int
		for _, p := range strings.Split(s, ".") {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				n = 0
			}
			parts = append(parts, n)
		}
		return parts
	}
	pa, pb := parse(a), parse(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var va, vb int
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ── firewalld 输出解析 ──

func parseFirewalldPortLine(line string) (HostFirewallRule, bool) {
	// "5900-5999/tcp" → PortStart=5900, PortEnd=5999, Protocol=tcp
	line = strings.TrimSpace(line)
	proto := ""
	if strings.Contains(line, "/") {
		parts := strings.SplitN(line, "/", 2)
		line = parts[0]
		proto = normalizeHostFirewallProtocol(parts[1])
	}
	start, end, ok := parseHostFirewallPortRange(strings.ReplaceAll(line, "-", ":"))
	if !ok {
		return HostFirewallRule{}, false
	}
	return HostFirewallRule{
		Action:    "allow",
		Protocol:  proto,
		PortStart: start,
		PortEnd:   end,
	}, true
}

func parseFirewalldRichRuleLine(line string) (HostFirewallRule, bool) {
	rule := HostFirewallRule{Action: "allow", SourceCIDR: ""}
	lower := strings.ToLower(line)
	// 动作 token 可能位于行尾（无 description 的规则），须匹配「token 前后空白或串首/串尾」，
	// 不能依赖两侧空白（如 " reject " 漏掉行尾 "reject"）。
	switch {
	case regexpMustCompile(`(?:^|\s)(?:reject|drop)(?:\s|$)`).MatchString(lower):
		rule.Action = "deny"
	case regexpMustCompile(`(?:^|\s)accept(?:\s|$)`).MatchString(lower):
		rule.Action = "allow"
	default:
		return HostFirewallRule{}, false
	}
	if m := regexpMustCompile(`port\s+port="(\d+)(?:[-:](\d+))?"`).FindStringSubmatch(line); len(m) > 0 {
		start, _ := strconv.Atoi(m[1])
		end := start
		if len(m) > 2 && m[2] != "" {
			end, _ = strconv.Atoi(m[2])
		}
		rule.PortStart, rule.PortEnd = start, end
	} else {
		return HostFirewallRule{}, false
	}
	if m := regexpMustCompile(`protocol="(\w+)"`).FindStringSubmatch(line); len(m) > 0 {
		rule.Protocol = normalizeHostFirewallProtocol(m[1])
	} else {
		rule.Protocol = "both"
	}
	if m := regexpMustCompile(`source address="([^"]+)"`).FindStringSubmatch(line); len(m) > 0 {
		rule.SourceCIDR = m[1]
	}
	if m := regexpMustCompile(`description='([^']*)'`).FindStringSubmatch(line); len(m) > 0 {
		rule.Comment = m[1]
	}
	if rule.Action == "" || rule.PortStart == 0 {
		return HostFirewallRule{}, false
	}
	return rule, true
}

func restoreconFile(path string) {
	if !commandAvailable("restorecon") {
		return
	}
	result := utils.ExecCommand(firewallCommandPath("restorecon"), path)
	if result.Error != nil {
		logger.App.Warn("restorecon %s 失败: %v", path, result.Error)
	}
}
