import { useState } from 'react'
import type { FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import Box from '@mui/material/Box'
import IconButton from '@mui/material/IconButton'
import InputBase from '@mui/material/InputBase'
import Pagination from '@mui/material/Pagination'
import Paper from '@mui/material/Paper'
import Typography from '@mui/material/Typography'
import MenuBookIcon from '@mui/icons-material/MenuBook'
import SearchIcon from '@mui/icons-material/Search'
import { api } from '../api'
import { NovelGrid, CenterLoading, EmptyState, ErrorState, SectionTitle, useAsync } from '../components'
import type { NovelSummary } from '../types'

interface NovelListResponse {
  list: NovelSummary[]
  total: number
  redirect_aid?: string
}

export default function NovelList() {
  const [params, setParams] = useSearchParams()
  const page = Math.max(1, Number(params.get('page') ?? '1') || 1)
  const q = params.get('q') ?? ''
  const [input, setInput] = useState(q)

  const state = useAsync<NovelListResponse | null>(async () => {
    if (q.trim()) {
      return api.get<NovelListResponse>(`/api/v2/jm/novel/search${api.qs({ q, page })}`)
    }
    const d = await api.get<NovelListResponse>(`/api/v2/jm/novels${api.qs({ page })}`)
    return d
  }, [q, page])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const trimmed = input.trim()
    setParams(trimmed ? { q: trimmed, page: '1' } : {})
  }

  const items = state.data?.list ?? []
  const total = state.data?.total ?? 0
  const pageCount = total > 0 ? Math.min(Math.ceil(total / 20), 500) : 100

  return (
    <Box>
      <Typography variant="h5" fontWeight={800} mb={2} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <MenuBookIcon color="primary" /> JM 小说
      </Typography>

      <Paper
        component="form"
        onSubmit={submit}
        sx={{ p: '4px 4px 4px 16px', display: 'flex', alignItems: 'center', borderRadius: 999, mb: 3 }}
      >
        <InputBase
          sx={{ ml: 1, flex: 1 }}
          placeholder="搜索小说…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          autoFocus
        />
        <IconButton type="submit" aria-label="搜索">
          <SearchIcon />
        </IconButton>
      </Paper>

      {state.loading ? (
        <CenterLoading label={q ? `正在搜索「${q}」` : '加载小说列表…'} />
      ) : state.error ? (
        <ErrorState message={state.error} onRetry={state.reload} />
      ) : items.length === 0 ? (
        <EmptyState text={q ? `没有找到与「${q}」相关的小说` : '暂无小说内容'} icon={<MenuBookIcon />} />
      ) : (
        <>
          <SectionTitle>
            {q ? `「${q}」的搜索结果` : '小说列表'}
          </SectionTitle>
          <NovelGrid items={items} />
          <Box sx={{ display: 'flex', justifyContent: 'center', mt: 4 }}>
            <Pagination
              count={pageCount}
              page={page}
              onChange={(_, p) => {
                setParams(q ? { q, page: String(p) } : { page: String(p) })
                window.scrollTo({ top: 0 })
              }}
              color="primary"
            />
          </Box>
        </>
      )}
    </Box>
  )
}
