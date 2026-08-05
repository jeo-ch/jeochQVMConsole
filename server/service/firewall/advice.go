package firewall

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kvm_console/utils"
)

// ── 组件升级提示 advice（v0.9.3，#Q，§4.1） ──
//
// 基于探测结果对比「现状 vs 期望」，只告知不改变后端自动返回行为。
// 供 /system-info 运行期 Banner 与 install.sh print_install_report 复用。

// UpgradeAdvice 结构化提示对象，多命中时前端按优先级取最高一条展示。
type UpgradeAdvice struct {
	FirewalldUnsupported bool `json:"firewalld_unsupported"` // firewall.version < 0.6（面板不启用统一管理，#V 三档分级 <0.6）
	FirewalldOld         bool `json:"firewalld_old"`         // 0.6 ≤ firewall.version < 0.9（无 policy）
	GlibcLowForNative    bool `json:"glibc_low_for_native"`  // glibc < native-glibc.txt 需求版
	SelinuxEnforcing     bool `json:"selinux_enforcing"`     // selinux.mode == enforcing
}

// DetectUpgradeAdvice 组装 upgrade_advice（§4.1）。
// 优先级：firewalld_unsupported（<0.6，不完整支持）> firewalld_old（0.6~0.9 缺 policy）> glibc_low_for_native（优化类）> selinux_enforcing（配置类）。
// 探测涉及多次子进程（版本/glibc/selinux），用带 TTL 的缓存避免每次 /system-info 重复探测。
func DetectUpgradeAdvice() UpgradeAdvice {
	adviceMu.RLock()
	if adviceCached && time.Since(adviceTime) < adviceCacheTTL {
		advice := adviceCache
		adviceMu.RUnlock()
		return advice
	}
	adviceMu.RUnlock()

	adviceMu.Lock()
	defer adviceMu.Unlock()
	if adviceCached && time.Since(adviceTime) < adviceCacheTTL {
		return adviceCache
	}
	adviceCache = detectUpgradeAdvice()
	adviceCached = true
	adviceTime = time.Now()
	return adviceCache
}

var (
	adviceMu     sync.RWMutex
	adviceCache  UpgradeAdvice
	adviceCached bool
	adviceTime   time.Time
)

// adviceCacheTTL 升级提示缓存有效期；组件升级后超时自动重新探测。
const adviceCacheTTL = 10 * time.Minute

func detectUpgradeAdvice() UpgradeAdvice {
	advice := UpgradeAdvice{}
	backend := resolveBackend()

	// firewalld 三档判定（§5.1 决策 3，P0-2，#V）：
	//   < 0.6 → 不完整支持（firewalld_unsupported，面板不启用统一管理）
	//   0.6~0.9 → 缺 policy 能力（firewalld_old，与 #R 同口径）
	//   ≥ 0.9 → healthy，不置位
	if backend.Name() == "firewalld" {
		version := backend.Version()
		if version != "" {
			if compareFirewalldVersion(version, firewalldMinEnableVer) < 0 {
				advice.FirewalldUnsupported = true
			} else if compareFirewalldVersion(version, firewalldMinPolicyVer) < 0 {
				advice.FirewalldOld = true
			}
		}
	}

	// glibc_low_for_native：glibc < native-glibc.txt 需求版（#A，与 install.sh 同口径）
	glibc := DetectGlibcVersion()
	if glibc != "" {
		if nativeRequired, ok := readNativeGlibcRequired(); ok && compareVersion(glibc, nativeRequired) < 0 {
			advice.GlibcLowForNative = true
		}
	}

	// selinux_enforcing：#S1 已单文件 restorecon，仍提示核对
	if DetectSELinuxMode() == "enforcing" {
		advice.SelinuxEnforcing = true
	}
	return advice
}

// DetectGlibcVersion 探测 glibc 版本（#G，与 install.sh 同口径）：
//  1. ldd --version 首行最后一个 token
//  2. 回退 getconf GNU_LIBC_VERSION 第二个字段
//  3. 失败返回空串
//
// 带 TTL 缓存（与 advice 同周期），避免每次 /system-info 重复起子进程。
func DetectGlibcVersion() string {
	glibcMu.RLock()
	if glibcCached && time.Since(glibcTime) < adviceCacheTTL {
		v := glibcCache
		glibcMu.RUnlock()
		return v
	}
	glibcMu.RUnlock()

	glibcMu.Lock()
	defer glibcMu.Unlock()
	if glibcCached && time.Since(glibcTime) < adviceCacheTTL {
		return glibcCache
	}
	glibcCache = detectGlibcVersion()
	glibcCached = true
	glibcTime = time.Now()
	return glibcCache
}

var (
	glibcMu     sync.RWMutex
	glibcCache  string
	glibcCached bool
	glibcTime   time.Time
)

// detectGlibcVersion 探测 glibc 版本（§4.5 评审：统一走 utils.DetectGlibcVersion 单一来源，
// 与 diagnostics/component_health.go 共用，不再各自实现）。
func detectGlibcVersion() string {
	return utils.DetectGlibcVersion()
}

// readNativeGlibcRequired 读取发行包内 native-glibc.txt；文件缺失或非法返回 ok=false。
// 运行时文件位于安装目录（与 kvm-console 二进制同目录），据此定位。
func readNativeGlibcRequired() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(exe)
	data, err := os.ReadFile(filepath.Join(dir, "native-glibc.txt"))
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(data))
	if !utils.ValidGlibcToken(v) {
		return "", false
	}
	return v, true
}

// execDetectCommand 探测用命令执行（无锁裸执行，单条只读命令）。
func execDetectCommand(name string, args ...string) *utils.CmdResult {
	return utils.ExecCommand(firewallCommandPath(name), args...)
}

// DetectSELinuxMode 返回 enforcing / permissive / disabled（无 SELinux 返回 disabled）。
// 带 TTL 缓存（与 advice 同周期）。
func DetectSELinuxMode() string {
	selinuxMu.RLock()
	if selinuxCached && time.Since(selinuxTime) < adviceCacheTTL {
		mode := selinuxCache
		selinuxMu.RUnlock()
		return mode
	}
	selinuxMu.RUnlock()

	selinuxMu.Lock()
	defer selinuxMu.Unlock()
	if selinuxCached && time.Since(selinuxTime) < adviceCacheTTL {
		return selinuxCache
	}
	selinuxCache = detectSELinuxMode()
	selinuxCached = true
	selinuxTime = time.Now()
	return selinuxCache
}

var (
	selinuxMu     sync.RWMutex
	selinuxCache  string
	selinuxCached bool
	selinuxTime   time.Time
)

func detectSELinuxMode() string {
	result := execDetectCommand("getenforce")
	if result.Error != nil {
		return "disabled"
	}
	mode := strings.ToLower(strings.TrimSpace(result.Stdout))
	switch mode {
	case "enforcing", "permissive", "disabled":
		return mode
	default:
		return "disabled"
	}
}

// compareVersion 比较 "a.b[.c]" 版本；a<b 返回负、a>b 返回正、相等返回 0。
func compareVersion(a, b string) int {
	pa := versionParts(a)
	pb := versionParts(b)
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

func versionParts(s string) []int {
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
