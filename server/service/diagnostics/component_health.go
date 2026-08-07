package diagnostics

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	archsvc "kvm_console/service/arch"
	vmwatchdogpkg "kvm_console/service/vmwatchdog"
	"kvm_console/utils"
)

// ── 组件版本健康度（§5.11.5 / M7.2） ──
//
// 构建期 build.sh 将 compat-manifest.json 写入本目录（§5.11.3），编译期 go:embed 嵌入，
// 作为组件版本阈值的「权威来源」；缺失/非法时回退空阈值，仅展示当前版本不报错（§8 manifest 缺失回退）。

//go:embed compat-manifest.json
var compatManifestJSON []byte

// compatRequirement 单个组件的版本阈值（对应 manifest 的 system_requirements 条目）
type compatRequirement struct {
	MinVersionAMD64 string   `json:"min_version_amd64"`
	MinVersionARM64 string   `json:"min_version_arm64"`
	MinVersion      string   `json:"min_version"`
	Recommended     string   `json:"recommended"`
	Category        string   `json:"category"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	Alternatives    []string `json:"alternatives"`
	Whitelist       []string `json:"whitelist"`
	Optional        bool     `json:"optional"`
}

// MinVersionFor 返回当前架构的最低版本（glibc 按架构区分；其余走通用 min_version）
func (r compatRequirement) MinVersionFor(goArch string) string {
	if r.MinVersionAMD64 != "" || r.MinVersionARM64 != "" {
		if archsvc.IsAarch64Arch(goArch) {
			return r.MinVersionARM64
		}
		return r.MinVersionAMD64
	}
	return r.MinVersion
}

// OsSupportEntry 单个发行版的支持等级与认证硬件（对应 manifest 的 os_compat 条目，M8.11/§14 P3-11）
type OsSupportEntry struct {
	Firewall          string   `json:"firewall"`
	GLIBC             string   `json:"glibc"`
	RecommendedTier   string   `json:"recommended_tier"`
	SupportLevel      string   `json:"support_level"` // S=A=官方全量回归、A=核心功能回归、B=社区自测、C=理论兼容
	CertifiedHardware []string `json:"certified_hardware"`
}

type compatManifest struct {
	SystemRequirements map[string]compatRequirement `json:"system_requirements"`
	OsCompat           map[string]OsSupportEntry    `json:"os_compat"`
}

// SupportLevelMeta 支持等级展示元数据（S/A/B/C → 中文描述 + 提示语）
type SupportLevelMeta struct {
	Level       string `json:"level"` // S | A | B | C
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SupportLevelMetas S/A/B/C 等级元数据（§14 P3-11 定义）
var SupportLevelMetas = []SupportLevelMeta{
	{Level: "S", Name: "官方全量回归", Description: "官方完整回归测试，生产推荐"},
	{Level: "A", Name: "核心功能回归", Description: "核心功能回归验证，可用于生产"},
	{Level: "B", Name: "社区自测", Description: "社区自测通过，谨慎用于生产"},
	{Level: "C", Name: "理论兼容", Description: "仅理论兼容，生产请升级到认证基线"},
}

// GetOsSupport 返回 compat-manifest 的 os_compat（发行版 → 支持等级），解析失败返回空 map。
func GetOsSupport() map[string]OsSupportEntry {
	m := loadManifest()
	if m == nil || m.OsCompat == nil {
		return map[string]OsSupportEntry{}
	}
	return m.OsCompat
}

// DetectCurrentOsSupport 识别当前系统对应的发行版支持等级（C1/§14 P3-11 增强）：// 按 /etc/os-release 的 ID / ID_LIKE 匹配 manifest os_compat 键；返回 nil 表示未命中。
func DetectCurrentOsSupport() *OsSupportEntry {
	osSupport := GetOsSupport()
	if len(osSupport) == 0 {
		return nil
	}
	rel := archsvc.ReadOSRelease()
	if rel.ID == "" {
		return nil
	}

	// 候选键：ID 自身 + ID_LIKE（空格分隔的继承链）
	candidates := []string{strings.ToLower(rel.ID)}
	for _, like := range strings.Fields(strings.ReplaceAll(rel.IDLike, ",", " ")) {
		candidates = append(candidates, strings.ToLower(like))
	}
	// ID 含版本号（如 "kylin"）时追加精确键（如 "kylin-v10-server"）
	if rel.Version != "" {
		candidates = append(candidates,
			strings.ToLower(rel.ID)+"-"+strings.ToLower(strings.Fields(rel.Version)[0]))
	}

	for _, key := range candidates {
		if entry, ok := osSupport[key]; ok {
			return &entry
		}
	}
	// 通配回退：PRETTY_NAME 子串匹配（如 "银河麒麟 V10 SP3" 未必命中精确键）
	for key, entry := range osSupport {
		if strings.Contains(strings.ToLower(rel.Pretty), strings.ToLower(key)) {
			return &entry
		}
	}
	return nil
}

// CurrentOsReleaseName 返回当前系统的 PRETTY_NAME（不存在时回退 ID），供前端展示「当前系统」。
func CurrentOsReleaseName() string {
	rel := archsvc.ReadOSRelease()
	if rel.Pretty != "" {
		return rel.Pretty
	}
	return rel.ID
}

// ComponentHealthItem 单个组件健康度条目（§5.11.5 / §8 字段契约）
type ComponentHealthItem struct {
	Component          string `json:"component"`
	Category           string `json:"category"`
	Status             string `json:"status"` // healthy | warning | critical | info
	CurrentVersion     string `json:"current_version"`
	RequiredVersion    string `json:"required_version"`
	RecommendedVersion string `json:"recommended_version"`
	Message            string `json:"message"`
	UpgradeHint        string `json:"upgrade_hint"`
}

// ComponentHealth 组件版本健康度聚合结果
type ComponentHealth struct {
	Overall   string                `json:"overall"` // healthy | warning | critical
	LastCheck string                `json:"last_check"`
	Items     []ComponentHealthItem `json:"items"`
}

// ── 探测缓存（sync.Once 语义，与 DetectHostFirewallBackend 同口径；前端「刷新」触发 Reset） ──

// healthCacheTTL 组件健康度缓存有效期（§4.3 评审）：与 advice.go 的 10min TTL 类似，
// 组件升级后无需手动刷新也能在 TTL 内自动反映，避免前端展示长期陈旧。
const healthCacheTTL = 30 * time.Minute

var (
	healthMu       sync.RWMutex
	healthCached   bool
	healthCachedAt time.Time
	healthValue    ComponentHealth
)

// GetComponentHealth 探测并缓存组件版本健康度（§5.11.5）。
// 首次调用触发一次完整探测，之后 TTL 内命中缓存；不设置高频轮询，避免频繁调用 --version 命令。
func GetComponentHealth() ComponentHealth {
	healthMu.RLock()
	if healthCached && time.Since(healthCachedAt) < healthCacheTTL {
		h := healthValue
		healthMu.RUnlock()
		return h
	}
	healthMu.RUnlock()

	healthMu.Lock()
	defer healthMu.Unlock()
	if healthCached && time.Since(healthCachedAt) < healthCacheTTL {
		return healthValue
	}
	healthValue = detectComponentHealth()
	healthCached = true
	healthCachedAt = time.Now()
	return healthValue
}

// ResetComponentHealthCache 清除组件健康度缓存（§5.11.5 前端「刷新」/ 运维手动刷新触发重新探测）。
func ResetComponentHealthCache() {
	healthMu.Lock()
	healthCached = false
	healthMu.Unlock()
}

// refreshCooldown 刷新冷却期：前端高频点击「刷新」时复用已探测结果（H3：避免低配国产机上
// 每次交互都触发 19 组件 × 子进程的全量探测）。
const refreshCooldown = 5 * time.Second

var (
	refreshMu   sync.Mutex
	lastRefresh time.Time
)

// RefreshComponentHealth 强制重新探测组件健康度。
// 冷却期内（refreshCooldown）直接返回当前缓存，冷却结束后重置缓存并触发一次全量探测
// （探测由 GetComponentHealth 的双检锁单飞合并并发请求）。
func RefreshComponentHealth() ComponentHealth {
	refreshMu.Lock()
	if !lastRefresh.IsZero() && time.Since(lastRefresh) < refreshCooldown {
		refreshMu.Unlock()
		return GetComponentHealth()
	}
	lastRefresh = time.Now()
	refreshMu.Unlock()

	healthMu.Lock()
	healthCached = false
	healthMu.Unlock()
	return GetComponentHealth()
}

// ── 探测实现 ──

var versionRe = regexp.MustCompile(`[0-9]+(\.[0-9]+){1,2}`)

func validVersionToken(s string) bool {
	// 锚定整串匹配：current 必须本身就是完整语义化版本 token（如 "6.2.0"），
	// 避免 "v6.0" / "6.0-beta" 等带前后缀的串被误判为合法版本。
	return versionRe.MatchString(s) && versionRe.FindString(s) == s
}

// detectVersion 执行命令并返回首个语义化版本 token。
// 命令路径走 utils.LookupCmdPath 缓存（§2.3 评审：与 firewall 共用，避免每次探测重复 LookPath）。
// 返回语义（§8 验收「版本解析失败降级」）：
//   - ""        → 命令不存在（组件未安装）
//   - "unknown" → 命令存在且成功执行，但输出中解析不到版本 token（解析失败降级 warning，不误报 critical）
func detectVersion(name string, args ...string) string {
	path := utils.LookupCmdPath(name)
	if path == name {
		// LookupCmdPath 解析失败返回原名 → 命令不存在
		return ""
	}
	res := utils.ExecCommandQuietWithTimeout(path, 5*time.Second, args...)
	if res.Error != nil {
		return ""
	}
	if ver := versionRe.FindString(res.Stdout + "\n" + res.Stderr); ver != "" {
		return ver
	}
	return "unknown"
}

// detectGlibcVersion 探测 glibc 版本（§4.5 评审：统一走 utils.DetectGlibcVersion 单一来源，
// 与 firewall/advice.go 共用，不再各自实现）。
func detectGlibcVersion() string {
	return utils.DetectGlibcVersion()
}

// compareVersions 比较 "a.b[.c]" 版本；a<b 返回负、a>b 返回正、相等返回 0。
func compareVersions(a, b string) int {
	pa := versionNums(a)
	pb := versionNums(b)
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

func versionNums(s string) []int {
	var parts []int
	for _, p := range strings.Split(strings.TrimSpace(s), ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}

// buildItem 根据探测结果与阈值生成条目（统一三态判定：critical < 最低 < warning < 推荐 < healthy）
func buildItem(comp, cat string, req compatRequirement, current, message, hint string) ComponentHealthItem {
	minVer := req.MinVersionFor(runtime.GOARCH)
	item := ComponentHealthItem{
		Component:          comp,
		Category:           cat,
		CurrentVersion:     current,
		RequiredVersion:    minVer,
		RecommendedVersion: req.Recommended,
		Message:            message,
		UpgradeHint:        hint,
	}
	if minVer == "any" {
		item.RequiredVersion = ""
	}

	if current == "" {
		if req.Optional {
			item.Status = "info"
			if item.Message == "" {
				item.Message = "可选组件缺失，不影响核心功能"
			}
		} else {
			item.Status = "warning"
			if item.Message == "" {
				item.Message = "组件未安装，相关功能可能受限"
			}
		}
		return item
	}

	// 版本格式异常 → 降级 warning 不报 critical（§8：运行期版本解析失败降级）
	if !validVersionToken(current) {
		item.Status = "warning"
		item.Message = "无法解析版本，请人工确认"
		return item
	}

	if minVer != "" && minVer != "any" && compareVersions(current, minVer) < 0 {
		item.Status = "critical"
		if item.Message == "" {
			item.Message = "低于最低要求，相关功能可能不可用"
		}
		return item
	}
	if req.Recommended != "" && req.Recommended != "any" && compareVersions(current, req.Recommended) < 0 {
		item.Status = "warning"
		if item.Message == "" {
			item.Message = "可运行，但低于推荐版本"
		}
		return item
	}
	item.Status = "healthy"
	if item.Message == "" {
		item.Message = "版本满足要求"
	}
	return item
}

// detectComponentHealth 执行完整探测（§5.11.2 清单 20 key，按架构省略另一 edk2 条目 → 每架构 19 项）
func detectComponentHealth() ComponentHealth {
	manifest := loadManifest()
	reqs := manifest.SystemRequirements
	reqOf := func(name string) compatRequirement {
		if reqs != nil {
			if r, ok := reqs[name]; ok {
				return r
			}
		}
		return compatRequirement{}
	}

	var items []ComponentHealthItem
	// 归一化宿主机二进制架构（runtime.GOARCH: amd64/arm64/riscv64 → 模块规范常量）。
	arch := archsvc.NormalizeArch(runtime.GOARCH)

	// ── 核心必装 ──

	// glibc（必装且永不缺失；阈值未加载时仅展示当前版本）
	req := reqOf("glibc")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	glibc := detectGlibcVersion()
	if glibc == "" {
		items = append(items, buildItem("glibc", "core", req, "", "无法解析版本，请人工确认", ""))
	} else {
		items = append(items, buildItem("glibc", "core", req, glibc, "与所选二进制档位匹配（见选优/构建档位）", ""))
	}

	// qemu-kvm（架构专属命令）
	qemuCmd := "qemu-kvm"
	if archsvc.IsAarch64Arch(arch) {
		qemuCmd = "qemu-system-aarch64"
	} else if _, err := exec.LookPath("qemu-system-x86_64"); err == nil {
		qemuCmd = "qemu-system-x86_64"
	}
	req = reqOf("qemu-kvm")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	items = append(items, buildItem("qemu-kvm", "core", req, detectVersion(qemuCmd, "--version"), "", pkgInstallHint("qemu-kvm")))

	req = reqOf("qemu-img")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	items = append(items, buildItem("qemu-img", "core", req, detectVersion("qemu-img", "--version"), "", pkgInstallHint("qemu-img")))

	// libvirt（libvirtd 优先，回退 virsh）
	req = reqOf("libvirt")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	libvirtVer := detectVersion("libvirtd", "--version")
	if libvirtVer == "" {
		libvirtVer = detectVersion("virsh", "--version")
	}
	items = append(items, buildItem("libvirt", "core", req, libvirtVer, "", pkgInstallHint("libvirt")))

	req = reqOf("openvswitch")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	items = append(items, buildItem("openvswitch", "core", req, detectVersion("ovs-vsctl", "--version"), "", pkgInstallHint("openvswitch")))

	req = reqOf("dnsmasq")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	items = append(items, buildItem("dnsmasq", "core", req, detectVersion("dnsmasq", "--version"), "", pkgInstallHint("dnsmasq")))

	// firewalld（RPM 系后端；命令不存在视为不适用，不误报）
	req = reqOf("firewalld")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		items = append(items, buildItem("firewalld", "core", req, detectVersion("firewall-cmd", "--version"), "", pkgInstallHint("firewalld")))
	} else {
		items = append(items, ComponentHealthItem{
			Component: "firewalld", Category: "core", Status: "info",
			RequiredVersion: req.MinVersionFor(arch), RecommendedVersion: req.Recommended,
			Message: "当前发行版未使用 firewalld 后端（RPM 系）",
		})
	}

	// ufw（Debian 系后端；命令不存在视为不适用）
	req = reqOf("ufw")
	if req.Category == "" {
		req = compatRequirement{Category: "core"}
	}
	if _, err := exec.LookPath("ufw"); err == nil {
		items = append(items, buildItem("ufw", "core", req, detectVersion("ufw", "--version"), "", pkgInstallHint("ufw")))
	} else {
		items = append(items, ComponentHealthItem{
			Component: "ufw", Category: "core", Status: "info",
			RequiredVersion: req.MinVersionFor(arch), RecommendedVersion: req.Recommended,
			Message: "当前发行版未使用 ufw 后端（Debian 系）",
		})
	}

	// ── 磁盘/镜像/初始化 ──

	req = reqOf("virt-install")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	items = append(items, buildItem("virt-install", "disk", req, detectVersion("virt-install", "--version"), "", pkgInstallHint("virt-install")))

	req = reqOf("virt-customize")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	items = append(items, buildItem("virt-customize", "disk", req, detectVersion("virt-customize", "--version"), "", pkgInstallHint("libguestfs-tools")))

	req = reqOf("guestfish")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	items = append(items, buildItem("guestfish", "disk", req, detectVersion("guestfish", "--version"), "", pkgInstallHint("libguestfs-tools")))

	// genisoimage / xorriso / mkisofs（任一可用即可）
	req = reqOf("genisoimage")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	isoTool, isoVer := "", ""
	for _, alt := range []string{"genisoimage", "xorriso", "mkisofs"} {
		if v := detectVersion(alt, "--version"); v != "" {
			isoTool, isoVer = alt, v
			break
		}
	}
	if isoVer != "" {
		items = append(items, buildItem("genisoimage", "disk", req, isoVer, "已安装替代工具 "+isoTool, ""))
	} else {
		items = append(items, buildItem("genisoimage", "disk", req, "", "genisoimage/xorriso/mkisofs 均未安装，Windows ConfigDrive ISO 生成不可用", pkgInstallHint("xorriso")))
	}

	req = reqOf("growpart")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	items = append(items, buildItem("growpart", "disk", req, detectVersion("growpart", "--version"), "", pkgInstallHint("growpart")))

	req = reqOf("ntfsresize")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	items = append(items, buildItem("ntfsresize", "disk", req, detectVersion("ntfsresize", "--version"), "", pkgInstallHint("ntfsresize")))

	// edk2 固件（架构专属：amd64 → edk2-ovmf，arm64 → edk2-aarch64）
	req = reqOf("edk2-ovmf")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	if archsvc.IsX86Arch(arch) {
		// 与 arch/x86_64.go UEFI 路径一致（优先 4M 变体），覆盖 Debian(/usr/share/OVMF)
		// 与 openEuler/RHEL9+(/usr/share/edk2/ovmf) 两种布局，避免已装却误报缺失
		ovmf := ""
		for _, f := range []string{
			"/usr/share/OVMF/OVMF_CODE_4M.fd", "/usr/share/OVMF/OVMF_CODE.fd",
			"/usr/share/edk2/ovmf/OVMF_CODE.fd",
		} {
			if fileExists(f) {
				ovmf = f
				break
			}
		}
		if ovmf != "" {
			items = append(items, ComponentHealthItem{Component: "edk2-ovmf", Category: "disk", Status: "healthy", CurrentVersion: "已安装", Message: "UEFI 固件可用（" + ovmf + "）"})
		} else {
			items = append(items, ComponentHealthItem{Component: "edk2-ovmf", Category: "disk", Status: "warning", Message: "UEFI 固件缺失，UEFI 引导类型 VM 创建不可用", UpgradeHint: pkgInstallHint("edk2-ovmf")})
		}
	}

	req = reqOf("edk2-aarch64")
	if req.Category == "" {
		req = compatRequirement{Category: "disk"}
	}
	if archsvc.IsAarch64Arch(arch) {
		// 与 arch/aarch64.go UEFI 路径一致（含 Debian arm64 的 qemu-efi-aarch64）
		aavmf := ""
		for _, f := range []string{"/usr/share/AAVMF/AAVMF_CODE.fd", "/usr/share/edk2/aarch64/AAVMF_CODE.fd", "/usr/share/qemu-efi-aarch64/QEMU_EFI.fd"} {
			if fileExists(f) {
				aavmf = f
				break
			}
		}
		if aavmf != "" {
			items = append(items, ComponentHealthItem{Component: "edk2-aarch64", Category: "disk", Status: "healthy", CurrentVersion: "已安装", Message: "UEFI 固件可用（" + aavmf + "）"})
		} else {
			items = append(items, ComponentHealthItem{Component: "edk2-aarch64", Category: "disk", Status: "warning", Message: "UEFI 固件缺失，UEFI 引导类型 VM 创建不可用", UpgradeHint: pkgInstallHint("edk2-aarch64")})
		}
	}

	// ── 诊断/扩展（可选） ──

	req = reqOf("tcpdump")
	if req.Category == "" {
		req = compatRequirement{Category: "diag"}
	}
	items = append(items, buildItem("tcpdump", "diag", req, detectVersion("tcpdump", "--version"), "", pkgInstallHint("tcpdump")))

	req = reqOf("tc")
	if req.Category == "" {
		req = compatRequirement{Category: "diag"}
	}
	items = append(items, buildItem("tc", "diag", req, detectVersion("tc", "-V"), "", pkgInstallHint("tc")))

	// kvm_stat（可选，无统一 --version，按命令存在判定）
	req = reqOf("kvm_stat")
	if req.Category == "" {
		req = compatRequirement{Category: "diag", Optional: true}
	}
	if _, err := exec.LookPath("kvm_stat"); err == nil {
		items = append(items, ComponentHealthItem{Component: "kvm_stat", Category: "diag", Status: "healthy", CurrentVersion: "可用", Message: "热迁移辅助指标可用"})
	} else {
		items = append(items, ComponentHealthItem{Component: "kvm_stat", Category: "diag", Status: "info", Message: "可选组件缺失，热迁移辅助指标不可用"})
	}

	// cpu_vendor（P0-1 / M8.1）：国产 CPU 厂商白名单判定；不在白名单 → warning「未认证厂商」
	// （§5.11.3 manifest cpu_vendor_whitelist；SMTXOS 对海光有显式启动参数分支，见 §5.8 precheck_domestic）
	cpuVendor := archsvc.DetectCPUVendor()
	cpuVendorItem := ComponentHealthItem{
		Component:      "cpu_vendor",
		Category:       "core",
		Status:         "healthy",
		CurrentVersion: cpuVendor,
		Message:        "CPU 厂商已认证",
	}
	req = reqOf("cpu_vendor")
	if len(req.Whitelist) > 0 {
		whitelisted := false
		for _, v := range req.Whitelist {
			if v == cpuVendor {
				whitelisted = true
				break
			}
		}
		if !whitelisted {
			cpuVendorItem.Status = "warning"
			cpuVendorItem.Message = "未认证 CPU 厂商，如遇虚拟化异常请联系技术支持确认硬件兼容性"
		}
	}
	items = append(items, cpuVendorItem)

	// hugepages（M8.9 / §14 P2-9）：宿主机内存 ≥128GB 且未启用大页 → warning「建议开启大页」
	hp := vmwatchdogpkg.CheckHugePagesAdvice()
	hpItem := ComponentHealthItem{
		Component:      "hugepages",
		Category:       "core",
		Status:         "healthy",
		CurrentVersion: fmt.Sprintf("%dGB / %d 页", hp.MemTotalGB, hp.TotalPages),
		Message:        "大页配置正常",
	}
	if hp.Suggested {
		hpItem.Status = "warning"
		hpItem.Message = hp.Message
	}
	items = append(items, hpItem)

	// kysec（麒麟 KYSEC 安全机制，Kylin 专项）：仅麒麟 V10+ 命中探测点上报，非麒麟不出现该条目。
	// 启用 → warning 提示放行建议（KYSEC 强制访问控制可能限制内核模块加载与 /dev/kvm 访问）。
	// 探测点：kysec_ctl 命令 / sysfs / procfs / 配置目录（见 server/service/arch/kysec.go）。
	if kysecEnabled, kysecDetail := archsvc.DetectKYSEC(); kysecEnabled {
		items = append(items, ComponentHealthItem{
			Component:      "kysec",
			Category:       "diag",
			Status:         "warning",
			CurrentVersion: "enabled",
			Message:        "麒麟 KYSEC 安全机制启用（" + kysecDetail + "），若 KVM 无法启用或虚拟机启动异常请用 kysec_ctl 放行 qemu/libvirt 相关策略",
		})
	}

	// ── 汇总 ──
	overall := "healthy"
	for _, it := range items {
		switch it.Status {
		case "critical":
			overall = "critical"
		case "warning":
			if overall == "healthy" {
				overall = "warning"
			}
		}
	}

	return ComponentHealth{
		Overall:   overall,
		LastCheck: time.Now().Format(time.RFC3339),
		Items:     items,
	}
}

// loadManifest 解析嵌入的 compat-manifest.json；失败返回 nil（阈值回退为空，仅展示当前版本）。
func loadManifest() *compatManifest {
	m := &compatManifest{}
	if err := json.Unmarshal(compatManifestJSON, m); err != nil {
		return nil
	}
	return m
}

// componentPkgMap 组件 → 各发行版包名（[apt, dnf]），与 §5.11.2 表格及 install.sh RPM_PKG_MAP 对齐
var componentPkgMap = map[string][2]string{
	"qemu-kvm":       {"qemu-system-x86", "qemu-kvm"},
	"qemu-img":       {"qemu-utils", "qemu-img"},
	"libvirt":        {"libvirt-daemon-system", "libvirt"},
	"openvswitch":    {"openvswitch-switch", "openvswitch"},
	"dnsmasq":        {"dnsmasq-base", "dnsmasq"},
	"firewalld":      {"firewalld", "firewalld"},
	"ufw":            {"ufw", "firewalld"},
	"virt-install":   {"virtinst", "virt-install"},
	"virt-customize": {"libguestfs-tools", "libguestfs-tools"},
	"guestfish":      {"libguestfs-tools", "libguestfs-tools"},
	"genisoimage":    {"genisoimage", "genisoimage"},
	"xorriso":        {"xorriso", "xorriso"},
	"growpart":       {"cloud-guest-utils", "cloud-utils-growpart"},
	"ntfsresize":     {"ntfs-3g", "ntfs-3g"},
	"edk2-ovmf":      {"ovmf", "edk2-ovmf"},
	"edk2-aarch64":   {"edk2-aarch64", "edk2-aarch64"},
	"tcpdump":        {"tcpdump", "tcpdump"},
	"tc":             {"iproute2", "iproute"},
	"kvm_stat":       {"linux-tools-common", "kernel-tools"},
}

// pkgInstallHint 按当前包管理器生成安装/升级命令提示（§5.11.2 包名映射，与 install.sh RPM_PKG_MAP 对齐）
func pkgInstallHint(comp string) string {
	names, ok := componentPkgMap[comp]
	if !ok {
		names = [2]string{comp, comp}
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return "sudo dnf install -y " + names[1]
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "sudo yum install -y " + names[1]
	}
	return "sudo apt install -y " + names[0]
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}
