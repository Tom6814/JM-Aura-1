import { lazy, Suspense, useEffect, useState } from 'react'
import { api } from '../api'

// 首访公告（薄壳）。
// 职责仅有三件事：拉 /api/announcement、按「公告内容哈希」门控、按需加载弹窗。
// 真正重的 Markdown/白名单 HTML 渲染逻辑拆到 AnnouncementDialog.tsx，
// 由 React.lazy 触发动态 import（独立 chunk），不计入主包首屏体积。
// 这里刻意不 import 任何 MUI 组件：无可展示内容时本组件零成本（返回 null）。

const AnnouncementDialog = lazy(() => import('./AnnouncementDialog'))

// 存「已读公告内容的哈希」：公告更新（哈希变化）时才再弹一次。
const DISMISS_KEY = 'jm_aura_announcement_dismissed'

interface AnnouncementResponse {
  announcement?: string
  hash?: string
}

export default function Announcement() {
  const [text, setText] = useState('')
  const [hash, setHash] = useState('')

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        const res = await api.get<AnnouncementResponse>('/api/announcement')
        if (!alive) return
        const body = (res?.announcement ?? '').trim()
        const h = res?.hash ?? ''
        // 无公告，或内容与上次已读的一致：不再弹出。
        if (!body || h === '') return
        if (localStorage.getItem(DISMISS_KEY) === h) return
        setText(body)
        setHash(h)
      } catch {
        /* localStorage 可能被禁用 / 公告请求失败：静默忽略 */
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  if (!text) return null

  const dismiss = () => {
    try {
      localStorage.setItem(DISMISS_KEY, hash)
    } catch {
      /* ignore */
    }
    setText('')
  }

  return (
    <Suspense fallback={null}>
      <AnnouncementDialog content={text} onDismiss={dismiss} />
    </Suspense>
  )
}
