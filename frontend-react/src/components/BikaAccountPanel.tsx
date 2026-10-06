// 哔咔账号面板：设置页的「哔咔账号」卡片与「首次切到哔咔」弹窗共用同一份实现。
//
// 一键注册走**后端后台任务**：POST 立即返回 task_id，真正的建号/登录/绑定在服务端跑，
// 前端只按固定间隔轻量轮询进度。这样前端不承担重活，用户关弹窗/刷新也不会中断注册。
import { useEffect, useRef, useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import LinearProgress from '@mui/material/LinearProgress'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import CloudIcon from '@mui/icons-material/Cloud'
import { api } from '../api'
import { CenterLoading, SectionTitle, useAsync } from '../components'
import { useToast } from '../toast'

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {}
}

interface BikaRegisterTask {
  task_id?: string
  state?: string
  step?: string
  attempt?: number
  max_attempts?: number
  account?: string
  name?: string
  error?: string
}

const POLL_MS = 1200

export function BikaAccountPanel({ onBound }: { onBound?: () => void }) {
  const { toast } = useToast()
  const [tick, setTick] = useState(0)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [task, setTask] = useState<BikaRegisterTask | null>(null)

  const aliveRef = useRef(true)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const info = useAsync(async () => {
    const caps = asRecord(await api.get('/api/v2/bika/capabilities'))
    const loggedIn = Boolean(caps.logged_in)
    const capMap = asRecord(caps.caps)
    let nickname = ''
    if (loggedIn) {
      try {
        const p = asRecord(await api.get('/api/v2/bika/user/profile'))
        nickname = String(p.nickname ?? p.username ?? '')
      } catch {
        /* 资料拉取失败不影响登录态展示 */
      }
    }
    return {
      loggedIn,
      publicAccount: Boolean(caps.public_account),
      canRegister: Boolean(capMap.register),
      nickname,
    }
  }, [tick])

  // 卸载即停轮询：不在后台留定时器（前端低占用）。
  useEffect(() => {
    aliveRef.current = true
    return () => {
      aliveRef.current = false
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [])

  const stopPolling = () => {
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = null
  }

  const poll = (id: string) => {
    void api
      .get<BikaRegisterTask>(`/api/v2/bika/auth/register/${encodeURIComponent(id)}`)
      .then((t) => {
        if (!aliveRef.current) return
        setTask(t)
        if (t.state === 'running') {
          timerRef.current = setTimeout(() => poll(id), POLL_MS)
          return
        }
        stopPolling()
        if (t.state === 'done') {
          toast(t.account ? `已注册并绑定：${t.account}` : '已注册并绑定哔咔账号')
          setTask(null)
          setTick((v) => v + 1)
          onBound?.()
          return
        }
        toast(String(t.error || '哔咔注册失败'), 'error')
        setTask(null)
      })
      .catch((e) => {
        if (!aliveRef.current) return
        stopPolling()
        setTask(null)
        toast(e instanceof Error ? e.message : '查询注册进度失败', 'error')
      })
  }

  // 一键注册：后端后台任务；重复点击会复用同一个任务（幂等，不会重复建号）。
  const register = async () => {
    if (!window.confirm('将在哔咔为你创建一个新账号并绑定到当前站点账号，确定继续？')) return
    setBusy(true)
    try {
      const t = await api.post<BikaRegisterTask>('/api/v2/bika/auth/register', {})
      setTask(t)
      if (t.task_id && t.state === 'running') {
        poll(t.task_id)
      } else if (t.state === 'done') {
        setTask(null)
        setTick((v) => v + 1)
        toast(t.account ? `已注册并绑定：${t.account}` : '已注册并绑定哔咔账号')
        onBound?.()
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const login = async () => {
    if (!email.trim() || !password) {
      toast('请输入哔咔邮箱与密码')
      return
    }
    setBusy(true)
    try {
      await api.post('/api/v2/bika/auth/login', { username: email.trim(), password })
      toast('哔咔账号已登录')
      setPassword('')
      setTick((t) => t + 1)
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const logout = async () => {
    if (!window.confirm('确认退出当前哔咔账号？')) return
    stopPolling()
    setTask(null)
    setBusy(true)
    try {
      await api.post('/api/v2/bika/auth/logout', {})
      toast('已退出哔咔账号')
      setTick((t) => t + 1)
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const running = task?.state === 'running'
  const attempt = task?.attempt ?? 0
  const maxAttempts = task?.max_attempts ?? 0

  return (
    <>
      <SectionTitle
        action={
          <Chip
            size="small"
            color={info.data?.loggedIn ? 'success' : 'default'}
            label={info.data?.loggedIn ? '已登录' : '未登录'}
          />
        }
      >
        <CloudIcon /> 哔咔账号
      </SectionTitle>
      {info.loading ? (
        <CenterLoading label="检测中…" />
      ) : info.data?.loggedIn ? (
        <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={2}>
          <Typography variant="body2">账号：{info.data.nickname || '已绑定'}</Typography>
          <Button size="small" color="error" disabled={busy} onClick={() => void logout()}>
            退出登录
          </Button>
        </Stack>
      ) : (
        <Stack spacing={1.5}>
          <Typography variant="body2" color="text.secondary">
            登录你自己的哔咔账号后即可使用收藏、评论、签到。未登录时仅能浏览
            {info.data?.publicAccount ? '（当前由本站提供的公共账号供浏览）' : ''}。
          </Typography>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5}>
            <TextField
              size="small"
              label="哔咔邮箱 / 账号"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="username"
              fullWidth
            />
            <TextField
              size="small"
              type="password"
              label="密码"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              fullWidth
            />
          </Stack>
          <Stack direction="row" spacing={1.5} flexWrap="wrap" useFlexGap>
            <Button variant="contained" disabled={busy || running} onClick={() => void login()}>
              登录哔咔
            </Button>
            {info.data?.canRegister && (
              <Button variant="outlined" disabled={busy || running} onClick={() => void register()}>
                {running ? '注册中…' : '一键注册并绑定'}
              </Button>
            )}
          </Stack>
          {running && (
            <Box>
              <LinearProgress sx={{ borderRadius: 999 }} />
              <Typography variant="caption" color="text.secondary" display="block" mt={0.75}>
                {task?.step || '处理中…'}
                {attempt > 1 && maxAttempts > 0 ? `（第 ${attempt}/${maxAttempts} 次尝试）` : ''}
              </Typography>
            </Box>
          )}
          {info.data?.canRegister && !running && (
            <Typography variant="caption" color="text.secondary">
              没有哔咔账号？可一键注册一个新账号并绑定；注册在本站后台完成，账号密码由本站保管，之后登录 JM 账号时会自动登录哔咔。
            </Typography>
          )}
        </Stack>
      )}
    </>
  )
}
