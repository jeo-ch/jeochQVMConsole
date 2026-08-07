package handler

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"kvm_console/config"
	"kvm_console/service"
	"kvm_console/service/arch"
	"kvm_console/service/ovs"
	"kvm_console/utils"
)

// GetVersion 返回系统版本信息
func GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"version":    Version,
			"build_time": BuildTime,
			"site_title": config.GlobalConfig.SiteTitle,
		},
	})
}

// systemInfoCacheTTL /system-info 聚合结果缓存时长：整包探测含组件健康度（冷启动 19 项）、
// 版本/防火墙/glibc/OVS 等，缓存全冷时单次可派生约 30 个子进程；TTL 内直接返回快照，
// 页面切换（关于/新建 VM/防火墙/诊断卡）不重复触发探测。
const systemInfoCacheTTL = 30 * time.Second

// systemInfoSnapshot /system-info 一次性探测结果快照（缓存粒度，按角色过滤后再组装响应）。
type systemInfoSnapshot struct {
	goVersion    string
	os           string
	distro       string
	osInfo       osReleaseInfo
	pkgMgr       string
	arch         string
	numCPU       int
	hostname     string
	numGoroutine int
	kernel       string
	uptime       string
	libvirt      string
	qemu         string
	qemuSpice    bool
	ovs          ovsDependencyInfo
	glibc        string
	cpuAvx2      bool
	cpuFma       bool
	cpuVendor    string
	kysec        string
	selinuxAvail bool
	selinuxMode  string
	firewall     service.BackendStatus
	advice       service.UpgradeAdvice
	health       service.ComponentHealth
}

var (
	systemInfoMu     sync.Mutex
	systemInfoCache  *systemInfoSnapshot
	systemInfoCached time.Time
)

// getSystemInfoSnapshot 返回聚合快照（TTL 缓存；探测在锁内串行，避免并发请求重复派生子进程）。
func getSystemInfoSnapshot() *systemInfoSnapshot {
	systemInfoMu.Lock()
	defer systemInfoMu.Unlock()
	if systemInfoCache != nil && time.Since(systemInfoCached) < systemInfoCacheTTL {
		return systemInfoCache
	}
	snap := probeSystemInfo()
	systemInfoCache = snap
	systemInfoCached = time.Now()
	return snap
}

// probeSystemInfo 一次性执行全部系统信息探测（各叶子探测自身已有缓存，此处为聚合层）。
func probeSystemInfo() *systemInfoSnapshot {
	hostname, _ := os.Hostname()
	osInfo := getOSReleaseInfo()
	fwStatus := getFirewallBackendStatus()
	cpuAvx2, cpuFma := detectCPUFlags()
	selinuxMode := service.DetectSELinuxMode()
	return &systemInfoSnapshot{
		goVersion:    runtime.Version(),
		os:           runtime.GOOS,
		distro:       getDistroName(),
		osInfo:       osInfo,
		pkgMgr:       detectPackageManager(),
		arch:         arch.GetHostArchDisplayName(),
		numCPU:       runtime.NumCPU(),
		hostname:     hostname,
		numGoroutine: runtime.NumGoroutine(),
		kernel:       getKernelVersion(),
		uptime:       getSystemUptime(),
		libvirt:      getLibvirtVersion(),
		qemu:         getQEMUVersion(),
		qemuSpice:    CheckQEMUSPICESupport(),
		ovs:          getOVSDependencyInfo(),
		glibc:        getGlibcVersion(),
		cpuAvx2:      cpuAvx2,
		cpuFma:       cpuFma,
		cpuVendor:    arch.DetectCPUVendor(),
		kysec:        arch.KysecStatus(),
		selinuxAvail: selinuxAvailable(),
		selinuxMode:  selinuxMode,
		firewall:     fwStatus,
		advice:       getUpgradeAdvice(),
		health:       getComponentHealth(),
	}
}

// GetPublicSystemInfo 返回系统运行环境信息（需登录认证）。
// 安全收敛（§3.3 评审）：glibc/cpu/selinux/firewall/component_health 为管理员专属字段，
// 普通用户仅返回基础信息（kernel/qemu/libvirt/arch/qemu_spice 保留——关于页与新建 VM 表单依赖，
// 属既有产品决策）。
func GetPublicSystemInfo(c *gin.Context) {
	role, _ := c.Get("role")
	isAdmin := role == "admin"
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": buildSystemInfoResponse(getSystemInfoSnapshot(), isAdmin),
	})
}

// buildSystemInfoResponse 按角色过滤组装响应：非管理员隐藏侦察面较大的详细字段。
func buildSystemInfoResponse(snap *systemInfoSnapshot, isAdmin bool) gin.H {
	base := gin.H{
		"go_version":          snap.goVersion,
		"os":                  snap.os,
		"distro":              snap.distro,
		"os_id":               snap.osInfo.ID,
		"os_id_like":          snap.osInfo.IDLike,
		"pkg_manager":         snap.pkgMgr,
		"arch":                snap.arch,
		"num_cpu":             snap.numCPU,
		"hostname":            snap.hostname,
		"num_goroutine":       snap.numGoroutine,
		"uptime":              snap.uptime,
		"kernel":              snap.kernel,
		"libvirt":             snap.libvirt,
		"qemu":                snap.qemu,
		"qemu_spice":          snap.qemuSpice,
		"ovs_package":         snap.ovs.PackageName,
		"ovs_service":         snap.ovs.ServiceName,
		"ovs_installed":       snap.ovs.Installed,
		"ovs_install_command": snap.ovs.InstallCommand,
	}
	if !isAdmin {
		return base
	}
	base["glibc"] = snap.glibc
	base["cpu"] = gin.H{
		"avx2":       snap.cpuAvx2,
		"fma":        snap.cpuFma,
		"vendor":     snap.cpuVendor,
		"cpu_vendor": snap.cpuVendor,
		"kysec":      snap.kysec,
	}
	base["selinux"] = gin.H{
		"available": snap.selinuxAvail,
		"enforcing": snap.selinuxMode == "enforcing",
		"mode":      snap.selinuxMode,
	}
	base["firewall"] = gin.H{
		"backend":           snap.firewall.Backend,
		"available":         snap.firewall.Available,
		"active":            snap.firewall.Active,
		"version":           snap.firewall.Version,
		"ip_backend":        snap.firewall.IPBackend,
		"nm_managed":        snap.firewall.NM,
		"docker_compatible": snap.firewall.DockerCompatible,
		"error_code":        snap.firewall.ErrorCode,
		"upgrade_advice":    snap.advice,
	}
	base["component_health"] = snap.health
	return base
}

// getComponentHealth 返回组件版本健康度（M7.2 / §5.11.5，缓存探测结果）
func getComponentHealth() service.ComponentHealth {
	return service.GetComponentHealth()
}

// ── 国产化组件诊断辅助（§4.1） ──

func getFirewallBackendStatus() service.BackendStatus {
	return service.GetFirewallBackendStatus()
}

func getGlibcVersion() string {
	return service.DetectGlibcVersion()
}

func getUpgradeAdvice() service.UpgradeAdvice {
	return service.DetectUpgradeAdvice()
}

func detectCPUFlags() (avx2, fma bool) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "flags") {
			// 用 Fields 分词而非 " avx2 " 子串，避免末尾旗标漏判（#A9）
			fields := strings.Fields(lower)
			for _, f := range fields {
				switch f {
				case "avx2":
					avx2 = true
				case "fma":
					fma = true
				}
			}
			return avx2, fma
		}
	}
	return false, false
}

func selinuxAvailable() bool {
	_, err := exec.LookPath("getenforce")
	return err == nil
}

// ── OS / 包管理 / OVS 依赖检测 ──

type osReleaseInfo struct {
	ID      string
	IDLike  string
	Name    string
	Version string
}

func getOSReleaseInfo() osReleaseInfo {
	rel := arch.ReadOSRelease()
	return osReleaseInfo{
		ID:      rel.ID,
		IDLike:  rel.IDLike,
		Name:    rel.Pretty,
		Version: rel.Version,
	}
}

// detectPackageManager 检测系统包管理器，返回 "apt" / "dnf" / "yum" / "unknown"
func detectPackageManager() string {
	rel := arch.ReadOSRelease()

	// Debian/Ubuntu 系列（使用 arch 包归一化判定，消除硬编码发行版列表）
	if rel.IsDeb() {
		if _, err := exec.LookPath("apt-get"); err == nil {
			return "apt"
		}
	}

	// RPM 系发行版（使用 arch 包归一化判定）
	if rel.IsRpm() {
		if _, err := exec.LookPath("dnf"); err == nil {
			return "dnf"
		}
		if _, err := exec.LookPath("yum"); err == nil {
			return "yum"
		}
	}

	// 通用回退：按命令可用性检测
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf"
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "yum"
	}
	if _, err := exec.LookPath("apt-get"); err == nil {
		return "apt"
	}
	return "unknown"
}

type ovsDependencyInfo struct {
	PackageName    string
	ServiceName    string
	Installed      bool
	InstallCommand string
}

func getOVSDependencyInfo() ovsDependencyInfo {
	info := ovsDependencyInfo{
		PackageName: "openvswitch-switch",
		ServiceName: ovs.DetectOpenvswitchServiceName(),
	}
	// 检查 ovs-vsctl 是否已安装
	if _, err := exec.LookPath("ovs-vsctl"); err == nil {
		info.Installed = true
	}
	// 根据包管理器生成安装命令
	pkgMgr := detectPackageManager()
	switch pkgMgr {
	case "apt":
		info.PackageName = "openvswitch-switch"
		info.InstallCommand = "sudo apt install -y openvswitch-switch"
	case "dnf":
		info.PackageName = "openvswitch"
		info.InstallCommand = "sudo dnf install -y openvswitch"
	case "yum":
		info.PackageName = "openvswitch"
		info.InstallCommand = "sudo yum install -y openvswitch"
	default:
		info.InstallCommand = "# 请根据系统包管理器手动安装 OVS"
	}
	return info
}

// ── 系统信息辅助函数 ──

// getKernelVersion 返回内核版本（统一走 utils 超时封装，避免子进程挂起请求）
func getKernelVersion() string {
	res := utils.ExecCommandWithTimeout("uname", 5*time.Second, "-r")
	if res.Error != nil {
		return "-"
	}
	return res.Stdout
}

func getSystemUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "-"
	}
	var uptimeSeconds float64
	fmt.Sscanf(string(data), "%f", &uptimeSeconds)
	d := time.Duration(uptimeSeconds * float64(time.Second))
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%d 天 %d 小时", days, hours)
	}
	return fmt.Sprintf("%d 小时", hours)
}

func getLibvirtVersion() string {
	res := utils.ExecCommandWithTimeout("libvirtd", 5*time.Second, "--version")
	if res.Error != nil {
		return "-"
	}
	return res.Stdout
}

// resolveQEMUCmd 按宿主机架构解析 QEMU 二进制名（与 component_health 同口径：
// aarch64 用 qemu-system-aarch64，避免 ARM 主机上版本探测恒为 "-"；缺失时回退 qemu-kvm）。
func resolveQEMUCmd() string {
	if arch.IsAarch64Arch(runtime.GOARCH) {
		if _, err := exec.LookPath("qemu-system-aarch64"); err == nil {
			return "qemu-system-aarch64"
		}
		return "qemu-kvm"
	}
	if _, err := exec.LookPath("qemu-system-x86_64"); err == nil {
		return "qemu-system-x86_64"
	}
	return "qemu-kvm"
}

func getQEMUVersion() string {
	res := utils.ExecCommandWithTimeout(resolveQEMUCmd(), 5*time.Second, "--version")
	if res.Error != nil {
		return "-"
	}
	lines := strings.SplitN(res.Stdout, "\n", 2)
	if len(lines) > 0 {
		return lines[0]
	}
	return "-"
}

func getDistroName() string {
	rel := arch.ReadOSRelease()
	if rel.Pretty != "" {
		return rel.Pretty
	}
	if rel.Name != "" {
		return rel.Name
	}
	if rel.ID != "" {
		return rel.ID
	}
	return "-"
}

// CheckQEMUSPICESupport 检测 QEMU 是否编译了 SPICE 支持（按架构解析二进制，统一超时）。
func CheckQEMUSPICESupport() bool {
	cmds := []string{resolveQEMUCmd()}
	if cmds[0] != "qemu-kvm" {
		cmds = append(cmds, "qemu-kvm")
	}
	for _, cmd := range cmds {
		// 方法1: 检查 -spice help
		if res := utils.ExecCommandWithTimeout(cmd, 5*time.Second, "-spice", "help"); res.Error == nil && strings.Contains(strings.ToLower(res.Stdout), "spice") {
			return true
		}
		// 方法2: 检查帮助信息中是否包含 spice 选项
		if res := utils.ExecCommandWithTimeout(cmd, 5*time.Second, "--help"); res.Error == nil && strings.Contains(strings.ToLower(res.Stdout), "-spice") {
			return true
		}
	}
	return false
}

// Version 通过 ldflags 在构建时注入，格式: -X kvm_console/handler.Version=v1.0.0
var Version = "dev"

// BuildTime 通过 ldflags 在构建时注入，格式: -X kvm_console/handler.BuildTime=2025-01-01T00:00:00Z
var BuildTime = ""
