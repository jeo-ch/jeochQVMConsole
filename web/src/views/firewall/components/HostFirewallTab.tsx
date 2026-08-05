/**
 * 宿主机防火墙 Tab（后端抽象：UFW / Firewalld / none）
 * - 状态横幅：启用/关闭 + 开启/关闭操作
 * - 左侧运行状态卡：防火墙后端、默认策略、SSH/面板端口、Docker 兼容说明、错误 hint（#R）
 * - 右侧规则表：端口/协议/动作/备注筛选，保护行禁止编辑删除
 * - #L 自检失败清单与回滚；#Q 组件升级提示 Banner（至多一条，可关闭）
 */
import { useMemo, useState } from 'react'
import { Banner, Button, Col, Input, Row, Select, Table, Tag, Tooltip } from '@douyinfe/semi-ui'
import {
  IconAlertTriangle,
  IconClose,
  IconDelete,
  IconEditStroked,
  IconInfoCircle,
  IconList,
  IconPlay,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconSettingStroked,
  IconTickCircle,
  IconVideo,
} from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import type { HostFirewallRule, HostFirewallStatus } from '@/api/firewall'
import type { UpgradeAdvice } from '@/api/settings'
import { formatRulePort } from '../utils'

interface HostFirewallTabProps {
  hostStatus: HostFirewallStatus | null
  loading: boolean
  /** 开启按钮预览请求中 */
  enableLoading: boolean
  /** #R：重新检测后端中 */
  backendResetting: boolean
  /** #Q：组件升级提示（v0.9.3，至多一条 Banner） */
  upgradeAdvice: UpgradeAdvice | null
  /** #L：Enable 任务自检失败项清单（null 表示无） */
  selfCheckFailures: string[] | null
  onEnable: () => void
  onDisable: () => void
  /** #R：重新检测（POST /firewall/host/reset-backend） */
  onResetBackend: () => void
  /** #L：自检失败后的回滚入口 */
  onRollbackEnable: () => void
  onAddVncDefault: () => void
  onEditRule: (row?: HostFirewallRule) => void
  onDeleteRule: (row: HostFirewallRule) => void
}

/** #R：错误码 → 可操作提示文案映射 */
const ERROR_CODE_HINTS: Record<string, { text: string; command: string }> = {
  FIREWALLD_NOT_RUNNING: { text: 'firewalld 服务未运行', command: 'systemctl start firewalld' },
  FIREWALLD_OLD_VERSION: { text: 'firewalld 版本过旧，端口转发可靠性受限', command: '' },
  FIREWALLD_COMMAND_FAILED: { text: 'firewall-cmd 命令执行失败', command: '' },
  ZONE_NOT_BOUND: { text: '接口未绑定专用 zone', command: 'firewall-cmd --get-zones' },
  DBUS_ERROR: { text: 'firewalld D-Bus 连接异常', command: 'systemctl restart firewalld' },
  PERMISSION_DENIED: { text: '权限不足，请检查运行账户', command: '' },
}

/** #Q：upgrade_advice 优先级（firewalld_unsupported > firewalld_old > glibc_low > selinux），命中第一条展示 */
function pickUpgradeAdvice(advice: UpgradeAdvice | null): string | null {
  if (!advice) return null
  if (advice.firewalld_unsupported) {
    return 'firewalld 版本低于 0.6，面板不启用宿主机防火墙统一管理，请升级至 0.6+ 或使用发行版 iptables-service'
  }
  if (advice.firewalld_old) {
    return 'firewalld 版本过旧，端口转发/SPICE 公网可靠性受限，建议升级至 0.9+'
  }
  if (advice.glibc_low_for_native) {
    return '当前使用 compat 档，升级 glibc 后系统将满足 native 档启用条件'
  }
  if (advice.selinux_enforcing) {
    return 'SELinux Enforcing 下 zone 文件已 restorecon 处理'
  }
  return null
}

export default function HostFirewallTab({
  hostStatus,
  loading,
  enableLoading,
  backendResetting,
  upgradeAdvice,
  selfCheckFailures,
  onEnable,
  onDisable,
  onResetBackend,
  onRollbackEnable,
  onAddVncDefault,
  onEditRule,
  onDeleteRule,
}: HostFirewallTabProps) {
  // ==================== 筛选 ====================
  const [portSearch, setPortSearch] = useState('')
  const [protocolFilter, setProtocolFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [remarkSearch, setRemarkSearch] = useState('')
  /** #Q：advice Banner 已关闭（会话内不再展示） */
  const [adviceDismissed, setAdviceDismissed] = useState(false)

  const rules = useMemo(() => hostStatus?.rules || [], [hostStatus])

  const filteredRules = useMemo(() => {
    let data = rules
    if (portSearch) {
      data = data.filter((r) => {
        const start = r.port_start ? String(r.port_start) : ''
        const end = r.port_end ? String(r.port_end) : ''
        return start.includes(portSearch) || end.includes(portSearch)
      })
    }
    if (protocolFilter) {
      data = data.filter((r) => r.protocol === protocolFilter)
    }
    if (actionFilter) {
      data = data.filter((r) => r.action === actionFilter)
    }
    if (remarkSearch) {
      const q = remarkSearch.toLowerCase()
      data = data.filter((r) => (r.comment || '').toLowerCase().includes(q))
    }
    return data
  }, [rules, portSearch, protocolFilter, actionFilter, remarkSearch])

  // ==================== #R 错误 hint ====================
  const errorHint = hostStatus?.error_code
    ? ERROR_CODE_HINTS[hostStatus.error_code] || {
        text: `防火墙后端异常（${hostStatus.error_code}）`,
        command: '',
      }
    : null

  // ==================== #Q advice Banner ====================
  const adviceText = adviceDismissed ? null : pickUpgradeAdvice(upgradeAdvice)

  // ==================== 表格 ====================
  const columns: ColumnProps<HostFirewallRule>[] = [
    {
      title: '动作',
      dataIndex: 'action',
      width: 90,
      align: 'center',
      render: (text) => (
        <Tag size="small" color={text === 'allow' ? 'green' : 'red'}>
          {text === 'allow' ? '允许' : '拒绝'}
        </Tag>
      ),
    },
    {
      title: '协议',
      dataIndex: 'protocol',
      width: 90,
      align: 'center',
      render: (text) => <span className="qvm-mono">{(text || '').toUpperCase()}</span>,
    },
    {
      title: '端口',
      dataIndex: 'port_start',
      width: 130,
      align: 'center',
      render: (_text, row) => <span className="qvm-mono">{formatRulePort(row)}</span>,
    },
    {
      title: '来源',
      dataIndex: 'source_cidr',
      render: (text) => <span className="qvm-mono">{text || 'any'}</span>,
    },
    {
      title: '备注',
      dataIndex: 'comment',
      render: (text) => (
        <Tooltip content={text || ''} position="top" showArrow={false}>
          <span className="fw-ellipsis">{text || '-'}</span>
        </Tooltip>
      ),
    },
    {
      title: '状态',
      dataIndex: 'protected',
      width: 140,
      align: 'center',
      render: (_text, row) => {
        if (row.protected) {
          return <Tag size="small" color="red">{row.protected_reason || '保护规则'}</Tag>
        }
        if (row.managed_by_panel) {
          return <Tag size="small" color="grey">面板管理</Tag>
        }
        return <span className="fw-muted">—</span>
      },
    },
    {
      title: '操作',
      dataIndex: 'actions',
      width: 80,
      align: 'center',
      render: (_text, row) => (
        <div className="fw-act-cell">
          <Tooltip content={row.protected ? '保护规则不可编辑' : '编辑'} position="top">
            <span
              className={`fw-act-ic edit${row.protected ? ' disabled' : ''}`}
              onClick={() => !row.protected && onEditRule(row)}
            >
              <IconEditStroked />
            </span>
          </Tooltip>
          <Tooltip content={row.protected ? '保护规则不可删除' : '删除'} position="top">
            <span
              className={`fw-act-ic delete${row.protected ? ' disabled' : ''}`}
              onClick={() => !row.protected && onDeleteRule(row)}
            >
              <IconDelete />
            </span>
          </Tooltip>
        </div>
      ),
    },
  ]

  // #O：转发默认「未管理」时按 ip_backend 区分 Tooltip 文案
  const routedTip = !hostStatus?.default_routed
    ? hostStatus?.ip_backend === 'legacy'
      ? '依赖面板 iptables FORWARD（可靠）'
      : '依赖 zone/policy 绑定，勿依赖面板 iptables 顺序'
    : ''

  return (
    <div className="fw-tab-pane">
      {/* #Q：组件升级提示 Banner（v0.9.3，至多一条，可关闭） */}
      {adviceText && (
        <Banner
          type="warning"
          title="组件升级提示"
          description={adviceText}
          onClose={() => setAdviceDismissed(true)}
          style={{ marginBottom: 16 }}
        />
      )}

      {/* 状态横幅 */}
      <div className={`fw-banner ${hostStatus?.active ? 'enabled' : 'disabled'}`}>
        <div className="fw-banner-icon">
          {hostStatus?.active ? <IconTickCircle /> : <IconAlertTriangle />}
        </div>
        <div className="fw-banner-body">
          <div className="fw-banner-title">
            {hostStatus?.active ? '宿主机防火墙已启用' : '宿主机防火墙已关闭'}
          </div>
          <div className="fw-banner-desc">
            {hostStatus?.active
              ? '防火墙规则正在保护宿主机入站流量'
              : '端口转发仍会写入防火墙持久放通规则'}
          </div>
        </div>
        <div className="fw-banner-actions">
          <Button
            type="primary"
            theme="light"
            icon={<IconPlay />}
            loading={enableLoading}
            onClick={onEnable}
          >
            开启防火墙
          </Button>
          <Button type="warning" theme="light" icon={<IconClose />} onClick={onDisable}>
            关闭防火墙
          </Button>
        </div>
      </div>

      {/* #L：Enable 自检失败清单 + 回滚入口（状态横幅下方） */}
      {selfCheckFailures && selfCheckFailures.length > 0 && (
        <div className="fw-banner warning" style={{ marginBottom: 12 }}>
          <div className="fw-banner-icon">
            <IconAlertTriangle />
          </div>
          <div className="fw-banner-body">
            <div className="fw-banner-title">启用后自检失败</div>
            <div className="fw-banner-desc" style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 8 }}>
              {selfCheckFailures.map((item) => (
                <Tooltip key={item} content={item} position="top">
                  <Tag size="small" color="red">{item}</Tag>
                </Tooltip>
              ))}
            </div>
          </div>
          <div className="fw-banner-actions">
            <Button type="danger" theme="light" icon={<IconClose />} onClick={onRollbackEnable}>
              回滚（关闭防火墙）
            </Button>
          </div>
        </div>
      )}

      {/* backend === 'none'：追加不可用提示（#S） */}
      {hostStatus?.backend === 'none' && (
        <Banner
          type="warning"
          closeIcon={null}
          icon={<IconAlertTriangle />}
          description="当前系统无 ufw/firewalld，宿主机防火墙不可用，端口转发仍会写入 iptables。"
          style={{ marginBottom: 16 }}
        />
      )}

      <Row gutter={[16, 16]}>
        {/* 运行状态 */}
        <Col xs={24} md={9} lg={8}>
          <div className="fw-card">
            <div className="fw-card-header">
              <IconSettingStroked />
              <span>运行状态</span>
            </div>
            <div className="fw-info-list">
              <div className="fw-info-item">
                <span className="fw-info-label">防火墙后端</span>
                <Tag size="small" color={hostStatus?.ufw_available ? 'green' : 'red'}>
                  {hostStatus?.backend_name || (hostStatus?.ufw_available ? '可用' : '不可用')}
                </Tag>
              </div>
              <div className="fw-info-item">
                <span className="fw-info-label">入站默认</span>
                <Tag size="small" color={hostStatus?.default_incoming === 'allow' ? 'red' : 'green'}>
                  {hostStatus?.default_incoming || '-'}
                </Tag>
              </div>
              <div className="fw-info-item">
                <span className="fw-info-label">出站默认</span>
                <Tag size="small" color={hostStatus?.default_outgoing === 'allow' ? 'green' : 'grey'}>
                  {hostStatus?.default_outgoing || '-'}
                </Tag>
              </div>
              <div className="fw-info-item">
                <span className="fw-info-label">转发默认</span>
                {hostStatus?.default_routed ? (
                  <Tag
                    size="small"
                    color={hostStatus.default_routed === 'allow' ? 'orange' : 'green'}
                  >
                    {hostStatus.default_routed}
                  </Tag>
                ) : (
                  <Tooltip content={routedTip} position="top" showArrow={false}>
                    <Tag size="small" color="grey">
                      未管理
                    </Tag>
                  </Tooltip>
                )}
              </div>
              <div className="fw-info-item">
                <span className="fw-info-label">SSH 端口</span>
                <span className="qvm-mono">{(hostStatus?.ssh_ports || []).join(', ') || '-'}</span>
              </div>
              <div className="fw-info-item">
                <span className="fw-info-label">面板端口</span>
                <span className="qvm-mono">{(hostStatus?.panel_ports || []).join(', ') || '-'}</span>
              </div>
            </div>
            {errorHint && (
              <div className="fw-card-footer">
                <IconAlertTriangle />
                <div style={{ display: 'flex', flexDirection: 'column', gap: 6, width: '100%' }}>
                  <span>{errorHint.text}</span>
                  {errorHint.command && (
                    <span className="qvm-mono" style={{ fontSize: 11.5 }}>
                      {errorHint.command}
                    </span>
                  )}
                  <Button
                    size="small"
                    icon={<IconRefresh spin={backendResetting} />}
                    loading={backendResetting}
                    onClick={onResetBackend}
                  >
                    重新检测
                  </Button>
                </div>
              </div>
            )}
            {!errorHint && hostStatus?.docker_compatibility && (
              <div className="fw-card-footer">
                <IconInfoCircle />
                <span>{hostStatus.docker_compatibility}</span>
              </div>
            )}
          </div>
        </Col>

        {/* 宿主机规则 */}
        <Col xs={24} md={15} lg={16}>
          <div className="fw-card">
            <div className="fw-card-header">
              <IconList />
              <span>宿主机规则</span>
              <Tag size="small">{filteredRules.length} 条</Tag>
              <div className="fw-card-header-actions">
                <Button size="small" icon={<IconVideo />} onClick={onAddVncDefault}>
                  添加 VNC 5900-5999
                </Button>
                <Button
                  size="small"
                  type="primary"
                  theme="light"
                  icon={<IconPlus />}
                  onClick={() => onEditRule()}
                >
                  添加规则
                </Button>
              </div>
            </div>
            <div className="fw-filter-bar">
              <Input
                prefix={<IconSearch />}
                placeholder="搜索端口"
                value={portSearch}
                onChange={setPortSearch}
                showClear
                size="small"
                style={{ width: 130 }}
              />
              <Select
                value={protocolFilter}
                onChange={(v) => setProtocolFilter(v as string)}
                placeholder="协议筛选"
                showClear
                size="small"
                style={{ width: 120 }}
                optionList={[
                  { label: 'TCP', value: 'tcp' },
                  { label: 'UDP', value: 'udp' },
                ]}
              />
              <Select
                value={actionFilter}
                onChange={(v) => setActionFilter(v as string)}
                placeholder="动作筛选"
                showClear
                size="small"
                style={{ width: 120 }}
                optionList={[
                  { label: '允许', value: 'allow' },
                  { label: '拒绝', value: 'deny' },
                ]}
              />
              <Input
                prefix={<IconSearch />}
                placeholder="搜索备注"
                value={remarkSearch}
                onChange={setRemarkSearch}
                showClear
                size="small"
                style={{ width: 150 }}
              />
            </div>
            <Table<HostFirewallRule>
              rowKey="id"
              columns={columns}
              dataSource={filteredRules}
              loading={loading}
              pagination={false}
              size="small"
              empty="暂无防火墙规则"
              scroll={{ y: 460 }}
            />
          </div>
        </Col>
      </Row>
    </div>
  )
}
