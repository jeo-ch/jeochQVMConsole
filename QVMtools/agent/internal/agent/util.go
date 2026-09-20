package agent

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// HostMemory 收集本机物理内存并格式化为人类可读字符串。
func HostMemory() string {
	if out, err := runOutput("sysctl", "-n", "hw.memsize"); err == nil && strings.TrimSpace(out) != "" {
		if b, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64); err == nil {
			return formatBytes(b)
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if v, e := strconv.ParseInt(fields[1], 10, 64); e == nil {
						return formatBytes(v * 1024)
					}
				}
				break
			}
		}
	}
	return "0 B"
}

// formatBytes 将字节数格式化为 GiB 等人类可读单位。
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
