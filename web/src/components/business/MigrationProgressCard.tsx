/**
 * 网关迁移进度卡片
 * 显示实时传输速度、进度百分比、已用时间、预计剩余时间。
 * 用于 agent-based 跨节点迁移场景。
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { Card, Progress, Tag } from '@douyinfe/semi-ui'
import { IconRefresh } from '@douyinfe/semi-icons'
import { getGatewayMigrationStatus, type GatewayMigrationStatus } from '@/api/gateway'
import { formatBytes } from '@/utils/format'

interface MigrationProgressCardProps {
  taskId: number
  onComplete?: () => void
  onError?: (msg: string) => void
}

const statusConfig: Record<string, { color: 'blue' | 'green' | 'red' | 'grey'; label: string }> = {
  pending: { color: 'grey', label: '等待中' },
  running: { color: 'blue', label: '迁移中' },
  done: { color: 'green', label: '已完成' },
  failed: { color: 'red', label: '失败' },
}

export default function MigrationProgressCard({ taskId, onComplete, onError }: MigrationProgressCardProps) {
  const [status, setStatus] = useState<GatewayMigrationStatus | null>(null)
  const [error, setError] = useState<string>('')
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const completedRef = useRef(false)

  const poll = useCallback(async () => {
    try {
      const res = await getGatewayMigrationStatus(taskId)
      const data = res.data
      setStatus(data)

      if (data.status === 'done' && !completedRef.current) {
        completedRef.current = true
        if (timerRef.current) clearInterval(timerRef.current)
        onComplete?.()
      } else if (data.status === 'failed' && !completedRef.current) {
        completedRef.current = true
        if (timerRef.current) clearInterval(timerRef.current)
        const msg = data.message || '迁移失败'
        setError(msg)
        onError?.(msg)
      }
    } catch {
      // 轮询失败不中断，下次重试
    }
  }, [taskId, onComplete, onError])

  useEffect(() => {
    poll()
    timerRef.current = setInterval(poll, 2000)
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [poll])

  if (error) {
    return (
      <Card style={{ marginBottom: 16 }}>
        <div style={{ color: 'var(--qvm-color-danger, #ff6b6b)' }}>
          迁移失败: {error}
        </div>
      </Card>
    )
  }

  if (!status) {
    return (
      <Card style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <IconRefresh spin />
          <span>正在获取迁移状态...</span>
        </div>
      </Card>
    )
  }

  const detail = status.detail
  const isRunning = status.status === 'running'
  const progressPct = status.progress || 0
  const cfg = statusConfig[status.status] || statusConfig.pending

  return (
    <Card
      style={{ marginBottom: 16 }}
      headerStyle={{ padding: '12px 16px' }}
      bodyStyle={{ padding: '12px 16px' }}
      header={
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span style={{ fontWeight: 600 }}>迁移进度</span>
          <Tag color={cfg.color} style={{ marginLeft: 8 }}>
            {isRunning && <IconRefresh spin style={{ marginRight: 4 }} />}
            {cfg.label}
          </Tag>
        </div>
      }
    >
      {/* 进度条 */}
      <Progress
        percent={progressPct}
        stroke={status.status === 'done' ? 'var(--qvm-color-success, #52c41a)' : undefined}
        style={{ marginBottom: detail ? 12 : 0 }}
      />

      {/* 传输详情 */}
      {detail && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))',
            gap: 12,
            marginTop: 12,
          }}
        >
          <DetailItem
            label="已传输"
            value={`${formatBytes(detail.bytes_transferred)} / ${formatBytes(detail.total_bytes)}`}
          />
          <DetailItem label="速度" value={detail.speed_human} highlight />
          <DetailItem label="已用时间" value={detail.elapsed} />
          <DetailItem label="预计剩余" value={isRunning ? detail.eta : '--'} />
        </div>
      )}

      {/* 阶段消息 */}
      {status.message && (
        <div
          style={{
            marginTop: 12,
            padding: '8px 12px',
            borderRadius: 4,
            background: 'var(--qvm-color-bg-1, #f5f7fa)',
            fontSize: 13,
            color: 'var(--qvm-text-2, #666)',
          }}
        >
          {status.message}
        </div>
      )}
    </Card>
  )
}

function DetailItem({
  label,
  value,
  highlight,
}: {
  label: string
  value: string
  highlight?: boolean
}) {
  return (
    <div>
      <div style={{ fontSize: 12, color: 'var(--qvm-text-3, #999)', marginBottom: 2 }}>
        {label}
      </div>
      <div
        style={{
          fontSize: 14,
          fontWeight: highlight ? 600 : 400,
          color: highlight ? 'var(--qvm-color-primary, #4080ff)' : 'var(--qvm-text-0, #333)',
        }}
      >
        {value}
      </div>
    </div>
  )
}
