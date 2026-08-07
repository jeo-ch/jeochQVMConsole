/**
 * VM 看门狗事件 API
 * 对应后端 /api/vm-watchdog/events 路由组（管理员专属，M8.9/§14 P2-9）
 */
import service from './client'
import type { ApiResponse } from '@/types/api'

/** 看门狗事件记录 */
export interface VMWatchdogEventItem {
  id: number
  vm_name: string
  /** reset（自动硬重置）/ warning（预警）/ recovered（恢复） */
  status: string
  reason: string
  result_message: string
  created_at: string
}

/** 看门狗事件列表响应 */
export interface VMWatchdogEventListResponse {
  list: VMWatchdogEventItem[]
  total: number
  page: number
  page_size: number
}

/** 获取看门狗事件列表（支持状态/虚拟机/时间范围筛选与分页） */
export function getVMWatchdogEventList(params: {
  page?: number
  page_size?: number
  status?: string
  vm_name?: string
  start?: string
  end?: string
}) {
  return service.get<unknown, ApiResponse<VMWatchdogEventListResponse>>(
    '/vm-watchdog/events',
    { params, silent: true },
  )
}