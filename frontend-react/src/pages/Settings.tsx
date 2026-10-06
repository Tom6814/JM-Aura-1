// 设置页：
// - 外观模式（本地主题上下文）
// - JM 连接状态 GET /api/config（legacy 裸响应）+ 会话重登 POST /api/session/relogin
// - 已存凭证 GET/DELETE /api/credentials
// - 下载缓存清理 POST /api/v2/cache/cleanup?keep_days=N
// - 爱发电赞助绑定 GET/POST /api/afdian/binding（绑定后解锁无损下载）
import { useEffect, useState } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import Button from '@mui/material/Button'
import Box from '@mui/material/Box'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import FormControlLabel from '@mui/material/FormControlLabel'
import Stack from '@mui/material/Stack'
import Switch from '@mui/material/Switch'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import BlockIcon from '@mui/icons-material/Block'
import CardGiftcardIcon from '@mui/icons-material/CardGiftcard'
import CleaningServicesIcon from '@mui/icons-material/CleaningServices'
import CloudSyncIcon from '@mui/icons-material/CloudSync'
import PaletteIcon from '@mui/icons-material/Palette'
import PersonOutlineIcon from '@mui/icons-material/PersonOutline'
import VpnKeyIcon from '@mui/icons-material/VpnKey'
import { api } from '../api'
import { appLink } from '../source'
import { AFDIAN_URL } from '../theme'
import { CenterLoading, EmptyState, ErrorState, SectionTitle, useAsync } from '../components'
import { BikaAccountPanel } from '../components/BikaAccountPanel'
import { useAuth } from '../auth'
import { useThemeMode } from '../mode'
import { useToast } from '../toast'

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {}
}

export default function Settings() {
  const { user } = useAuth()
  if (!user)
    return (
      <EmptyState
        icon={<PersonOutlineIcon />}
        text="登录 JM 账号后可管理设置"
        action={
          <Button component={RouterLink} to={appLink('/login')} variant="contained">
            去登录
          </Button>
        }
      />
    )
  return (
    <Stack spacing={2}>
      <SectionTitle>
        <PersonOutlineIcon sx={{ color: 'primary.main' }} /> 设置
      </SectionTitle>
      <AppearanceCard />
      <MaskFilterCard />
      <JmConnectionCard />
      <BikaAccountCard />
      <AfdianCard />
      <MaintenanceCard />
    </Stack>
  )
}

// —— 屏蔽与过滤 ——
// 前端只做「读一份规则 + 写一个词」：匹配、剔除、评论过滤全在后端。规则跟随当前源
// （/api/v2/jm/... 会按 URL 前缀自动切到 jm / bika）。
// 对齐 Breeze：只有一个输入框 + 词条列表；ID/标题/作者/标签命中即全线屏蔽，
// 评论区命中即遮挡警告，同一作品命中 3 条及以上再加入黑名单（阈值在后端）。

interface MaskPresetMeta {
  key: string
  label: string
  hint: string
  count: number
}

interface MaskRules {
  words: string[]
  /** 一键预设开关（键由后端决定）。 */
  presets: Record<string, boolean>
  /** 后端下发的预设元信息（只读，仅用于渲染开关标签）。 */
  presets_meta?: MaskPresetMeta[]
}

// 屏蔽常开（后端没有总开关，详情页命中即遮挡），这里只维护词表与预设。
function emptyRules(): MaskRules {
  return { words: [], presets: {} }
}

function normalizeRules(raw: unknown): MaskRules {
  const r = asRecord(raw)
  const presets = asRecord(r.presets)
  const out: MaskRules = {
    words: Array.isArray(r.words) ? r.words.map((w) => String(w)) : [],
    presets: {},
  }
  for (const [k, v] of Object.entries(presets)) out.presets[k] = v === true
  const metas = r.presets_meta
  out.presets_meta = Array.isArray(metas)
    ? metas.map((m) => {
        const mm = asRecord(m)
        return {
          key: String(mm.key ?? ''),
          label: String(mm.label ?? ''),
          hint: String(mm.hint ?? ''),
          count: Number(mm.count) || 0,
        }
      })
    : []
  return out
}

function MaskFilterCard() {
  const { toast } = useToast()
  const loaded = useAsync(() => api.get('/api/v2/jm/filter'), [])
  const [rules, setRules] = useState<MaskRules>(emptyRules)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (loaded.data) setRules(normalizeRules(loaded.data))
  }, [loaded.data])

  // 所有操作都即时落盘（没有「保存」按钮）：PUT 整份规则，以后端回读为准。
  const persist = async (next: MaskRules) => {
    setBusy(true)
    setRules(next)
    try {
      const saved = await api.put('/api/v2/jm/filter', next)
      setRules(normalizeRules(saved))
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'error')
      loaded.reload()
    } finally {
      setBusy(false)
    }
  }

  const add = () => {
    const w = draft.trim()
    if (!w) return
    setDraft('')
    if (rules.words.some((x) => x.toLowerCase() === w.toLowerCase())) return
    void persist({ ...rules, words: [...rules.words, w] })
  }

  const remove = (w: string) => void persist({ ...rules, words: rules.words.filter((x) => x !== w) })

  const presetOn = rules.presets_meta?.filter((p) => rules.presets[p.key]).length ?? 0

  return (
    <Card sx={{ borderRadius: 3 }}>
      <CardContent>
        <SectionTitle>
          <BlockIcon /> 屏蔽与过滤
        </SectionTitle>
        {loaded.loading ? (
          <CenterLoading label="读取规则…" />
        ) : loaded.error ? (
          <ErrorState message={`规则获取失败：${loaded.error}`} onRetry={loaded.reload} />
        ) : (
          <Stack spacing={1.5}>
            {rules.presets_meta && rules.presets_meta.length > 0 && (
              <Stack spacing={1}>
                {rules.presets_meta.map((p) => (
                  <FormControlLabel
                    key={p.key}
                    sx={{ m: 0 }}
                    control={
                      <Switch
                        checked={!!rules.presets[p.key]}
                        disabled={busy}
                        onChange={(e) =>
                          void persist({ ...rules, presets: { ...rules.presets, [p.key]: e.target.checked } })
                        }
                        sx={{ transform: 'scale(1.2)', mr: 1 }}
                      />
                    }
                    label={
                      <Box>
                        <Typography variant="subtitle1" fontWeight={700} lineHeight={1.2}>
                          {p.label}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {p.hint}（{p.count} 词）
                        </Typography>
                      </Box>
                    }
                  />
                ))}
              </Stack>
            )}
            <Divider />
            <Typography variant="subtitle2">屏蔽词</Typography>
            <Typography variant="caption" color="text.secondary">
              输入任意关键词后点「确定」即全线屏蔽：ID / 标题 / 作者 / 标签 命中即隐藏；
              评论区只要命中就先遮挡并给警告，同一作品命中 3 条及以上则再加入黑名单（列表也隐藏）。
            </Typography>
            <Stack direction="row" spacing={1} alignItems="center">
              <TextField
                size="small"
                value={draft}
                placeholder="输入要屏蔽的关键词，如：牛头人"
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    add()
                  }
                }}
                disabled={busy}
                sx={{ maxWidth: 320, width: '100%' }}
              />
              <Button variant="contained" onClick={add} disabled={busy || !draft.trim()}>
                确定
              </Button>
            </Stack>
            {rules.words.length > 0 ? (
              <Stack direction="row" flexWrap="wrap" useFlexGap gap={0.5}>
                {rules.words.map((w) => (
                  <Chip key={w} size="small" label={w} disabled={busy} onDelete={() => remove(w)} />
                ))}
              </Stack>
            ) : (
              <Typography variant="caption" color="text.disabled">
                还没有屏蔽词{presetOn > 0 ? `（当前启用 ${presetOn} 个预设）` : ''}。
              </Typography>
            )}
          </Stack>
        )}
      </CardContent>
    </Card>
  )
}

// 哔咔账号卡片：复用共享面板（与「首次切到哔咔」弹窗用的是同一份实现）。
function BikaAccountCard() {
  return (
    <Card sx={{ borderRadius: 3 }}>
      <CardContent>
        <BikaAccountPanel />
      </CardContent>
    </Card>
  )
}

function AppearanceCard() {
  const { mode, toggle } = useThemeMode()
  return (
    <Card sx={{ borderRadius: 3 }}>
      <CardContent>
        <SectionTitle>
          <PaletteIcon /> 外观
        </SectionTitle>
        <FormControlLabel
          control={<Switch checked={mode === 'dark'} onChange={toggle} />}
          label={mode === 'dark' ? '深色模式' : '浅色模式'}
        />
      </CardContent>
    </Card>
  )
}

function JmConnectionCard() {
  const { toast } = useToast()
  const [tick, setTick] = useState(0)
  const [busy, setBusy] = useState(false)
  const cfg = useAsync(
    async () =>
      asRecord(await api.get('/api/config')) as { username?: string; is_logged_in?: boolean },
    [tick],
  )

  const relogin = async () => {
    setBusy(true)
    try {
      await api.post('/api/session/relogin', {})
      toast('JM 会话已刷新')
      setTick((t) => t + 1)
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card sx={{ borderRadius: 3 }}>
      <CardContent>
        <SectionTitle
          action={
            <Chip
              size="small"
              color={cfg.data?.is_logged_in ? 'success' : 'default'}
              label={cfg.data?.is_logged_in ? '已连接' : '未连接'}
            />
          }
        >
          <CloudSyncIcon /> JM 连接
        </SectionTitle>
        {cfg.loading ? (
          <CenterLoading label="检测中…" />
        ) : cfg.error ? (
          <ErrorState message={`状态获取失败：${cfg.error}`} onRetry={cfg.reload} />
        ) : (
          <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={2}>
            <Typography variant="body2">
              账号：
              {cfg.data?.username || '未登录'}
            </Typography>
            <Button size="small" variant="outlined" disabled={busy} onClick={() => void relogin()}>
              刷新会话
            </Button>
          </Stack>
        )}
        <Divider sx={{ my: 1.5 }} />
        <CredentialsRow onChanged={() => setTick((t) => t + 1)} />
      </CardContent>
    </Card>
  )
}

function CredentialsRow({ onChanged }: { onChanged: () => void }) {
  const { toast } = useToast()
  const cred = useAsync(
    async () => asRecord(await api.get('/api/credentials')) as { has_saved?: boolean; username?: string },
    [],
  )

  const clear = async () => {
    if (!window.confirm('确认清除已保存的 JM 账号密码？')) return
    try {
      await api.del('/api/credentials')
      toast('凭证已清除')
      cred.reload()
      onChanged()
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={2}>
      <Typography variant="body2">
        <VpnKeyIcon fontSize="inherit" sx={{ verticalAlign: '-0.15em', mr: 0.5 }} />
        {cred.loading
          ? '凭证读取中…'
          : cred.data?.has_saved
            ? `已保存凭据：${cred.data.username || ''}`
            : '未保存 JM 密码'}
      </Typography>
      {cred.data?.has_saved ? (
        <Button size="small" color="error" onClick={() => void clear()}>
          清除凭证
        </Button>
      ) : null}
    </Stack>
  )
}

function MaintenanceCard() {
  const { toast } = useToast()
  const [keepDays, setKeepDays] = useState('7')
  const [busy, setBusy] = useState(false)

  const cleanup = async () => {
    setBusy(true)
    try {
      const d = asRecord(
        await api.post(`/api/v2/cache/cleanup${api.qs({ keep_days: Number(keepDays) || 7 })}`, {}),
      )
      toast(`已清理 ${Number(d.removed_dirs) || 0} 个缓存目录`)
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card sx={{ borderRadius: 3 }}>
      <CardContent>
        <SectionTitle>
          <CleaningServicesIcon /> 缓存维护
        </SectionTitle>
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} alignItems={{ sm: 'center' }}>
          <TextField
            size="small"
            type="number"
            label="保留最近天数"
            value={keepDays}
            onChange={(e) => setKeepDays(e.target.value)}
            sx={{ width: 160 }}
          />
          <Button variant="contained" disabled={busy} onClick={() => void cleanup()}>
            清理下载缓存
          </Button>
        </Stack>
        <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
          仅清理超过保留期的已完成下载缓存，不影响任务记录。
        </Typography>
      </CardContent>
    </Card>
  )
}

// 爱发电赞助绑定：填入订单号 → 后端调 query-order 核验为已支付 → 绑定后解锁无损下载。
function AfdianCard() {
  const { toast } = useToast()
  const [orderNo, setOrderNo] = useState('')
  const [busy, setBusy] = useState(false)
  const info = useAsync(async () => asRecord(await api.get('/api/afdian/binding')), [])

  const donor = info.data?.donor === true
  const configured = info.data?.configured !== false

  const bind = async () => {
    const v = orderNo.trim()
    if (!v) {
      toast('请输入爱发电订单号', 'warning')
      return
    }
    setBusy(true)
    try {
      await api.post('/api/afdian/binding', { order_no: v })
      toast('绑定成功，已解锁无损下载', 'success')
      setOrderNo('')
      info.reload()
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card sx={{ borderRadius: 3 }}>
      <CardContent>
        <SectionTitle>
          <CardGiftcardIcon /> 爱发电赞助
        </SectionTitle>
        {info.loading ? (
          <CenterLoading label="读取赞助状态…" />
        ) : (
          <>
            <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1.5 }}>
              <Typography variant="body2">无损下载权限：</Typography>
              <Chip size="small" color={donor ? 'success' : 'default'} label={donor ? '已开通' : '未开通'} />
              {donor && info.data?.order_no ? (
                <Typography variant="caption" color="text.secondary">
                  订单号 {String(info.data.order_no)}
                </Typography>
              ) : null}
            </Stack>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} alignItems={{ sm: 'center' }}>
              <TextField
                size="small"
                label="爱发电订单号"
                value={orderNo}
                onChange={(e) => setOrderNo(e.target.value)}
                disabled={busy || !configured}
                sx={{ width: { xs: '100%', sm: 280 } }}
              />
              <Button variant="contained" disabled={busy || !configured} onClick={() => void bind()}>
                验证并绑定
              </Button>
              <Button component="a" href={AFDIAN_URL} target="_blank" rel="noreferrer" size="small">
                去爱发电赞助
              </Button>
            </Stack>
            <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
              {configured
                ? '赞助后在爱发电「我的」→「订单」复制订单号填入即可；一次绑定长期有效，一个订单号只能绑定一个账号。'
                : '本站尚未配置爱发电校验凭证，绑定暂不可用。'}
            </Typography>
          </>
        )}
      </CardContent>
    </Card>
  )
}
