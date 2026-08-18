/**
 * 系统设置相关 API
 * 对应后端 /settings 路由组与 /host 宿主机级配置接口
 */
import service from './client'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from '@/types/api'

/** 公开设置（无需登录即可获取） */
export interface PublicSettings {
  site_title?: string
  password_breach_check_enabled?: boolean
  spice_enabled_by_default?: boolean
  [key: string]: unknown
}

/** 获取公开设置 */
export function getPublicSettings() {
  return service.get<unknown, ApiResponse<PublicSettings>>('/public/settings', { silent: true })
}

/** 面板版本信息（无需登录） */
export interface PublicVersion {
  version?: string
  build_time?: string
  site_title?: string
}

/** 获取面板版本信息 */
export function getPublicVersion() {
  return service.get<unknown, ApiResponse<PublicVersion>>('/public/version', { silent: true })
}

/** 系统设置（完整字段见设置页 types.ts，此处保持开放结构供各业务按需取用） */
export interface SystemSettings {
  iso_dir?: string
  [key: string]: unknown
}

/** 构造阶段令牌请求头（安全初始化 bootstrap 令牌，未传时走请求层默认注入） */
function withStageToken(stageToken?: string) {
  return stageToken ? { headers: { Authorization: `Bearer ${stageToken}` } } : {}
}

/** 获取系统设置（bootstrap 阶段传 stageToken） */
export function getSettings(stageToken?: string) {
  return service.get<unknown, ApiResponse<SystemSettings>>('/settings', {
    silent: true,
    ...withStageToken(stageToken),
  })
}

/** 更新系统设置（含维护模式切换时触发 428 高风险二次验证，由请求层自动处理） */
export function updateSettings(data: Record<string, unknown>, stageToken?: string) {
  return service.put<unknown, ApiResponse>('/settings', data, withStageToken(stageToken))
}

/** 测试 SMTP 发信（可携带未保存的 SMTP 配置直接测试；bootstrap 阶段传 stageToken） */
export function testSMTP(data: { email: string } & Record<string, unknown>, stageToken?: string) {
  return service.post<unknown, ApiResponse>('/settings/smtp/test', data, withStageToken(stageToken))
}

/** 手动轮换 JWT 密钥（高风险操作） */
export function rotateJWTSecret() {
  return service.post<unknown, ApiResponse>('/settings/jwt-secret/rotate', {})
}

/** 获取当前用户存储 ISO 目录路径（一键替换系统 ISO 存放位置） */
export function getUserStorageISOPath() {
  return service.get<unknown, ApiResponse<{ iso_path?: string }>>(
    '/settings/user-storage-iso-path',
    { silent: true },
  )
}

/** 保存 CPU 亲和性预设列表（管理员） */
export function saveCPUAffinityPresets(data: { presets: CpuAffinityPreset[] }) {
  return service.put<unknown, ApiResponse>('/settings/cpu-affinity-presets', data)
}

// ==================== 宿主机级配置（KSM / zRAM / KVM 兼容性 / 硬件直通） ====================

/** 宿主机挡位描述（KSM / zRAM 共用） */
export interface HostProfileOption {
  key: string
  name: string
  description: string
}

/** KSM 内存去重状态 */
export interface KSMStatus {
  supported: boolean
  enabled: boolean
  message?: string
  current_profile?: string
  persistent_configured?: boolean
  persistent_profile?: string
  profiles?: HostProfileOption[]
  runtime_config?: {
    run?: number
    pages_to_scan?: number
    sleep_millisecs?: number
    merge_across_nodes?: boolean | number
    use_zero_pages?: boolean | number
    smart_scan?: boolean | number
  }
  metrics?: {
    pages_shared?: number
    pages_sharing?: number
    pages_unshared?: number
    pages_scanned?: number
    full_scans?: number
  }
}

/** 获取宿主机 KSM 状态 */
export function getHostKSMStatus() {
  return service.get<unknown, ApiResponse<KSMStatus>>('/host/ksm', { silent: true })
}

/** 设置宿主机 KSM 挡位 */
export function updateHostKSMProfile(data: { profile: string }) {
  return service.put<unknown, ApiResponse<KSMStatus>>('/host/ksm', data, { timeout: 30000 })
}

/** zRAM 压缩内存状态 */
export interface ZRAMStatus {
  supported: boolean
  enabled: boolean
  message?: string
  current_profile?: string
  persistent_configured?: boolean
  persistent_profile?: string
  profiles?: HostProfileOption[]
  runtime_config?: {
    device?: string
    size_mb?: number
    used_mb?: number
    algorithm?: string
    priority?: number
  }
}

/** 获取宿主机 zRAM 状态 */
export function getHostZRAMStatus() {
  return service.get<unknown, ApiResponse<ZRAMStatus>>('/host/zram', { silent: true })
}

/** 设置宿主机 zRAM 挡位 */
export function updateHostZRAMProfile(data: { profile: string }) {
  return service.put<unknown, ApiResponse<ZRAMStatus>>('/host/zram', data, { timeout: 30000 })
}

/** Intel KVM Unrestricted Guest 状态 */
export interface KVMUnrestrictedGuestStatus {
  supported: boolean
  message?: string
  runtime_available?: boolean
  runtime_enabled?: boolean
  persistent_configured?: boolean
  persistent_enabled?: boolean
  requires_reload?: boolean
  active_vm_count?: number
}

/** 获取宿主机 Intel KVM unrestricted_guest 状态 */
export function getHostKVMUnrestrictedGuestStatus() {
  return service.get<unknown, ApiResponse<KVMUnrestrictedGuestStatus>>(
    '/host/kvm-intel-unrestricted-guest',
    { silent: true },
  )
}

/** 设置宿主机 Intel KVM unrestricted_guest */
export function updateHostKVMUnrestrictedGuest(data: { enabled: boolean }) {
  return service.put<unknown, ApiResponse<KVMUnrestrictedGuestStatus>>(
    '/host/kvm-intel-unrestricted-guest',
    data,
    { timeout: 30000 },
  )
}

/** 可直通设备 */
export interface PassthroughDevice {
  pci_address: string
  product_name?: string
  is_vfio_bound?: boolean
  current_driver?: string
  is_active_framebuffer?: boolean
  iommu_group?: number
}

/** 硬件直通环境状态 */
export interface HardwarePassthroughStatus {
  ready?: boolean
  ready_message?: string
  cpu_virt_flag?: string
  bios_iommu_enabled?: boolean
  bios_iommu_message?: string
  iommu_enabled?: boolean
  iommu_type?: string
  iommu_in_cmdline?: boolean
  vfio_pci_loaded?: boolean
  passthrough_devices?: PassthroughDevice[]
}

/** 获取硬件直通状态 */
export function getHardwarePassthroughStatus() {
  return service.get<unknown, ApiResponse<HardwarePassthroughStatus>>(
    '/host/hardware-passthrough/status',
    { silent: true },
  )
}

/** 一键开启 IOMMU（写入 grub + update-grub） */
export function enableIommu() {
  return service.post<unknown, ApiResponse>('/host/hardware-passthrough/enable-iommu', {}, {
    timeout: 60000,
  })
}

/** 一键加载 vfio-pci 模块 */
export function loadVfioPci() {
  return service.post<unknown, ApiResponse>('/host/hardware-passthrough/load-vfio', {}, {
    timeout: 30000,
  })
}

// ==================== 日志管理 ====================

/** 日志文件项 */
export interface LogFileItem {
  name: string
  category: string
  size: number
  mod_time: string
  is_today?: boolean
}

/** 日志磁盘占用状态 */
export interface LogStatus {
  total_size: number
  total_size_human: string
  files: LogFileItem[]
  categories?: string[]
}

/** 获取日志状态（文件列表、磁盘占用） */
export function getLogStatus() {
  return service.get<unknown, ApiResponse<LogStatus>>('/settings/log/status', { silent: true })
}

/** 删除日志文件 */
export function deleteLogs(data: { files: string[] }) {
  return service.post<unknown, ApiResponse>('/settings/log/delete', data)
}

/** 导出日志文件（blob 响应由请求层直接放行，需取 .data 使用） */
export function exportLogs(data: { files: string[] }) {
  return service.post<unknown, AxiosResponse<Blob>>('/settings/log/export', data, {
    responseType: 'blob',
    timeout: 120000,
  })
}

// ==================== 诊断导出 ====================

/** 诊断类别 */
export interface DiagnosticCategory {
  id: string
  label: string
  description?: string
}

/** 获取诊断类别列表 */
export function getDiagnosticCategories() {
  return service.get<unknown, ApiResponse<DiagnosticCategory[]>>(
    '/settings/diagnostics/categories',
    { silent: true },
  )
}

/** 收集并导出诊断信息（收集耗时较长，超时放宽到 120s） */
export function exportDiagnostics(data: { categories: string[] }) {
  return service.post<unknown, AxiosResponse<Blob>>('/settings/diagnostics/export', data, {
    responseType: 'blob',
    timeout: 120000,
  })
}

// ==================== 组件版本健康度（M7.2 / §5.11.5） ====================

/** 单个组件健康度条目 */
export interface ComponentHealthItem {
  component: string
  category: string
  status: 'healthy' | 'warning' | 'critical' | 'info'
  current_version: string
  required_version: string
  recommended_version: string
  message: string
  upgrade_hint: string
}

/** 组件版本健康度聚合结果 */
export interface ComponentHealth {
  overall: 'healthy' | 'warning' | 'critical'
  last_check: string
  items: ComponentHealthItem[]
}

/** 重新探测组件版本健康度（POST /settings/diagnostics/refresh） */
export function refreshDiagnostics() {
  return service.post<unknown, ApiResponse<ComponentHealth>>(
    '/settings/diagnostics/refresh',
    {},
    { silent: true },
  )
}

/** 发行版支持等级条目（compat-manifest os_compat，M8.11/§14 P3-11） */
export interface OsSupportEntry {
  firewall?: string
  glibc?: string
  recommended_tier?: string
  /** S=官方全量回归、A=核心功能回归、B=社区自测、C=理论兼容 */
  support_level: 'S' | 'A' | 'B' | 'C'
  certified_hardware: string[]
}

/** 支持等级元数据（S/A/B/C → 中文名 + 描述） */
export interface SupportLevelMeta {
  level: 'S' | 'A' | 'B' | 'C'
  name: string
  description: string
}

/** 发行版支持等级矩阵响应 */
export interface OsSupportResponse {
  os_compat: Record<string, OsSupportEntry>
  meta: SupportLevelMeta[]
  /** 当前系统识别（C1）：命中 os_compat 则为其支持等级，否则 null */
  current_os?: OsSupportEntry | null
  /** 当前系统 PRETTY_NAME */
  os_release?: string
}

/** 获取各发行版支持等级矩阵（GET /settings/diagnostics/os-support） */
export function getOsSupport() {
  return service.get<unknown, ApiResponse<OsSupportResponse>>('/settings/diagnostics/os-support', {
    silent: true,
  })
}

// ==================== 用户存储维护 ====================

/** 存储回收结果 */
export interface TrimStorageResult {
  image_path: string
  mount_point: string
  before_blocks: number
  after_blocks: number
  trimmed_bytes: number
  trimmed_human: string
}

export interface TrimStorageTask {
  id: number
  status: string
}

/** 执行用户存储回收（fstrim + fallocate --dig-holes） */
export function trimUserStorage() {
  return service.post<unknown, ApiResponse<{ task: TrimStorageTask; reused: boolean }>>(
    '/settings/storage/trim',
    {},
  )
}

/** 宿主机公开信息（架构 / SPICE 支持等） */
export interface PublicSystemInfo {
  arch?: string
  qemu_spice?: boolean
  /** CPU 指令集与厂商（管理员专属字段，M8.1/P0-1 新增 cpu_vendor） */
  cpu?: {
    avx2?: boolean
    fma?: boolean
    vendor?: string
    cpu_vendor?: string
  }
  /** 国产化组件诊断（v0.9.3/#Q） */
  firewall?: {
    backend?: string
    available?: boolean
    active?: boolean
    version?: string
    ip_backend?: string
    nm_managed?: boolean
    docker_compatible?: boolean
    error_code?: string
    upgrade_advice?: UpgradeAdvice
  }
  /** 组件版本健康度（M7.2/§5.11.5，v0.9.8 新增） */
  component_health?: ComponentHealth
  [key: string]: unknown
}

/** 组件升级提示（#Q，§4.1）：多命中时前端按优先级 firewalld_unsupported > firewalld_old > glibc_low > selinux 取一条 */
export interface UpgradeAdvice {
  firewalld_unsupported?: boolean
  firewalld_old?: boolean
  glibc_low_for_native?: boolean
  selinux_enforcing?: boolean
}

/** 获取宿主机公开系统信息（M5：并发单飞去重——多个页面同时挂载时只发起一次请求，后端缓存已进一步缓解） */
let publicSystemInfoInFlight: Promise<ApiResponse<PublicSystemInfo>> | null = null

export function getPublicSystemInfo() {
  if (!publicSystemInfoInFlight) {
    publicSystemInfoInFlight = service
      .get<unknown, ApiResponse<PublicSystemInfo>>('/system-info', { silent: true })
      .finally(() => {
        publicSystemInfoInFlight = null
      })
  }
  return publicSystemInfoInFlight
}

/** 获取宿主机 CPU 物理核心数（CPU 热添加上限） */
export function getHostCPUCores() {
  return service.get<unknown, ApiResponse<{ cores: number }>>('/host/cpus', { silent: true })
}

/** CPU 亲和性预设 */
export interface CpuAffinityPreset {
  name: string
  value: string
}

/** 获取 CPU 亲和性预设列表 */
export function getCPUAffinityPresets() {
  return service.get<unknown, ApiResponse<CpuAffinityPreset[]>>('/cpu-affinity-presets', {
    silent: true,
  })
}
