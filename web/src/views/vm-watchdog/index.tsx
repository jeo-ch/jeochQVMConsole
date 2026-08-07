/**
 * VM 看门狗事件页（仅管理员，M8.9/§14 P2-9）
 * - 看门狗自动重置事件列表：支持状态/虚拟机/时间范围筛选，服务端分页
 * - 顶部概要说明看门狗自动重置行为
 */
import { useCallback, useEffect, useState } from 'react'
import {
  Button,
  DatePicker,
  Empty,
  Input,
  Pagination,
  Select,
  Table,
  Tag,
} from '@douyinfe/semi-ui'
import { IconLock, IconRefresh, IconSearch, IconStopwatchStroked } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import {
  getVMWatchdogEventList,
  type VMWatchdogEventItem,
} from '@/api/watchdog'
import { useUserStore } from '@/stores/user'
import { ROLES } from '@/config/constants'
import { formatDateTime } from '@/utils/format'
import '../scheduler/scheduler.css'

/** 事件状态 Tag 配色 */
function statusTagColor(status: string): 'red' | 'orange' | 'green' | 'grey' {
  const map: Record<string, 'red' | 'orange' | 'green'> = {
    reset: 'red',
    warning: 'orange',
    recovered: 'green',
  }
  return map[status] || 'grey'
}

/** 事件状态中文文案 */
function statusText(status: string): string {
  const map: Record<string, string> = {
    reset: '自动重置',
    warning: '预警',
    recovered: '恢复',
  }
  return map[status] || status
}

/** 时间参数格式化为后端支持的 "YYYY-MM-DD HH:mm:ss" */
function fmtTimeParam(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

interface EventFilters {
  status: string
  vmName: string
  range: Date[]
}

const EMPTY_FILTERS: EventFilters = { status: '', vmName: '', range: [] }

export default function VmWatchdogPage() {
  const role = useUserStore((s) => s.role)
  const isAdmin = role === ROLES.admin

  const [events, setEvents] = useState<VMWatchdogEventItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [filters, setFilters] = useState<EventFilters>(EMPTY_FILTERS)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  const fetchEvents = useCallback(
    async (targetPage: number, targetPageSize: number, f: EventFilters) => {
      setLoading(true)
      try {
        const res = await getVMWatchdogEventList({
          page: targetPage,
          page_size: targetPageSize,
          status: f.status || undefined,
          vm_name: f.vmName || undefined,
          start: f.range[0] ? fmtTimeParam(f.range[0]) : undefined,
          end: f.range[1] ? fmtTimeParam(f.range[1]) : undefined,
        })
        setEvents(res.data?.list || [])
        setTotal(res.data?.total || 0)
      } catch {
        // 请求层已统一提示
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  useEffect(() => {
    void fetchEvents(1, pageSize, EMPTY_FILTERS)
  }, [fetchEvents, pageSize])

  const handleSearch = useCallback(() => {
    setPage(1)
    void fetchEvents(1, pageSize, filters)
  }, [fetchEvents, filters, pageSize])

  const handleReset = useCallback(() => {
    setFilters(EMPTY_FILTERS)
    setPage(1)
    void fetchEvents(1, pageSize, EMPTY_FILTERS)
  }, [fetchEvents, pageSize])

  const columns: ColumnProps<VMWatchdogEventItem>[] = [
    {
      title: '触发时间',
      dataIndex: 'created_at',
      width: 170,
      render: (text) => <span className="qvm-mono">{formatDateTime(text)}</span>,
    },
    { title: '虚拟机', dataIndex: 'vm_name', width: 180 },
    {
      title: '类型',
      dataIndex: 'status',
      width: 110,
      align: 'center',
      render: (text) => (
        <Tag size="small" color={statusTagColor(text)}>
          {statusText(text)}
        </Tag>
      ),
    },
    {
      title: '原因',
      dataIndex: 'reason',
      ellipsis: { showTitle: true },
      render: (text) => text || '-',
    },
    {
      title: '结果',
      dataIndex: 'result_message',
      ellipsis: { showTitle: true },
      render: (text) => text || '-',
    },
  ]

  if (!isAdmin) {
    return (
      <div className="sch-page">
        <div className="sch-empty">
          <div className="sch-empty-icon">
            <IconLock />
          </div>
          <div>看门狗事件仅对管理员开放</div>
        </div>
      </div>
    )
  }

  return (
    <div className="sch-page">
      <div className="sch-page-header qvm-fade-up">
        <div>
          <h2>
            <IconStopwatchStroked style={{ marginRight: 8, color: 'var(--qvm-acc-ink)' }} />
            看门狗事件
          </h2>
          <p className="sch-page-sub">
            开启 VM 看门狗后，Guest Agent 连续失联达阈值会自动硬重置并在此记录，用于排查与审计
          </p>
        </div>
        <div className="sch-header-actions">
          <Button
            icon={<IconRefresh />}
            loading={loading}
            onClick={() => void fetchEvents(page, pageSize, filters)}
          >
            刷新
          </Button>
        </div>
      </div>

      <div className="sch-section-card qvm-fade-up">
        <div className="sch-filter-bar">
          <Select
            value={filters.status || undefined}
            onChange={(v) => setFilters((f) => ({ ...f, status: (v as string) || '' }))}
            placeholder="类型"
            showClear
            style={{ width: 130 }}
            optionList={[
              { label: '自动重置', value: 'reset' },
              { label: '预警', value: 'warning' },
              { label: '恢复', value: 'recovered' },
            ]}
          />
          <Input
            prefix={<IconSearch />}
            placeholder="虚拟机名称"
            value={filters.vmName}
            onChange={(v) => setFilters((f) => ({ ...f, vmName: v }))}
            showClear
            style={{ width: 180 }}
          />
          <DatePicker
            type="dateTimeRange"
            value={filters.range}
            onChange={(v) =>
              setFilters((f) => ({ ...f, range: Array.isArray(v) ? (v as Date[]) : [] }))
            }
            style={{ width: 380 }}
          />
          <Button type="primary" theme="light" onClick={handleSearch}>
            查询
          </Button>
          <Button onClick={handleReset}>重置</Button>
        </div>

        <Table<VMWatchdogEventItem>
          rowKey="id"
          columns={columns}
          dataSource={events}
          loading={loading}
          pagination={false}
          size="small"
          empty={<Empty description="暂无看门狗事件" />}
        />
        <div className="sch-pagination">
          <Pagination
            total={total}
            currentPage={page}
            pageSize={pageSize}
            pageSizeOpts={[10, 20, 50]}
            showSizeChanger
            showTotal
            onChange={(p, ps) => {
              setPage(p)
              setPageSize(ps)
              void fetchEvents(p, ps, filters)
            }}
          />
        </div>
      </div>
    </div>
  )
}