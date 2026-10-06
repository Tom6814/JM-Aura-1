// 首次切换到哔咔源时的绑定引导：已登录站点账号、且尚未绑定哔咔时弹一次。
// 游客不弹（绑定哔咔必须先有站点账号，弹了也只能报错）。
import { useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { sourceFromPath } from '../source'
import { BikaBindDialog } from './BikaBindDialog'

const DISMISS_KEY = 'jm.bika.bind.prompt.v1'

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {}
}

export function BikaBindPrompt() {
  const location = useLocation()
  const { user } = useAuth()
  const [open, setOpen] = useState(false)
  const shownRef = useRef(false)
  const source = sourceFromPath(location.pathname)

  useEffect(() => {
    if (source !== 'bika') {
      shownRef.current = false // 离开哔咔后重新武装，下次再进来可再提示
      return
    }
    if (shownRef.current || !user) return // 游客不弹
    try {
      if (localStorage.getItem(DISMISS_KEY) === '1') return
    } catch {
      /* localStorage 不可用则按未提示处理 */
    }
    shownRef.current = true
    let alive = true
    void api
      .get('/api/v2/bika/capabilities')
      .then((d) => {
        if (alive && !asRecord(d).logged_in) setOpen(true)
      })
      .catch(() => {
        /* 探测失败不打扰用户 */
      })
    return () => {
      alive = false
    }
  }, [source, user])

  const close = (dontAsk: boolean) => {
    if (dontAsk) {
      try {
        localStorage.setItem(DISMISS_KEY, '1')
      } catch {
        /* ignore */
      }
    }
    setOpen(false)
  }

  return <BikaBindDialog open={open} onClose={close} />
}
