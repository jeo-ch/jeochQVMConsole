/**
 * 面板/虚拟化栈健康状态灯（M8.10 / §14 P2-10）
 * - 绿色：面板在线 + libvirt 可用
 * - 黄色：面板在线 + libvirt 不可用（虚拟化栈异常，先行告警）
 * - 红色：面板离线（轮询失败/超时）
 * - 未知：首次探测完成前
 * 每 30s 轮询 /api/system/health/latest。
 */
import { useEffect, useRef, useState } from 'react'
import { getHealthProbeLatest, type HealthLightStatus } from '@/api/health'

const POLL_INTERVAL = 30_000

export default function HealthLight() {
  const [status, setStatus] = useState<HealthLightStatus>('unknown')
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    let timer = 0

    const poll = async () => {
      try {
        const res = await getHealthProbeLatest()
        if (!mountedRef.current) return
        if (res?.data?.libvirt_ready) {
          setStatus('green')
        } else {
          setStatus('yellow')
        }
      } catch {
        // 接口不可达/超时 → 面板离线
        if (!mountedRef.current) return
        setStatus('red')
      }
    }

    void poll()
    timer = window.setInterval(poll, POLL_INTERVAL)
    window.addEventListener('focus', () => void poll())

    return () => {
      mountedRef.current = false
      window.clearInterval(timer)
      window.removeEventListener('focus', () => void poll())
    }
  }, [])

  const label: Record<HealthLightStatus, string> = {
    green: '面板与虚拟化栈运行正常',
    yellow: '面板在线，但 libvirt 不可用',
    red: '面板离线或无法连接',
    unknown: '健康状态检测中',
  }

  return (
    <span className="qvm-health-light" title={label[status]}>
      <i className={`qvm-health-light-dot ${status}`} />
    </span>
  )
}
