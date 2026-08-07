package arch

import (
	"strings"
)

// ── 架构字符串归一化 ──
//
// 统一将 uname -m、GOARCH、XML <type arch>、前端入参等来源的架构别名
// 归一化到模块规范常量（x86_64 / aarch64 / riscv64）。调用方应统一经由
// NormalizeArch / IsX86Arch 判断，避免散落的裸字符串比较（如 "arm64"、
// "amd64"、"i686" 直接出现）。

// normalizeArchOnce 存放已验证的归一化表；归一化失败返回原始 clean 值。
// 注意：NormalizeArch 不做"未知架构回退到 x86_64"——未知架构原样返回，
// 由调用方自行决定是否回退（避免掩盖真实架构）。
func NormalizeArch(arch string) string {
	c := strings.ToLower(strings.TrimSpace(arch))
	switch c {
	case "x86_64", "amd64", "x64", "i386", "i486", "i586", "i686", "x86":
		return ArchX8664
	case "aarch64", "arm64", "armv8", "armv8-a":
		return ArchAarch64
	case "riscv64", "riscv":
		return ArchRiscv64
	default:
		return c
	}
}

// IsX86Arch 判断架构是否为 x86 系列（含 32/64 位变体）。
func IsX86Arch(arch string) bool {
	switch NormalizeArch(arch) {
	case ArchX8664:
		return true
	default:
		return false
	}
}

// IsAarch64Arch 判断架构是否为 ARM64（aarch64）系列。
func IsAarch64Arch(arch string) bool {
	return NormalizeArch(arch) == ArchAarch64
}

// IsRiscv64Arch 判断架构是否为 RISC-V 64 位。
func IsRiscv64Arch(arch string) bool {
	return NormalizeArch(arch) == ArchRiscv64
}
