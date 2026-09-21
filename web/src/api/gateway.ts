import service from './client'

export interface GatewayMigrationStatus {
  id: number
  type: string
  status: string
  progress: number
  message: string
  detail?: {
    bytes_transferred: number
    total_bytes: number
    percent: number
    speed_bytes: number
    speed_human: string
    elapsed: string
    eta: string
  }
  result?: string
}

export interface GatewayMigrationRequest {
  source_host_id: number
  target_host_id: number
  vm_name: string
  disk_target?: string
  snapshot_name?: string
  target_disk_path?: string
  format?: string
  shutdown?: boolean
  target_ssh: {
    host: string
    port?: string
    user?: string
    auth_method?: string
    key_content?: string
    password?: string
  }
}

/** 发起网关迁移任务 */
export function startGatewayMigration(data: GatewayMigrationRequest) {
  return service.post<{ id: number; status: string }>('/gateway/migrations', data)
}

/** 查询网关迁移任务状态 */
export function getGatewayMigrationStatus(id: number) {
  return service.get<GatewayMigrationStatus>(`/gateway/migrations/${id}`)
}

/** 列出网关迁移任务（按类型筛选） */
export function listGatewayMigrationTasks(type = 'gateway_migration') {
  return service.get<GatewayMigrationStatus[]>('/gateway/migrations', { params: { type } })
}
