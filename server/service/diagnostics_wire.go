package service

import "kvm_console/service/diagnostics"

// ── 组件版本健康度委托（M7.2 / §5.11.5） ──

// ComponentHealth 组件版本健康度聚合结果（诊断页展示与 /system-info 挂载点共用）。
type ComponentHealth = diagnostics.ComponentHealth

// GetComponentHealth 返回组件版本健康度（缓存探测结果）。
func GetComponentHealth() diagnostics.ComponentHealth {
	return diagnostics.GetComponentHealth()
}

// ResetComponentHealthCache 清除组件健康度缓存（前端「刷新」/ POST /settings/diagnostics/refresh 触发重新探测）。
func ResetComponentHealthCache() {
	diagnostics.ResetComponentHealthCache()
}

// RefreshComponentHealth 强制重新探测组件健康度（H3：带冷却，防止前端高频刷新触发多次全量探测）。
func RefreshComponentHealth() diagnostics.ComponentHealth {
	return diagnostics.RefreshComponentHealth()
}

// StartHealthProbe 启动周期健康探针（M8.10 / §14 P2-10）。
func StartHealthProbe() {
	diagnostics.StartHealthProbe()
}

// GetHealthProbe 返回最近一次健康探针快照（供 /api/system/health/latest 读取）。
func GetHealthProbe() diagnostics.HealthProbe {
	return diagnostics.GetHealthProbe()
}
