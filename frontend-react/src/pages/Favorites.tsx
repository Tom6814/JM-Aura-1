// 收藏夹：漫画走 v2 /api/v2/{源}/favorites（统一形态，JM/哔咔同形），小说走 v2 /api/v2/jm/novel_favorites。
import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import Pagination from '@mui/material/Pagination'
import Stack from '@mui/material/Stack'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import FavoriteIcon from '@mui/icons-material/Favorite'
import FolderIcon from '@mui/icons-material/Folder'
import MenuBookIcon from '@mui/icons-material/MenuBook'
import { api } from '../api'
import { sourceFromPath } from '../source'
import type { ComicSummary, NovelSummary } from '../types'
import { CenterLoading, ComicGrid, EmptyState, ErrorState, NovelGrid, SectionTitle, useAsync } from '../components'
import { useAuth } from '../auth'

interface JmFavorites {
  comics: ComicSummary[]
  folders: { id: string; name: string }[]
  pages: number
}

interface JmNovelFavorites {
  list: NovelSummary[]
  folders: { id: string; name: string }[]
  pages: number
}

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {}
}

function asFolders(v: unknown): { id: string; name: string }[] {
  const list = Array.isArray(v) ? v : []
  return list.map((f) => {
    const r = asRecord(f)
    const id = String(r.id ?? r.fid ?? r.FID ?? r.folder_id ?? '')
    const name = String(r.name ?? '未命名')
    return { id, name }
  })
}

function asComicSummaries(v: unknown): ComicSummary[] {
  const list = Array.isArray(v) ? v : []
  return list
    .map((c) => {
      const r = asRecord(c)
      return {
        source: String(r.source ?? 'jm'),
        comic_id: String(r.comic_id ?? ''),
        title: String(r.title ?? ''),
        author: r.author == null ? null : String(r.author),
        cover_url: r.cover_url == null ? null : String(r.cover_url),
        tags: Array.isArray(r.tags) ? (r.tags as string[]) : [],
        category: r.category == null ? null : String(r.category),
      }
    })
    .filter((c) => c.comic_id !== '')
}

function asNovelSummaries(v: unknown): NovelSummary[] {
  const list = Array.isArray(v) ? v : []
  return list
    .map((n) => {
      const r = asRecord(n)
      return {
        source: String(r.source ?? 'jm'),
        novel_id: String(r.novel_id ?? ''),
        title: String(r.title ?? ''),
        author: r.author == null ? null : String(r.author),
        cover_url: r.cover_url == null ? null : String(r.cover_url),
        category: r.category == null ? null : String(r.category),
      }
    })
    .filter((n) => n.novel_id !== '')
}

export default function Favorites() {
  const { user } = useAuth()
  const [tab, setTab] = useState(0)
  // 哔咔没有小说，隐藏「小说」分栏，直接展示漫画收藏。
  const isBika = sourceFromPath() === 'bika'
  const sourceLabel = isBika ? '哔咔' : 'JM'

  if (!user) {
    return (
      <Stack spacing={2}>
        <SectionTitle>
          <FavoriteIcon sx={{ color: 'primary.main' }} /> 我的收藏
        </SectionTitle>
        <Alert severity="info" sx={{ borderRadius: 3 }}>
          云端收藏需要登录 {sourceLabel} 账号后可见。
        </Alert>
      </Stack>
    )
  }

  return (
    <Stack spacing={2}>
      <SectionTitle>
        <FavoriteIcon sx={{ color: 'primary.main' }} /> 我的收藏
      </SectionTitle>
      {isBika ? (
        <ComicFavoritesPane />
      ) : (
        <>
          <Tabs value={tab} onChange={(_e, v: number) => setTab(v)} variant="fullWidth">
            <Tab label="漫画" />
            <Tab label="小说" />
          </Tabs>
          {tab === 0 ? <ComicFavoritesPane /> : <NovelFavoritesPane />}
        </>
      )}
    </Stack>
  )
}

function ComicFavoritesPane() {
  const [page, setPage] = useState(1)
  const [folderId, setFolderId] = useState('0')

  const favs = useAsync<JmFavorites>(async () => {
    const d = asRecord(
      await api.get(`/api/v2/jm/favorites${api.qs({ page, folder_id: folderId })}`),
    )
    return {
      comics: asComicSummaries(d.items),
      folders: asFolders(d.folders),
      pages: Number(d.pages) || 1,
    }
  }, [page, folderId])

  if (favs.loading) return <CenterLoading />
  if (favs.error) return <ErrorState message={`云端收藏加载失败：${favs.error}`} onRetry={favs.reload} />

  return (
    <Stack spacing={2}>
      <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
        <Chip
          label="全部分类"
          color={folderId === '0' ? 'primary' : 'default'}
          onClick={() => {
            setFolderId('0')
            setPage(1)
          }}
        />
        {favs.data?.folders.map((f) => (
          <Chip
            key={f.id}
            label={f.name}
            icon={<FolderIcon />}
            color={folderId === f.id ? 'primary' : 'default'}
            onClick={() => {
              setFolderId(f.id || '0')
              setPage(1)
            }}
          />
        ))}
      </Stack>
      {favs.data && favs.data.comics.length > 0 ? (
        <ComicGrid items={favs.data.comics} />
      ) : (
        <EmptyState icon={<FavoriteIcon />} text="暂无云端收藏" />
      )}
      {(favs.data?.pages ?? 1) > 1 && (
        <Box sx={{ display: 'flex', justifyContent: 'center', pb: 2 }}>
          <Pagination
            count={favs.data!.pages}
            page={page}
            onChange={(_e, v: number) => setPage(v)}
            shape="rounded"
          />
        </Box>
      )}
    </Stack>
  )
}

function NovelFavoritesPane() {
  const [page, setPage] = useState(1)
  const [folderId, setFolderId] = useState('')

  const favs = useAsync<JmNovelFavorites>(async () => {
    const d = asRecord(await api.get(`/api/v2/jm/novel_favorites${api.qs({ page, folder_id: folderId, o: 'mr' })}`))
    return {
      list: asNovelSummaries(d.list),
      folders: asFolders(d.folders),
      pages: Number(d.pages) || 1,
    }
  }, [page, folderId])

  if (favs.loading) return <CenterLoading />
  if (favs.error) return <ErrorState message={`小说收藏加载失败：${favs.error}`} onRetry={favs.reload} />

  return (
    <Stack spacing={2}>
      <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
        <Chip
          label="全部小说"
          color={folderId === '' ? 'primary' : 'default'}
          onClick={() => {
            setFolderId('')
            setPage(1)
          }}
        />
        {favs.data?.folders.map((f) => (
          <Chip
            key={f.id}
            label={f.name}
            icon={<FolderIcon />}
            color={folderId === f.id ? 'primary' : 'default'}
            onClick={() => {
              setFolderId(f.id)
              setPage(1)
            }}
          />
        ))}
      </Stack>
      {favs.data && favs.data.list.length > 0 ? (
        <NovelGrid items={favs.data.list} />
      ) : (
        <EmptyState icon={<MenuBookIcon />} text="暂无小说收藏" />
      )}
      {(favs.data?.pages ?? 1) > 1 && (
        <Box sx={{ display: 'flex', justifyContent: 'center', pb: 2 }}>
          <Pagination
            count={favs.data!.pages}
            page={page}
            onChange={(_e, v: number) => setPage(v)}
            shape="rounded"
          />
        </Box>
      )}
    </Stack>
  )
}
