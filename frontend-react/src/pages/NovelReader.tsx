import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import CircularProgress from '@mui/material/CircularProgress'
import Drawer from '@mui/material/Drawer'
import IconButton from '@mui/material/IconButton'
import Slider from '@mui/material/Slider'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import MenuBookIcon from '@mui/icons-material/MenuBook'
import NavigateBeforeIcon from '@mui/icons-material/NavigateBefore'
import NavigateNextIcon from '@mui/icons-material/NavigateNext'
import SettingsIcon from '@mui/icons-material/Settings'
import { api } from '../api'
import { useAsync } from '../components'
import { useAuth } from '../auth'
import type { NovelChapterDetail, NovelDetail } from '../types'

interface ReaderSettings {
  fontSize: number
  lineHeight: number
}

const DEFAULT_SETTINGS: ReaderSettings = { fontSize: 18, lineHeight: 1.8 }
const SETTINGS_KEY = 'jm.novel.reader.settings.v1'

function loadSettings(): ReaderSettings {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY)
    if (!raw) return DEFAULT_SETTINGS
    const p = JSON.parse(raw) as Partial<ReaderSettings>
    const clamp = (v: unknown, min: number, max: number, fb: number) => {
      const n = Number(v)
      return Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : fb
    }
    return {
      fontSize: clamp(p.fontSize, 14, 32, DEFAULT_SETTINGS.fontSize),
      lineHeight: clamp(p.lineHeight, 1.4, 3, DEFAULT_SETTINGS.lineHeight),
    }
  } catch {
    return DEFAULT_SETTINGS
  }
}

export default function NovelReader() {
  const { chapterId = '' } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()

  const nid = params.get('nid') ?? ''
  // 语言持久化：优先读 URL 的 lang（来自阅读历史），默认繁体。
  const [lang, setLang] = useState<'tw' | 'cn'>(() => (params.get('lang') === 'cn' ? 'cn' : 'tw'))
  const [settings, setSettings] = useState<ReaderSettings>(loadSettings)
  const [sheet, setSheet] = useState<null | 'settings'>(null)
  const [controls, setControls] = useState(true)

  const { user } = useAuth()
  const scrollRef = useRef<HTMLDivElement>(null)
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastScrollPct = useRef(0)
  const resumedFor = useRef<string | null>(null)
  // 语言切换时记录当前进度，等新内容渲染后再恢复，避免跳回顶部。
  const restorePctRef = useRef<number | null>(null)
  // 每章只写一次「初始历史」；语言切换不重复写（否则会把进度清零）。
  const postedInitFor = useRef<string | null>(null)
  const latestLang = useRef(lang)
  useEffect(() => {
    latestLang.current = lang
  }, [lang])

  const initialScroll = useMemo(() => {
    const s = params.get('scroll')
    if (!s) return null
    const n = Number(s)
    return Number.isFinite(n) && n > 0 && n <= 1 ? n : null
  }, [params])

  const chapter = useAsync(
    () => api.get<NovelChapterDetail>(`/api/v2/jm/novel_chapter/${encodeURIComponent(chapterId)}?lang=${lang}`),
    [chapterId, lang],
  )

  const novel = useAsync(async () => {
    if (!nid) return null
    try {
      return await api.get<NovelDetail>(`/api/v2/jm/novel/${encodeURIComponent(nid)}`)
    } catch {
      return null
    }
  }, [nid])

  const chapters = novel.data?.chapters ?? []
  const chIdx = useMemo(() => chapters.findIndex((c) => c.id === chapterId), [chapters, chapterId])
  const prevCh = chIdx > 0 ? chapters[chIdx - 1] : null
  const nextCh = chIdx >= 0 && chIdx < chapters.length - 1 ? chapters[chIdx + 1] : null

  useEffect(() => {
    const save = JSON.stringify(settings)
    localStorage.setItem(SETTINGS_KEY, save)
  }, [settings])

  // 仅切章时回到顶部；切换语言不重置滚动（由 restorePctRef 恢复原位置）。
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: 0 })
    setControls(true)
  }, [chapterId])

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const max = el.scrollHeight - el.clientHeight
    const pct = max > 0 ? el.scrollTop / max : 0
    lastScrollPct.current = pct
    if (saveTimer.current) clearTimeout(saveTimer.current)
    saveTimer.current = setTimeout(() => {
      saveTimer.current = null
      if (user && nid && chapterId) {
        void api
          .post('/api/aura/library/history', {
            album_id: nid,
            photo_id: chapterId,
            type: 'novel',
            lang,
            scroll_pct: Math.round(pct * 10000) / 10000,
          })
          .catch(() => {})
      }
    }, 500)
  }, [user, nid, chapterId, lang])

  // 章节数据到达：写一次初始历史（每章一次；语言切换不重复写，避免进度被清零）
  useEffect(() => {
    if (!chapter.data) return
    if (postedInitFor.current === chapterId) return
    postedInitFor.current = chapterId
    if (user && nid && chapterId) {
      void api
        .post('/api/aura/library/history', {
          album_id: nid,
          album_title: novel.data?.title ?? '',
          photo_id: chapterId,
          title: chapter.data?.title ?? '',
          type: 'novel',
          lang,
          scroll_pct: initialScroll ?? 0,
          timestamp: Date.now(),
        })
        .catch(() => {})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chapter.data, chapterId, lang])

  // 小说详情到达：补全 album_title
  useEffect(() => {
    if (!novel.data || !user || !nid) return
    void api
      .post('/api/aura/library/history', {
        album_id: nid,
        album_title: novel.data.title,
        type: 'novel',
      })
      .catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [novel.data])

  // 续读定位：章节内容渲染后滚动到保存位置
  useEffect(() => {
    if (!chapter.data || initialScroll == null) return
    if (resumedFor.current === chapterId) return
    const target = initialScroll
    let tries = 0
    let raf = 0
    const tick = () => {
      const el = scrollRef.current
      if (!el) return
      const max = el.scrollHeight - el.clientHeight
      if (max > 10) {
        el.scrollTo({ top: max * target })
        return
      }
      if (++tries < 30) raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    resumedFor.current = chapterId
    return () => cancelAnimationFrame(raf)
  }, [chapter.data, initialScroll, chapterId])

  // 语言切换后：新内容渲染完成，恢复到切换前的阅读位置
  useEffect(() => {
    if (!chapter.data) return
    const pct = restorePctRef.current
    if (pct == null) return
    restorePctRef.current = null
    let tries = 0
    let raf = 0
    const tick = () => {
      const el = scrollRef.current
      if (!el) return
      const max = el.scrollHeight - el.clientHeight
      if (max > 10) {
        el.scrollTo({ top: max * pct })
        return
      }
      if (++tries < 40) raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [chapter.data])

  // 切话/卸载时兜底上报最后进度
  useEffect(() => {
    return () => {
      if (saveTimer.current) clearTimeout(saveTimer.current)
      if (user && nid && chapterId) {
        void api
          .post('/api/aura/library/history', {
            album_id: nid,
            photo_id: chapterId,
            type: 'novel',
            lang: latestLang.current,
            scroll_pct: Math.round(lastScrollPct.current * 10000) / 10000,
          })
          .catch(() => {})
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chapterId])

  // 切换繁/简：记录当前位置以便切后恢复，并把语言写入历史 + URL。
  const switchLang = (l: 'tw' | 'cn') => {
    if (l === lang) return
    restorePctRef.current = lastScrollPct.current
    setLang(l)
    const sp = new URLSearchParams(params)
    sp.set('lang', l)
    navigate(`/novel_reader/${encodeURIComponent(chapterId)}?${sp.toString()}`, { replace: true })
    if (user && nid && chapterId) {
      void api
        .post('/api/aura/library/history', {
          album_id: nid,
          photo_id: chapterId,
          type: 'novel',
          lang: l,
          scroll_pct: Math.round(lastScrollPct.current * 10000) / 10000,
        })
        .catch(() => {})
    }
  }

  // 章内切章用 replace：阅读页始终只占用一个历史位。
  // 否则每切一次章都会压深一层，「详情 → 阅读 → 下一章」后点返回
  // 会先退回上一章，而不是直接回到详情页。
  const goChapter = (id: string) => {
    const qs = nid ? `?nid=${encodeURIComponent(nid)}` : ''
    navigate(`/novel_reader/${encodeURIComponent(id)}${qs}`, { replace: true })
  }

  // 退出阅读：优先「退栈」回上一页。
  // 从详情页进入时即回到原来那条详情页记录（不新增历史），
  // 避免再 push 一条详情页造成「详情 → 阅读 → 详情」的返回死循环；
  // 直接打开阅读页（无站内历史可退）时，用 replace 兜底跳详情，同样不新增记录。
  const exitReader = () => {
    if ((window.history.state?.idx ?? 0) > 0) {
      navigate(-1)
      return
    }
    navigate(nid ? `/novel/${encodeURIComponent(nid)}` : '/novels', { replace: true })
  }

  const title = chapter.data?.title || `#${chapterId}`

  return (
    <Box
      ref={scrollRef}
      onScroll={handleScroll}
      sx={{
        position: 'fixed',
        inset: 0,
        zIndex: 1200,
        bgcolor: 'background.default',
        overflowY: 'auto',
      }}
    >
      {controls && (
        <Box
          sx={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            zIndex: 1220,
            bgcolor: 'aura.surfaceContainer',
            borderBottom: '1px solid',
            borderColor: 'divider',
            px: 1,
            py: 0.5,
            display: 'flex',
            alignItems: 'center',
            gap: 0.5,
          }}
        >
          <IconButton onClick={exitReader} aria-label="返回">
            <ArrowBackIcon />
          </IconButton>
          <Typography
            sx={{
              flex: 1,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              fontSize: 15,
              fontWeight: 600,
            }}
          >
            {title}
          </Typography>
          <Button
            size="small"
            variant={lang === 'tw' ? 'contained' : 'outlined'}
            onClick={() => switchLang('tw')}
            sx={{ minWidth: 0, px: 1.5, fontSize: 13, fontWeight: 700 }}
          >
            繁
          </Button>
          <Button
            size="small"
            variant={lang === 'cn' ? 'contained' : 'outlined'}
            onClick={() => switchLang('cn')}
            sx={{ minWidth: 0, px: 1.5, fontSize: 13, fontWeight: 700 }}
          >
            简
          </Button>
          <IconButton onClick={() => setSheet('settings')} aria-label="设置">
            <SettingsIcon />
          </IconButton>
        </Box>
      )}

      <Box
        sx={{
          maxWidth: 720,
          mx: 'auto',
          px: { xs: 2, sm: 4 },
          pt: controls ? 7 : 3,
          pb: controls && (prevCh || nextCh) ? 11 : 6,
          minHeight: '100dvh',
          cursor: 'pointer',
        }}
        onClick={() => setControls((v) => !v)}
      >
        {chapter.loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 10 }}>
            <CircularProgress />
          </Box>
        ) : chapter.error ? (
          <Box sx={{ py: 6, textAlign: 'center' }} onClick={(e) => e.stopPropagation()}>
            <Typography color="error" mb={2}>
              {chapter.error || '加载章节失败'}
            </Typography>
            <Button onClick={chapter.reload} variant="outlined">
              重试
            </Button>
          </Box>
        ) : (
          <>
            <Typography variant="h5" fontWeight={700} mb={3} sx={{ textAlign: 'center' }}>
              {title}
            </Typography>
            <Box
              dangerouslySetInnerHTML={{ __html: chapter.data?.content ?? '' }}
              sx={{
                '& p': {
                  fontSize: `${settings.fontSize}px`,
                  lineHeight: settings.lineHeight,
                  mb: 2,
                  textIndent: '2em',
                  color: 'text.primary',
                  wordBreak: 'break-word',
                },
                '& br': { lineHeight: settings.lineHeight },
                '& a': { color: 'primary.main', textDecoration: 'none' },
                '& img': { maxWidth: '100%', borderRadius: 2, my: 1 },
                '& h1,h2,h3,h4': { mt: 3, mb: 1.5 },
              }}
            />
            <Stack
              direction="row"
              spacing={2}
              justifyContent="center"
              sx={{ mt: 6 }}
              onClick={(e) => e.stopPropagation()}
            >
              <Button
                startIcon={<NavigateBeforeIcon />}
                disabled={!prevCh}
                onClick={() => prevCh && goChapter(prevCh.id)}
                variant="outlined"
              >
                上一章
              </Button>
              <Button startIcon={<MenuBookIcon />} onClick={exitReader} variant="text">
                目录
              </Button>
              <Button
                endIcon={<NavigateNextIcon />}
                disabled={!nextCh}
                onClick={() => nextCh && goChapter(nextCh.id)}
                variant="outlined"
              >
                下一章
              </Button>
            </Stack>
          </>
        )}
      </Box>

      {controls && (prevCh || nextCh) && (
        <Box
          sx={{
            position: 'fixed',
            bottom: 0,
            left: 0,
            right: 0,
            zIndex: 1220,
            bgcolor: 'aura.surfaceContainer',
            borderTop: '1px solid',
            borderColor: 'divider',
            display: 'flex',
            pb: 'env(safe-area-inset-bottom)',
          }}
        >
          <Button
            fullWidth
            disabled={!prevCh}
            onClick={() => prevCh && goChapter(prevCh.id)}
            sx={{ borderRadius: 0, py: 1.5 }}
          >
            上一章
          </Button>
          <Button
            fullWidth
            disabled={!nextCh}
            onClick={() => nextCh && goChapter(nextCh.id)}
            sx={{ borderRadius: 0, py: 1.5 }}
          >
            下一章
          </Button>
        </Box>
      )}

      <Drawer
        anchor="bottom"
        open={sheet === 'settings'}
        onClose={() => setSheet(null)}
        slotProps={{
          paper: { sx: { borderRadius: '16px 16px 0 0', p: 3, zIndex: 1300 } },
        }}
        PaperProps={{ elevation: 8 }}
      >
        <Typography variant="h6" mb={2}>
          阅读设置
        </Typography>
        <Box mb={3}>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 1 }}>
            <Typography variant="body2" color="text.secondary">
              字体大小
            </Typography>
            <Typography variant="body2" fontWeight={700} color="primary.main">
              {settings.fontSize}px
            </Typography>
          </Box>
          <Slider
            min={14}
            max={32}
            step={1}
            value={settings.fontSize}
            onChange={(_, v) => setSettings((s) => ({ ...s, fontSize: Array.isArray(v) ? (v[0] ?? s.fontSize) : v }))}
          />
        </Box>
        <Box mb={3}>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 1 }}>
            <Typography variant="body2" color="text.secondary">
              行距
            </Typography>
            <Typography variant="body2" fontWeight={700} color="primary.main">
              {settings.lineHeight.toFixed(1)}
            </Typography>
          </Box>
          <Slider
            min={1.4}
            max={3}
            step={0.1}
            value={settings.lineHeight}
            onChange={(_, v) =>
              setSettings((s) => ({ ...s, lineHeight: Array.isArray(v) ? (v[0] ?? s.lineHeight) : v }))
            }
          />
        </Box>
        <Button variant="contained" fullWidth onClick={() => setSheet(null)} sx={{ borderRadius: 3 }}>
          确定
        </Button>
      </Drawer>
    </Box>
  )
}
