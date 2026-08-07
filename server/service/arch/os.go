package arch

import (
	"os"
	"strconv"
	"strings"
)

// ── 发行版归一化 ──
//
// 统一将 /etc/os-release 的 ID / ID_LIKE / PRETTY_NAME / VERSION_ID 归一到
// 模块规范常量（Debian / Ubuntu / RHEL 系 / openEuler / Kylin…），供
// component_health、/system-info、安装期 dpkg/dnf 分档等统一口径，避免散落的裸
// ID 比较。与「架构归一化」（normalize.go）对称成体系。
//
// 归一化不替代发行版原始信息：原始 ID/ID_LIKE/版本仍保留在 OSRelease 中，
// 调用方按需取用；本模块只提供「成人可读的收纳」与若干谓词。

// DistroFamily 归一化后的发行版家族标识（对应安装/升级/健康度的分档口径）。
type DistroFamily string

const (
	// ── Debian 系 ──
	DistroDebian DistroFamily = "debian"
	DistroUbuntu DistroFamily = "ubuntu"
	DistroDeepin DistroFamily = "deepin"
	DistroUOS    DistroFamily = "uos" // 统信 UOS（ID_LIKE=debian）
	// ── RPM 系 ──
	DistroRHEL      DistroFamily = "rhel" // Red Hat / Rocky / Alma / Anolis / OpenCloudOS / Amazon
	DistroCentOS    DistroFamily = "centos"
	DistroFedora    DistroFamily = "fedora"
	DistroOpenEuler DistroFamily = "openeuler"
	DistroKylin     DistroFamily = "kylin"
	DistroNeoKylin  DistroFamily = "neokylin"
	DistroNFOS      DistroFamily = "nfos" // 中科方德（ID_LIKE=debian）
	DistroOpenKylin DistroFamily = "openkylin"
	DistroAmazon    DistroFamily = "amazon" // Amazon Linux（RPM 系但 ID_LIKE 常空）
	// ── 未知 ──
	DistroUnknown DistroFamily = "unknown"
)

// PackageFamily 归一化后的包管理家族（apt / dnf|yum 双档判断专用）。
type PackageFamily string

const (
	PkgDeb PackageFamily = "deb" // apt（Debian / Ubuntu）
	PkgRpm PackageFamily = "rpm" // dnf / yum（RHEL 系 / openEuler / Kylin …）
	PkgAny PackageFamily = "any" // 无法判定
)

// OSRelease 解析 /etc/os-release 后的字段（与安装脚本 `. /etc/os-release` 对齐）。
type OSRelease struct {
	ID      string // ID=（如 "debian"、"openeuler"、"kylin"）
	IDLike  string // ID_LIKE=（空格分隔继承链）
	Name    string // NAME=
	Pretty  string // PRETTY_NAME=
	Version string // VERSION_ID=（主版本号，如 24.03）
	// VersionIDFloat 见 VersionID()
}

// VersionID 将 VERSION_ID 数值化（"24.03"→24.03；缺失/非法返回 0）。
func (r OSRelease) VersionID() float64 {
	if r.Version == "" {
		return 0
	}
	v, err := strconv.ParseFloat(r.Version, 64)
	if err != nil {
		return 0
	}
	return v
}

// Family 归一到 DistroFamily 常量（见 DistroFamilyOf）。
func (r OSRelease) Family() DistroFamily {
	return DistroFamilyOf(r.ID, r.IDLike, r.Pretty)
}

// PackFamily 归一到 PackageFamily（deb/rpm/any）。
func (r OSRelease) PackFamily() PackageFamily {
	return PackageFamilyOf(r)
}

// IsDeb 判断是否为 Debian/Ubuntu 系（apt）。
func (r OSRelease) IsDeb() bool { return r.PackFamily() == PkgDeb }

// IsRpm 判断是否为 RPM 系（dnf/yum）。
func (r OSRelease) IsRpm() bool { return r.PackFamily() == PkgRpm }

// ReadOSRelease 读取并解析 /etc/os-release；失败返回零值 OSRelease。
func ReadOSRelease() OSRelease {
	var r OSRelease
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return r
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ID=") && r.ID == "":
			r.ID = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		case strings.HasPrefix(line, "ID_LIKE=") && r.IDLike == "":
			r.IDLike = strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), "\"")
		case strings.HasPrefix(line, "NAME=") && r.Name == "":
			r.Name = strings.Trim(strings.TrimPrefix(line, "NAME="), "\"")
		case strings.HasPrefix(line, "PRETTY_NAME=") && r.Pretty == "":
			r.Pretty = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		case strings.HasPrefix(line, "VERSION_ID=") && r.Version == "":
			r.Version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
	}
	return r
}

// DistroFamilyOf 将发行版 ID/ID_LIKE/PRETTY_NAME 归一到 DistroFamily 常量。
// 判定顺序：ID 精确归组 → ID_LIKE 继承链 → PRETTY_NAME 子串兜底。
// 未命中返回 DistroUnknown（不臆改为任一家族，避免掩盖真实发行版）。
func DistroFamilyOf(id, idLike, pretty string) DistroFamily {
	id = strings.ToLower(strings.TrimSpace(id))
	idl := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(idLike), ",", " "))
	prs := strings.ToLower(strings.TrimSpace(pretty))

	// ① ID 精确归组（含常见别名；Kylin 桌面版 ID=kylin、服务器版 ID=kylin 同源）
	switch id {
	case "debian":
		return DistroDebian
	case "ubuntu":
		return DistroUbuntu
	case "rhel", "redhat", "rocky", "alma", "almalinux", "anolis", "opencloudos", "tencentos", "alinux", "alibabacloud":
		return DistroRHEL
	case "centos":
		return DistroCentOS
	case "fedora":
		return DistroFedora
	case "openeuler", "open_euler":
		return DistroOpenEuler
	case "kylin":
		return DistroKylin
	case "neokylin":
		return DistroNeoKylin
	case "amzn", "amazon", "amazonlinux":
		return DistroAmazon
	}

	// ② ID_LIKE 继承链（逐个匹配；Ubuntu Debian 派生直接归 debian 系）
	debianSuffix := map[string]DistroFamily{
		"debian": DistroDebian,
		"ubuntu": DistroUbuntu,
	}
	for _, like := range strings.Fields(idl) {
		like = strings.ToLower(like)
		if f, ok := debianSuffix[like]; ok {
			return f
		}
		switch like {
		case "rhel", "redhat":
			return DistroRHEL
		case "centos":
			return DistroCentOS
		case "fedora":
			return DistroFedora
		case "openeuler":
			return DistroOpenEuler
		case "kylin", "neokylin":
			return DistroKylin
		}
	}

	// ③ PRETTY_NAME 子串兜底（中文名如 "银河麒麟 V10 SP3"、openCloudOS 等）
	for _, probe := range []struct {
		kw string
		f  DistroFamily
	}{
		{"debian", DistroDebian},
		{"ubuntu", DistroUbuntu},
		{"deepin", DistroDeepin}, {"深度", DistroDeepin},
		{"统信", DistroUOS}, {"uos", DistroUOS},
		{"openeuler", DistroOpenEuler},
		{"openkylin", DistroOpenKylin},
		{"方德", DistroNFOS}, {"nfos", DistroNFOS},
		{"银河麒麟", DistroKylin}, {"kylin", DistroKylin}, {"麒麟", DistroKylin},
		{"opencloudos", DistroRHEL},
		{"rocky", DistroRHEL}, {"almalinux", DistroRHEL}, {"anolis", DistroRHEL},
		{"red hat", DistroRHEL}, {"rhel", DistroRHEL}, {"红帽", DistroRHEL},
		{"centos", DistroCentOS},
	} {
		if strings.Contains(prs, probe.kw) {
			return probe.f
		}
	}
	return DistroUnknown
}

// PackageFamilyOf 由归一族派生包家族：
// deb/rpm/any（RHEL/CentOS/Fedora/openEuler/Kylin/NeoKylin/Amazon → rpm）。
func PackageFamilyOf(r OSRelease) PackageFamily {
	switch DistroFamilyOf(r.ID, r.IDLike, r.Pretty) {
	case DistroDebian, DistroUbuntu:
		return PkgDeb
	case DistroRHEL, DistroCentOS, DistroFedora, DistroOpenEuler, DistroKylin, DistroNeoKylin, DistroAmazon:
		return PkgRpm
	default:
		return PkgAny
	}
}

// IsRpm 便捷谓词：判断给定 os-release 是否 RPM 系。
// 保留此函数供不持有 OSRelease 值的调用方使用（如包级别辅助）。
func IsRpm(r OSRelease) bool { return r.PackFamily() == PkgRpm }
