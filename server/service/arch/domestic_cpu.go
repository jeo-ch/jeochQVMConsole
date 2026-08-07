package arch

import (
	"os"
	"strings"
)

// ── 国产 CPU 厂商探测（M8.1 / P0-1，§5.8/§5.11.2） ──
//
// 读 /proc/cpuinfo 厂商字段，识别 Intel/AMD 与国产厂商（海光/飞腾/鲲鹏/兆芯），
// 供 install.sh precheck_domestic 写入 .env DOMESTIC_CPU_VENDOR、component_health
// 上报 cpu_vendor 白名单命中（§5.11.3 manifest cpu_vendor_whitelist）。

// CPUVendor 归一化后的 CPU 厂商标识（component_health 上报口径）。
const (
	CPUVendorIntel   = "Intel"
	CPUVendorAMD     = "AMD"
	CPUVendorHygon   = "Hygon"
	CPUVendorPhytium = "Phytium"
	CPUVendorZhaoxin = "Zhaoxin"
	CPUVendorKunpeng = "Kunpeng"
	CPUVendorUnknown = "Unknown"
)

// CPUVendorWhitelist CPU 厂商白名单（§5.11.3 manifest cpu_vendor_whitelist 同源）；
// 不在白名单 → component_health warning「未认证厂商」。
var CPUVendorWhitelist = []string{
	CPUVendorIntel,
	CPUVendorAMD,
	CPUVendorHygon,
	CPUVendorPhytium,
	CPUVendorZhaoxin,
	CPUVendorKunpeng,
}

// DetectCPUVendor 探测当前 CPU 厂商（归一化到 CPUVendor* 白名单值）。
// 探测口径与 detectCPUFlags 一致：读 /proc/cpuinfo 逐行匹配 vendor 字段，
// 缺失文件/解析失败返回 Unknown。
func DetectCPUVendor() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return CPUVendorUnknown
	}
	vendor := cpuVendorFromCpuinfo(string(data))
	if vendor == "" {
		return CPUVendorUnknown
	}
	return normalizeCPUVendor(vendor)
}

// cpuVendorFromCpuinfo 从 cpuinfo 内容提取原始 vendor 字段值（首个非空）。
func cpuVendorFromCpuinfo(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "vendor_id") ||
			strings.HasPrefix(trimmed, "CPU implementer") {
			val := strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1])
			if val != "" {
				return val
			}
		}
	}
	return ""
}

// normalizeCPUVendor 将原始厂商串归一化到白名单标识。
// 海光 vendor_id = "HygonGenuine"（对齐 SMTXOS 显式分支）；飞腾/鲲鹏 ARM64
// 无 vendor_id，依 "CPU implementer" 的 hex code（飞腾 0x70 / 鲲鹏 0x41=ARM）。
func normalizeCPUVendor(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "genuineintel"), strings.Contains(lower, "intel"):
		return CPUVendorIntel
	case strings.Contains(lower, "authenticamd"), strings.Contains(lower, "amd"):
		return CPUVendorAMD
	case strings.Contains(lower, "hygon"):
		return CPUVendorHygon
	case strings.Contains(lower, "0x70"), strings.Contains(lower, "phytium"), strings.Contains(lower, "飞腾"):
		return CPUVendorPhytium
	case strings.Contains(lower, "0x41"), strings.Contains(lower, "arm"), strings.Contains(lower, "kunpeng"), strings.Contains(lower, "鲲鹏"):
		return CPUVendorKunpeng
	case strings.Contains(lower, "zhaoxin"), strings.Contains(lower, "兆芯"), strings.Contains(lower, "centaurhauls"):
		return CPUVendorZhaoxin
	}
	return CPUVendorUnknown
}
