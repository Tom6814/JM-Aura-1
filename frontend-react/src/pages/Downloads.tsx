// 下载管理：POST /api/v2/jm/download/tasks {comic_id,include_all} 创建整本下载；
// GET /api/v2/jm/download/tasks/{task_id} 轮询；DELETE 排队期取消；completed 后 download_url 提供 ZIP。
// 后端无"任务列表"端点，已创建任务登记在 localStorage，由前端负责轮询与追踪。
import { useEffect, useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import IconButton from '@mui/material/IconButton'
import InputAdornment from '@mui/material/InputAdornment'
import LinearProgress from '@mui/material/LinearProgress'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import AddTaskIcon from '@mui/icons-material/AddTask'
import CancelOutlinedIcon from '@mui/icons-material/CancelOutlined'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline'
import DownloadIcon from '@mui/icons-material/Download'
import MenuBookIcon from '@mui/icons-material/MenuBook'
import { api } from '../api'
import { EmptyState, SectionTitle } from '../components'
import type { NovelExportTask } from '../types'
import {
  COMIC_DL_LS_KEY,
  NOVEL_EXPORT_LS_KEY,
  STATUS_LABEL,
  loadEntries as loadTaskEntries,
  percentOf,
  persistEntries as persistTaskEntries,
  removeEntry as removeTaskEntry,
  statusColor,
  type TaskEntry,
} from '../downloadTasks'

interface TaskPub {
  task_id: string
  album_title: string
  status: string
  stage: string
  message: string
  total_images: number
  downloaded_images: number
  zipped_files: number
  total_zip_files: number
  percent: number
  download_url: string
  /** 仅排队中的任务有：前面还有多少个任务在等（后端按串行执行排队）。 */
  queued_ahead?: number
}

const LS_KEY = COMIC_DL_LS_KEY

function loadEntries(): TaskEntry[] {
  return loadTaskEntries(LS_KEY)
}

function persistEntries(list: TaskEntry[]) {
  persistTaskEntries(LS_KEY, list)
}

function asPub(v: unknown, fallbackTitle: string): TaskPub | null {
  const r = v && typeof v === 'object' ? (v as Record<string, unknown>) : null
  if (!r || typeof r.task_id !== 'string') return null
  return {
    task_id: r.task_id,
    album_title: String(r.album_title ?? fallbackTitle),
    status: String(r.status ?? 'unknown'),
    stage: String(r.stage ?? ''),
    message: String(r.message ?? ''),
    total_images: Number(r.total_images) || 0,
    downloaded_images: Number(r.downloaded_images) || 0,
    zipped_files: Number(r.zipped_files) || 0,
    total_zip_files: Number(r.total_zip_files) || 0,
    percent: Number(r.percent) || 0,
    download_url: String(r.download_url ?? ''),
    queued_ahead: r.queued_ahead == null ? undefined : Number(r.queued_ahead),
  }
}

/** 排队中的任务补一句「前面还有 N 个任务」，避免长时间等待看起来像卡死。 */
function stageText(t: { status: string; stage: string; message?: string; queued_ahead?: number }): string {
  const base = t.message || t.stage
  if (t.status === 'queued' && typeof t.queued_ahead === 'number' && t.queued_ahead > 0) {
    return `${base}（前面还有 ${t.queued_ahead} 个任务）`
  }
  return base
}

function asNovelPub(v: unknown, fallbackTitle: string): NovelExportTask | null {
  const r = v && typeof v === 'object' ? (v as Record<string, unknown>) : null
  if (!r || typeof r.task_id !== 'string') return null
  return {
    task_id: r.task_id,
    novel_id: String(r.novel_id ?? ''),
    novel_title: String(r.novel_title ?? fallbackTitle),
    author: r.author == null ? null : String(r.author),
    lang: String(r.lang ?? ''),
    format: String(r.format ?? ''),
    status: String(r.status ?? 'unknown'),
    stage: String(r.stage ?? ''),
    message: String(r.message ?? ''),
    total_chapters: Number(r.total_chapters) || 0,
    downloaded_chapters: Number(r.downloaded_chapters) || 0,
    failed_chapters: Number(r.failed_chapters) || 0,
    percent: Number(r.percent) || 0,
    download_url: r.download_url == null ? undefined : String(r.download_url),
    queued_ahead: r.queued_ahead == null ? undefined : Number(r.queued_ahead),
  }
}

export default function Downloads() {
  const [entries, setEntries] = useState<TaskEntry[]>(() => loadEntries())
  const [tasks, setTasks] = useState<Record<string, TaskPub>>({})
  const [input, setInput] = useState('')
  const [adding, setAdding] = useState(false)
  const [addErr, setAddErr] = useState<string | null>(null)
  const [novelEntries, setNovelEntries] = useState<TaskEntry[]>(() => loadTaskEntries(NOVEL_EXPORT_LS_KEY))
  const [novelTasks, setNovelTasks] = useState<Record<string, NovelExportTask>>({})

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const poll = async () => {
      const list = loadEntries()
      await Promise.all(
        list.map(async (en) => {
          try {
            const raw = await api.get(`/api/v2/jm/download/tasks/${encodeURIComponent(en.task_id)}`)
            const pub = asPub(raw, en.title)
            if (alive && pub) setTasks((m) => ({ ...m, [pub.task_id]: pub }))
          } catch {
            /* 单次轮询失败静默跳过，下轮重试 */
          }
        }),
      )
      if (alive) timer = window.setTimeout(() => void poll(), 2500)
    }
    void poll()
    return () => {
      alive = false
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const poll = async () => {
      const list = loadTaskEntries(NOVEL_EXPORT_LS_KEY)
      await Promise.all(
        list.map(async (en) => {
          try {
            const raw = await api.get<NovelExportTask>(
              `/api/v2/jm/novel/export/tasks/${encodeURIComponent(en.task_id)}`,
            )
            const pub = asNovelPub(raw, en.title)
            if (alive && pub) setNovelTasks((m) => ({ ...m, [pub.task_id]: pub }))
          } catch {
            /* 单次轮询失败静默跳过，下轮重试 */
          }
        }),
      )
      if (alive) timer = window.setTimeout(() => void poll(), 2500)
    }
    void poll()
    return () => {
      alive = false
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  const add = async () => {
    const comicId = input.trim()
    if (!comicId || adding) return
    setAdding(true)
    setAddErr(null)
    try {
      const raw = await api.post('/api/v2/jm/download/tasks', { comic_id: comicId, include_all: true })
      const pub = asPub(raw, `作品 ${comicId}`)
      if (!pub) throw new Error('创建任务返回格式异常')
      const next = [{ task_id: pub.task_id, title: pub.album_title }, ...loadEntries()]
      persistEntries(next)
      setEntries(next)
      setTasks((m) => ({ ...m, [pub.task_id]: pub }))
      setInput('')
    } catch (e) {
      setAddErr(e instanceof Error ? e.message : String(e))
    } finally {
      setAdding(false)
    }
  }

  const cancelTask = async (en: TaskEntry) => {
    try {
      await api.del(`/api/v2/jm/download/tasks/${encodeURIComponent(en.task_id)}`)
    } catch (e) {
      // 非排队态取消会被后端拒绝，提示但仍然允许移出列表
      window.alert(e instanceof Error ? e.message : String(e))
    }
    removeEntry(en)
  }

  const removeEntry = (en: TaskEntry) => {
    setEntries(removeTaskEntry(LS_KEY, en.task_id))
    setTasks((m) => {
      const { [en.task_id]: _drop, ...rest } = m
      return rest
    })
  }

  const removeNovelEntry = (en: TaskEntry) => {
    setNovelEntries(removeTaskEntry(NOVEL_EXPORT_LS_KEY, en.task_id))
    setNovelTasks((m) => {
      const { [en.task_id]: _drop, ...rest } = m
      return rest
    })
  }

  const cancelNovelTask = async (en: TaskEntry) => {
    try {
      await api.del(`/api/v2/jm/novel/export/tasks/${encodeURIComponent(en.task_id)}`)
    } catch (e) {
      // 非排队态取消会被后端拒绝，提示但仍然允许移出列表
      window.alert(e instanceof Error ? e.message : String(e))
    }
    removeNovelEntry(en)
  }

  return (
    <Stack spacing={2}>
      <SectionTitle>
        <DownloadIcon sx={{ color: 'primary.main' }} /> 下载管理
      </SectionTitle>

      <Box>
        <TextField
          fullWidth
          size="small"
          label="输入作品 ID，一键打包下载全话"
          placeholder="例如 447422"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') void add()
          }}
          sx={{
            '& .MuiOutlinedInput-root': {
              borderRadius: '24px',
              '& .MuiOutlinedInput-input': {
                paddingTop: '12.5px',
                paddingBottom: '12.5px',
              },
            },
            '& .MuiInputLabel-outlined:not(.MuiInputLabel-shrink)': {
              transform: 'translate(14px, 13px) scale(1)',
            },
          }}
          slotProps={{
            input: {
              endAdornment: (
                <InputAdornment position="end">
                  <Button variant="contained" size="small" disabled={adding || !input.trim()} onClick={() => void add()} startIcon={adding ? <CircularProgress size={16} color="inherit" /> : <AddTaskIcon />}>
                    创建任务
                  </Button>
                </InputAdornment>
              ),
            },
          }}
        />
        {addErr && (
          <Typography variant="caption" color="error" sx={{ mt: 1, display: 'block' }}>
            {addErr}
          </Typography>
        )}
      </Box>

      {entries.length === 0 ? (
        <EmptyState icon={<DownloadIcon />} text="暂无下载任务" />
      ) : (
        entries.map((en) => {
          const t = tasks[en.task_id]
          return (
            <Card key={en.task_id} sx={{ borderRadius: 3 }}>
              <CardContent>
                <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={1}>
                  <Typography fontWeight={600} noWrap>
                    {t?.album_title || en.title || `作品 ${en.task_id}`}
                  </Typography>
                  <Stack direction="row" alignItems="center">
                    {t?.status === 'completed' && t.download_url ? (
                      <Button size="small" variant="contained" startIcon={<DownloadIcon />} href={api.url(t.download_url)}>
                        下载 ZIP
                      </Button>
                    ) : null}
                    {t && ['queued'].includes(t.status) ? (
                      <Tooltip title="取消任务">
                        <IconButton size="small" onClick={() => void cancelTask(en)}>
                          <CancelOutlinedIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    ) : null}
                    <Tooltip title="移出列表">
                      <IconButton size="small" onClick={() => removeEntry(en)}>
                        <DeleteOutlineIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Stack>
                </Stack>

                {!t ? (
                  <Typography variant="caption" color="text.secondary">
                    正在获取任务状态…
                  </Typography>
                ) : (
                  <>
                    <Stack direction="row" spacing={1} alignItems="center" sx={{ mt: 1 }}>
                      <Chip size="small" color={statusColor(t.status)} label={STATUS_LABEL[t.status] ?? t.status} />
                      <Typography variant="caption" color="text.secondary" noWrap>
                        {stageText(t)}
                      </Typography>
                    </Stack>
                    <Box sx={{ mt: 1.5 }}>
                      <LinearProgress
                        variant={t.percent > 0 ? 'determinate' : 'indeterminate'}
                        value={percentOf(t.percent)}
                      />
                    </Box>
                    <Stack direction="row" justifyContent="space-between" sx={{ mt: 0.75 }}>
                      <Typography variant="caption" color="text.secondary">
                        图片 {t.downloaded_images}/{t.total_images}
                        {t.total_zip_files > 0 ? ` · 打包 ${t.zipped_files}/${t.total_zip_files}` : ''}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {(percentOf(t.percent)).toFixed(1)}%
                      </Typography>
                    </Stack>
                  </>
                )}
              </CardContent>
            </Card>
          )
        })
      )}

      {novelEntries.length > 0 && (
        <>
          <SectionTitle>
            <MenuBookIcon sx={{ color: 'primary.main' }} /> 小说导出
          </SectionTitle>
          {novelEntries.map((en) => {
            const t = novelTasks[en.task_id]
            return (
              <Card key={en.task_id} sx={{ borderRadius: 3 }}>
                <CardContent>
                  <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={1}>
                    <Typography fontWeight={600} noWrap>
                      {t?.novel_title || en.title || `任务 ${en.task_id}`}
                    </Typography>
                    <Stack direction="row" alignItems="center">
                      {t?.status === 'completed' && t.download_url ? (
                        <Button
                          size="small"
                          variant="contained"
                          startIcon={<DownloadIcon />}
                          href={api.url(t.download_url)}
                        >
                          下载 {t.format ? t.format.toUpperCase() : '文件'}
                        </Button>
                      ) : null}
                      {t && ['queued'].includes(t.status) ? (
                        <Tooltip title="取消任务">
                          <IconButton size="small" onClick={() => void cancelNovelTask(en)}>
                            <CancelOutlinedIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      ) : null}
                      <Tooltip title="移出列表">
                        <IconButton size="small" onClick={() => removeNovelEntry(en)}>
                          <DeleteOutlineIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </Stack>
                  </Stack>

                  {!t ? (
                    <Typography variant="caption" color="text.secondary">
                      正在获取任务状态…
                    </Typography>
                  ) : (
                    <>
                      <Stack direction="row" spacing={1} alignItems="center" sx={{ mt: 1 }}>
                        <Chip size="small" color={statusColor(t.status)} label={STATUS_LABEL[t.status] ?? t.status} />
                        <Typography variant="caption" color="text.secondary" noWrap>
                          {stageText(t)}
                        </Typography>
                      </Stack>
                      <Box sx={{ mt: 1.5 }}>
                        <LinearProgress
                          variant={t.percent > 0 ? 'determinate' : 'indeterminate'}
                          value={percentOf(t.percent)}
                        />
                      </Box>
                      <Stack direction="row" justifyContent="space-between" sx={{ mt: 0.75 }}>
                        <Typography variant="caption" color="text.secondary">
                          章节 {t.downloaded_chapters}/{t.total_chapters}
                          {t.failed_chapters > 0 ? ` · 失败 ${t.failed_chapters}` : ''}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {(percentOf(t.percent)).toFixed(1)}%
                        </Typography>
                      </Stack>
                    </>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </>
      )}
    </Stack>
  )
}
