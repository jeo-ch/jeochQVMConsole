/**
 * 诊断 Tab：诊断类别收集导出 + 组件版本健康度卡片（M7.2/§5.11.5）+ 发行版支持等级卡片（M8.11/§14 P3-11）+ 面板运行状态卡片（M8.10/§14 P2-10，健康探针）
 */
import { useEffect, useMemo, useState } from 'react'
import { Banner, Button, Checkbox, CheckboxGroup, Divider, Spin, Tag, Toast, Typography } from '@douyinfe/semi-ui'
import { IconDownload, IconRefresh } from '@douyinfe/semi-icons'
import {
  exportDiagnostics,
  getDiagnosticCategories,
  getOsSupport,
  getPublicSystemInfo,
  refreshDiagnostics,
  type ComponentHealth,
  type ComponentHealthItem,
  type DiagnosticCategory,
  type OsSupportEntry,
  type SupportLevelMeta,
} from '@/api/settings'
import { getHealthProbeLatest, type HealthProbe } from '@/api/health'
import { downloadBlob, timestampFilename } from '@/utils/download'

/** 分区元信息（与 §5.11.2 分类规则一致） */
const CATEGORY_META: { key: string; label: string }[] = [
  { key: 'core', label: '核心组件' },
  { key: 'disk', label: '磁盘/镜像/初始化' },
  { key: 'diag', label: '诊断/扩展（可选）' },
]

/** 健康度状态 Tag 颜色（TagColor 子集，Semi 根导出无该类型故本地声明） */
type HealthTagColor = 'green' | 'orange' | 'red' | 'grey'

const STATUS_META: Record<string, { color: HealthTagColor; label: string }> = {
  healthy: { color: 'green', label: '健康' },
  warning: { color: 'orange', label: '警告' },
  critical: { color: 'red', label: '不满足' },
  info: { color: 'grey', label: '提示' },
}

const OVERALL_META: Record<string, { color: HealthTagColor; label: string }> = {
  healthy: { color: 'green', label: '整体健康' },
  warning: { color: 'orange', label: '整体警告' },
  critical: { color: 'red', label: '整体不满足' },
}

/** 支持等级 → 说明文案（与后端 SupportLevelMetas 对齐，M8.11/§14 P3-11） */
const SUPPORT_LEVEL_DESC: Record<string, string> = {
  S: '官方全量回归，生产推荐',
  A: '核心功能回归，可用于生产',
  B: '社区自测通过，谨慎用于生产',
  C: '仅理论兼容，生产请升级到认证基线',
}

/** 秒 → 「Xd Yh Zm」运行时长（面板健康探针展示，M8.10） */
function formatUptime(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) return '-'
  const s = Math.floor(totalSeconds)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}天 ${h}时 ${m}分`
  if (h > 0) return `${h}时 ${m}分`
  return `${m}分 ${s % 60}秒`
}

export default function DiagnosticsTab() {
  const [categories, setCategories] = useState<DiagnosticCategory[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [health, setHealth] = useState<ComponentHealth | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [osSupport, setOsSupport] = useState<Record<string, OsSupportEntry> | null>(null)
  const [osMeta, setOsMeta] = useState<SupportLevelMeta[]>([])
  const [currentOs, setCurrentOs] = useState<OsSupportEntry | null>(null)
  const [currentOsName, setCurrentOsName] = useState('')
  const [probe, setProbe] = useState<HealthProbe | null>(null)

  useEffect(() => {
    setLoading(true)
    getDiagnosticCategories()
      .then((res) => {
        const list = res.data || []
        setCategories(list)
        // 默认全选
        setSelected(list.map((c) => c.id))
      })
      .catch(() => {})
      .finally(() => setLoading(false))

    // 组件版本健康度（挂载点 /system-info，M7.2/§5.11.5）
    getPublicSystemInfo()
      .then((res) => {
        if (res.data?.component_health) {
          setHealth(res.data.component_health)
        }
      })
      .catch(() => {})

    // 发行版支持等级矩阵（M8.11/§14 P3-11）
    getOsSupport()
      .then((res) => {
        setOsSupport(res.data?.os_compat || null)
        setOsMeta(res.data?.meta || [])
        setCurrentOs(res.data?.current_os || null)
        setCurrentOsName(res.data?.os_release || '')
      })
      .catch(() => {})

    // 面板健康探针快照（M8.10/§14 P2-10，经 /api/system/health/latest 暴露，§14.5 候选①）
    void getHealthProbeLatest()
      .then((res) => setProbe(res.data || null))
      .catch(() => {})
  }, [])

  const handleExport = async () => {
    if (selected.length === 0) {
      Toast.warning('请至少选择一个诊断类别')
      return
    }
    setExporting(true)
    try {
      const res = await exportDiagnostics({ categories: selected })
      downloadBlob(res.data, timestampFilename('qvmconsole-diagnostics', 'zip'))
      Toast.success('诊断信息导出成功')
    } catch {
      // 请求层已统一提示
    } finally {
      setExporting(false)
    }
  }

  const handleRefreshHealth = async () => {
    setRefreshing(true)
    const prevCheck = health?.last_check
    try {
      const res = await refreshDiagnostics()
      setHealth(res.data)
      // 5s 冷却期（H3）内后端返回上次探测结果：last_check 未变即冷却命中，提示而非误导为已刷新
      if (res.data?.last_check && res.data.last_check === prevCheck) {
        Toast.info('检测进行中，冷却期内返回上次探测结果（约 5 秒后可再次刷新）')
      } else {
        Toast.success('组件版本健康度已刷新')
      }
    } catch {
      // 请求层已统一提示
    } finally {
      setRefreshing(false)
    }
  }

  // 导出报告（§5.11.5 / §8 验收）：导出组件健康度 JSON，便于离线排查或提 issue 时附带
  const handleExportReport = () => {
    if (!health) {
      Toast.warning('暂无组件版本健康度数据可导出')
      return
    }
    const payload = {
      exported_at: new Date().toISOString(),
      component_health: health,
    }
    downloadBlob(
      new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' }),
      timestampFilename('qvmconsole-component-health', 'json'),
    )
    Toast.success('组件版本健康度报告导出成功')
  }

  const groupedItems = useMemo(() => {
    if (!health) return []
    return CATEGORY_META.map((cat) => ({
      ...cat,
      items: health.items.filter((it) => it.category === cat.key),
    })).filter((g) => g.items.length > 0)
  }, [health])

  return (
    <div className="stg-tab-pane">
      <Banner
        type="info"
        closeIcon={null}
        className="stg-banner"
        description="此功能收集系统及面板诊断信息用于排查问题。所有数据仅用于诊断分析，不会修改任何系统状态。"
      />

      {/* 面板健康状态卡片（M8.10/§14 P2-10，§14.5 候选①） */}
      <div className="stg-comp-health">
        <div className="stg-comp-health-header">
          <h4 className="stg-comp-title">面板运行状态</h4>
          {probe && (
            <Tag
              color={
                probe.maintenance_mode ? 'orange' : probe.libvirt_ready ? 'green' : 'yellow'
              }
              type="light"
            >
              {probe.maintenance_mode
                ? '维护模式'
                : probe.libvirt_ready
                  ? '正常'
                  : 'libvirt 不可用'}
            </Tag>
          )}
        </div>
        {probe ? (
          <div className="stg-os-meta stg-probe-items">
            <div className="stg-comp-item">
              <span className="stg-comp-name">上次探针</span>
              <span className="stg-comp-ver">
                {probe.timestamp ? new Date(probe.timestamp).toLocaleString() : '-'}
              </span>
            </div>
            <div className="stg-comp-item">
              <span className="stg-comp-name">运行时长</span>
              <span className="stg-comp-ver">{formatUptime(probe.service_uptime_s)}</span>
            </div>
            <div className="stg-comp-item">
              <span className="stg-comp-name">libvirtd</span>
              <span className="stg-comp-ver">{probe.libvirt_daemon ? '运行中' : '未运行'}</span>
            </div>
            <div className="stg-comp-item">
              <span className="stg-comp-name">面板版本</span>
              <span className="stg-comp-ver">{probe.version || '-'}</span>
            </div>
          </div>
        ) : (
          <div className="stg-comp-empty">面板健康探针数据暂不可用</div>
        )}
      </div>

      <Divider margin="16px" />

      <Spin spinning={loading}>
        <div className="stg-diag-toolbar">
          <Button
            size="small"
            theme="borderless"
            type="primary"
            onClick={() =>
              setSelected(
                selected.length === categories.length ? [] : categories.map((c) => c.id),
              )
            }
          >
            {selected.length === categories.length && categories.length > 0 ? '取消全选' : '全选'}
          </Button>
        </div>
        <CheckboxGroup
          value={selected}
          onChange={(v) => setSelected((v || []).map(String))}
          direction="vertical"
        >
          {categories.map((cat) => (
            <Checkbox key={cat.id} value={cat.id}>
              <span className="stg-diag-label">{cat.label}</span>
              {cat.description && <span className="stg-diag-desc">{cat.description}</span>}
            </Checkbox>
          ))}
        </CheckboxGroup>
      </Spin>

      <Divider margin="16px" />

      {/* 组件版本健康度卡片（M7.2/§5.11.5） */}
      <div className="stg-comp-health">
        <div className="stg-comp-health-header">
          <h4 className="stg-comp-title">组件版本健康度</h4>
          {health && (
            <Tag color={OVERALL_META[health.overall]?.color ?? 'grey'} type="light">
              {OVERALL_META[health.overall]?.label ?? health.overall}
            </Tag>
          )}
          <Button
            size="small"
            theme="borderless"
            type="primary"
            icon={refreshing ? <IconRefresh spin /> : <IconRefresh />}
            loading={refreshing}
            onClick={() => void handleRefreshHealth()}
          >
            刷新
          </Button>
          <Button
            size="small"
            theme="borderless"
            type="tertiary"
            icon={<IconDownload />}
            disabled={!health}
            onClick={() => void handleExportReport()}
          >
            导出报告
          </Button>
        </div>

        {health ? (
          <>
            <div className="stg-comp-last-check">
              最近探测: {health.last_check ? new Date(health.last_check).toLocaleString() : '-'}
            </div>
            {groupedItems.map((group) => (
              <div key={group.key} className="stg-comp-cat">
                <div className="stg-comp-cat-label">{group.label}</div>
                {group.items.map((item) => (
                  <ComponentHealthRow key={item.component} item={item} />
                ))}
              </div>
            ))}
          </>
        ) : (
          <div className="stg-comp-empty">组件版本健康度暂不可用</div>
        )}
      </div>

      <Divider margin="16px" />

      {/* 发行版支持等级矩阵（M8.11/§14 P3-11） */}
      <div className="stg-comp-health">
        <div className="stg-comp-health-header">
          <h4 className="stg-comp-title">发行版支持等级</h4>
          {currentOs && (
            <Tag color="green" type="light" size="small" className="stg-os-current">
              当前系统: {currentOsName || '本机'} · {currentOs.support_level} 级
            </Tag>
          )}
          {osMeta.length > 0 && (
            <div className="stg-os-meta">
              {osMeta.map((m) => (
                <Tag key={m.level} color="blue" type="light" size="small">
                  {m.level}: {m.name}
                </Tag>
              ))}
            </div>
          )}
        </div>
        {osSupport && Object.keys(osSupport).length > 0 ? (
          Object.entries(osSupport).map(([osName, entry]) => (
            <div key={osName} className="stg-comp-item">
              <Tag color="blue" type="light" size="small" className="stg-comp-status">
                {entry.support_level}
              </Tag>
              <span className="stg-comp-name">{osName}</span>
              {entry.glibc && <span className="stg-comp-ver">glibc {entry.glibc}</span>}
              {entry.recommended_tier && (
                <span className="stg-comp-req">档位: {entry.recommended_tier}</span>
              )}
              {entry.certified_hardware.length > 0 && (
                <span className="stg-comp-req">
                  认证硬件: {entry.certified_hardware.join(' / ')}
                </span>
              )}
              {SUPPORT_LEVEL_DESC[entry.support_level] && (
                <div className="stg-comp-msg">{SUPPORT_LEVEL_DESC[entry.support_level]}</div>
              )}
            </div>
          ))
        ) : (
          <div className="stg-comp-empty">发行版支持等级数据暂不可用</div>
        )}
      </div>

      <Divider margin="16px" />

      <div className="stg-diag-actions">
        <Button
          type="primary"
          theme="solid"
          icon={exporting ? <IconRefresh spin /> : <IconDownload />}
          loading={exporting}
          disabled={selected.length === 0}
          onClick={() => void handleExport()}
        >
          收集并导出
        </Button>
        {exporting && <span className="stg-diag-exporting">正在收集诊断信息，请耐心等待...</span>}
      </div>
    </div>
  )
}

function ComponentHealthRow({ item }: { item: ComponentHealthItem }) {
  const meta = STATUS_META[item.status] ?? { color: 'grey', label: item.status }
  return (
    <div className="stg-comp-item">
      <Tag color={meta.color} type="light" size="small" className="stg-comp-status">
        {meta.label}
      </Tag>
      <span className="stg-comp-name">{item.component}</span>
      <span className="stg-comp-ver">{item.current_version || '(缺失)'}</span>
      {item.required_version && <span className="stg-comp-req">最低 ≥ {item.required_version}</span>}
      {item.recommended_version && (
        <span className="stg-comp-req">推荐 ≥ {item.recommended_version}</span>
      )}
      {item.message && <div className="stg-comp-msg">{item.message}</div>}
      {item.upgrade_hint && (
        <div className="stg-comp-hint">
          <Typography.Text copyable={{ content: item.upgrade_hint }} className="stg-comp-hint-text">
            {item.upgrade_hint}
          </Typography.Text>
        </div>
      )}
    </div>
  )
}