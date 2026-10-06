// 随机漫游：一次呈现 10 本。
//
// 挑选、去重等重活都在后端完成，前端只发一次 GET 并把结果铺成网格——
// 与「每日精选」一致，页面本身只有一次请求的开销。
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import CasinoIcon from '@mui/icons-material/Casino'
import RefreshIcon from '@mui/icons-material/Refresh'
import { api } from '../api'
import { ComicGrid, CenterLoading, EmptyState, ErrorState, useAsync } from '../components'
import type { ComicSummary } from '../types'

export default function RandomPage() {
  const list = useAsync(() => api.get<ComicSummary[]>('/api/v2/jm/random'), [])

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 1, mb: 2 }}>
        <Box>
          <Typography
            variant="h5"
            fontWeight={800}
            sx={{ display: 'flex', alignItems: 'center', gap: 1 }}
          >
            <CasinoIcon color="primary" /> 随机漫游
          </Typography>
          <Typography variant="caption" color="text.secondary">
            每次随机 10 本，换一批看看
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<RefreshIcon />}
          onClick={list.reload}
          disabled={list.loading}
          sx={{ flexShrink: 0 }}
        >
          换一批
        </Button>
      </Box>

      {list.loading ? (
        <CenterLoading label="正在掷骰子…" />
      ) : list.error ? (
        <ErrorState message={list.error} onRetry={list.reload} />
      ) : !list.data || list.data.length === 0 ? (
        <EmptyState text="没有抽到内容，再试一次吧" icon={<CasinoIcon />} />
      ) : (
        <ComicGrid items={list.data} />
      )}
    </Box>
  )
}
