/**
 * 周期健康探针 API（M8.10 / §14 P2-10）
 * 供 Dashboard 状态灯轮询：面板离线（接口不可达/超时）→ 红灯；
 * libvirt 不可用 → 黄灯；否则绿灯。
 */
import service from './client'
import type { ApiResponse } from '@/types/api'

export interface HealthProbe {
  timestamp: string
  service_uptime_s: number
  panel_online: boolean
  libvirt_ready: boolean
  libvirt_daemon: boolean
  maintenance_mode: boolean
  version: string
}

export type HealthLightStatus = 'green' | 'yellow' | 'red' | 'unknown'

/** 面板离线判定：轮询失败/超时视为面板离线（红灯） */
export function getHealthProbeLatest() {
  return service.get<unknown, ApiResponse<HealthProbe>>('/system/health/latest', {
    silent: true,
    timeout: 8000,
  })
}
