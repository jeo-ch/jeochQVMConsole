// Package-level periodic health probe (M8.10 / §14 P2-10).
//
// 周期健康探针：后台协程每分钟将面板与虚拟化栈健康快照写入
// ${INSTALL_DIR}/.health/latest.json（默认 /opt/QVMConsole/.health/latest.json）。
// 前端 Dashboard 轮询该文件（经 /api/system/health/latest 暴露）：
//   - 面板在线 + libvirtd 正常 → 绿灯
//   - 面板在线 + libvirtd 不可用 → 黄灯
//   - 面板离线（轮询超时/连接失败）→ 红灯
//
// 文件写盘采用「临时文件 + rename」保证原子性，避免前端读到半截 JSON。
package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kvm_console/config"
	"kvm_console/logger"
	"kvm_console/service/libvirt_rpc"
	"kvm_console/utils"
)

// HealthProbe 健康探针快照内容。
type HealthProbe struct {
	Timestamp       string `json:"timestamp"`
	ServiceUptimeS  int64  `json:"service_uptime_s"`
	PanelOnline     bool   `json:"panel_online"`     // 恒为 true（文件由面板自身写出）
	LibvirtReady    bool   `json:"libvirt_ready"`    // libvirt 连接可用
	LibvirtDaemon   bool   `json:"libvirt_daemon"`   // libvirtd 服务 active
	MaintenanceMode bool   `json:"maintenance_mode"` // 维护模式
	Version         string `json:"version"`
}

const defaultHealthDirRel = "/.health"

var (
	healthFileMu    sync.Mutex
	healthLastProbe HealthProbe
)

// healthDir 返回健康探针目录（可用 KVM_HEALTH_DIR 覆盖，默认 ${INSTALL_DIR}/.health）。
func healthDir() string {
	if config.GlobalConfig != nil && config.GlobalConfig.HealthDir != "" {
		return config.GlobalConfig.HealthDir
	}
	return config.InstallDir() + defaultHealthDirRel
}

// libvirtdActive 探测 libvirtd 服务是否 active（systemd 优先，回退 pgrep）。
func libvirtdActive() bool {
	res := utils.ExecCommandQuiet("systemctl", "is-active", "libvirtd.service")
	if res.Error == nil {
		out := strings.TrimSpace(res.Stdout)
		return out == "active" || out == "activating"
	}
	res = utils.ExecCommandQuiet("pgrep", "-x", "libvirtd")
	return res.Error == nil
}

// StartHealthProbe 启动周期健康探针（main.go 启动阶段调用，每分钟写盘一次）。
func StartHealthProbe() {
	probeVersion := ""
	if config.GlobalConfig != nil {
		probeVersion = config.GlobalConfig.AppVersion
	}
	startedAt := time.Now()

	go func() {
		defer utils.RecoverAndLog("health-probe")
		// 启动后立即写一版，随后每分钟刷新
		writeProbeOnce(startedAt, probeVersion)
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			writeProbeOnce(startedAt, probeVersion)
		}
	}()
}

func writeProbeOnce(startedAt time.Time, version string) {
	probe := HealthProbe{
		Timestamp:       time.Now().Format(time.RFC3339),
		ServiceUptimeS:  int64(time.Since(startedAt).Seconds()),
		PanelOnline:     true,
		LibvirtReady:    libvirt_rpc.IsLibvirtRPCAvailable(),
		LibvirtDaemon:   libvirtdActive(),
		MaintenanceMode: false,
		Version:         version,
	}
	if config.GlobalConfig != nil {
		probe.MaintenanceMode = config.GlobalConfig.MaintenanceMode
	}

	healthFileMu.Lock()
	defer healthFileMu.Unlock()
	healthLastProbe = probe

	if err := writeHealthFile(probe); err != nil {
		logger.App.Warn("健康探针写盘失败", "error", err)
	}
}

// writeHealthFile 原子写盘：临时文件 + rename。
func writeHealthFile(probe HealthProbe) error {
	data, err := json.Marshal(probe)
	if err != nil {
		return err
	}
	dir := healthDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".latest.json.tmp")
	final := filepath.Join(dir, "latest.json")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// GetHealthProbe 返回最近一次健康探针快照（供 /api/system/health/latest 接口读取）。
func GetHealthProbe() HealthProbe {
	healthFileMu.Lock()
	defer healthFileMu.Unlock()
	return healthLastProbe
}
