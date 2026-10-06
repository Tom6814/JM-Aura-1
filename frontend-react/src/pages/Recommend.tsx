import { useEffect, useState } from 'react'
import { Link as RouterLink, useNavigate, useSearchParams } from 'react-router-dom'
import Avatar from '@mui/material/Avatar'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardActionArea from '@mui/material/CardActionArea'
import Chip from '@mui/material/Chip'
import IconButton from '@mui/material/IconButton'
import Pagination from '@mui/material/Pagination'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import AddIcon from '@mui/icons-material/Add'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline'
import ThumbUpAltOutlinedIcon from '@mui/icons-material/ThumbUpAltOutlined'
import { api, imgUrl } from '../api'
import { useAuth } from '../auth'
import { CenterLoading, EmptyState, ErrorState, useAsync } from '../components'
import { RecommendDialog } from '../components/RecommendDialog'
import { useToast } from '../toast'
import { sourcePath, type SourceKind } from '../source'
import type { Recommendation, RecommendationList, WorkKind } from '../types'

const PAGE_SIZE = 10

function formatTime(ms: number): string {
  return new Date(ms).toLocaleString()
}

/** 作品类型文字标签：漫画 / 小说。 */
function KindTag({ kind }: { kind: WorkKind }) {
  const isNovel = kind === 'novel'
  return (
    <Chip
      label={isNovel ? '小说' : '漫画'}
      size="small"
      variant="outlined"
      color={isNovel ? 'secondary' : 'default'}
      sx={{ height: 18, fontSize: 11, flex: '0 0 auto', '& .MuiChip-label': { px: 0.75, py: 0 } }}
    />
  )
}

function workTo(source: SourceKind, kind: WorkKind, id: string): string {
  const logical = kind === 'novel' ? `/novel/${encodeURIComponent(id)}` : `/comic/${encodeURIComponent(id)}`
  return sourcePath(source, logical)
}

function workLabel(kind: WorkKind, id: string, title: string): string {
  if (title) return title
  return kind === 'novel' ? `小说 ${id}` : `本子 ${id}`
}

function NoteItem({ post, onDelete }: { post: Recommendation; onDelete: (id: string) => void }) {
  const cover = imgUrl(post.cover_url, 320) ?? null
  const src: SourceKind = post.source === 'bika' ? 'bika' : 'jm'
  const to = workTo(src, post.kind, post.comic_id)
  const name = post.author_name || post.author
  const label = workLabel(post.kind, post.comic_id, post.comic_title)

  return (
    <Card sx={{ display: 'flex', alignItems: 'stretch' }}>
      <CardActionArea
        component={RouterLink}
        to={to}
        sx={{
          p: 1.5,
          flex: 1,
          minWidth: 0,
          display: 'flex',
          gap: 1.5,
          alignItems: 'flex-start',
          color: 'inherit',
          textDecoration: 'none',
        }}
      >
        <Box
          sx={{
            position: 'relative',
            flex: '0 0 auto',
            width: 64,
            height: 88,
            borderRadius: 1.5,
            overflow: 'hidden',
            bgcolor: 'action.hover',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 24,
            opacity: 0.95,
          }}
        >
          {cover ? (
            <Box
              component="img"
              src={cover}
              alt={label}
              loading="lazy"
              decoding="async"
              sx={{ width: '100%', height: '100%', objectFit: 'cover' }}
            />
          ) : (
            '📖'
          )}
        </Box>

        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Stack direction="row" spacing={0.75} alignItems="center" sx={{ minWidth: 0 }}>
            <Avatar sx={{ width: 20, height: 20, fontSize: 11 }}>{name.slice(0, 1).toUpperCase()}</Avatar>
            <Typography variant="subtitle2" fontWeight={700} noWrap sx={{ minWidth: 0 }}>
              {name}
            </Typography>
            <Typography variant="caption" color="text.disabled" noWrap>
              · {formatTime(post.created_at)}
            </Typography>
          </Stack>

          {post.body && (
            <Typography
              variant="body2"
              sx={{
                mt: 0.5,
                display: '-webkit-box',
                WebkitLineClamp: 4,
                WebkitBoxOrient: 'vertical',
                overflow: 'hidden',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                color: 'text.primary',
              }}
            >
              {post.body}
            </Typography>
          )}

          <Box sx={{ mt: 0.75, display: 'flex', alignItems: 'center', gap: 0.5, minWidth: 0 }}>
            <KindTag kind={post.kind} />
            <Typography
              variant="caption"
              title={label}
              sx={{
                minWidth: 0,
                color: 'primary.main',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {label}
            </Typography>
          </Box>
        </Box>
      </CardActionArea>

      {post.can_delete && (
        <Box sx={{ display: 'flex', alignItems: 'flex-start', pt: 1.5, pr: 0.5 }}>
          <Tooltip title="删除">
            <IconButton size="small" onClick={() => onDelete(post.id)}>
              <DeleteOutlineIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        </Box>
      )}
    </Card>
  )
}

export default function Recommend() {
  const [params, setParams] = useSearchParams()
  const page = Math.max(1, Number(params.get('page') ?? '1') || 1)
  const navigate = useNavigate()
  const { user } = useAuth()
  const { toast } = useToast()
  const [dialogOpen, setDialogOpen] = useState(false)

  const state = useAsync(async () => api.get<RecommendationList>(`/api/recommend${api.qs({ page })}`), [page])

  useEffect(() => {
    window.scrollTo({ top: 0 })
  }, [page])

  const data = state.data
  const items = data?.items ?? []
  const totalPages = data ? Math.max(1, Math.ceil(data.total / (data.page_size || PAGE_SIZE))) : 1

  const openPost = () => {
    if (!user) {
      navigate('/login')
      return
    }
    setDialogOpen(true)
  }

  const remove = async (id: string) => {
    if (!window.confirm('确定删除这条笔记吗？')) return
    try {
      await api.del(`/api/recommend/${encodeURIComponent(id)}`)
      toast('已删除')
      state.reload()
    } catch (e) {
      toast(e instanceof Error ? e.message : '删除失败', 'error')
    }
  }

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" mb={1.5} flexWrap="wrap" useFlexGap>
        <Typography variant="h5" fontWeight={800} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <ThumbUpAltOutlinedIcon color="primary" /> 阅读笔记
        </Typography>
        <Button variant="contained" startIcon={<AddIcon />} onClick={openPost} sx={{ borderRadius: 3, fontWeight: 700 }}>
          写笔记
        </Button>
      </Stack>

      {state.loading ? (
        <CenterLoading />
      ) : state.error ? (
        <ErrorState message={state.error} onRetry={state.reload} />
      ) : items.length === 0 ? (
        <EmptyState
          text="还没有人写笔记，来发第一条吧"
          icon={<ThumbUpAltOutlinedIcon />}
          action={
            <Button variant="contained" startIcon={<AddIcon />} onClick={openPost} sx={{ borderRadius: 3 }}>
              写笔记
            </Button>
          }
        />
      ) : (
        <>
          <Stack spacing={1.25}>
            {items.map((p) => (
              <NoteItem key={p.id} post={p} onDelete={(id) => void remove(id)} />
            ))}
          </Stack>
          {totalPages > 1 && (
            <Box sx={{ display: 'flex', justifyContent: 'center', mt: 4 }}>
              <Pagination
                count={totalPages}
                page={page}
                onChange={(_, p) => setParams(p > 1 ? { page: String(p) } : {})}
                color="primary"
              />
            </Box>
          )}
        </>
      )}

      <RecommendDialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        onPosted={() => {
          setParams({})
          state.reload()
        }}
      />
    </Box>
  )
}
