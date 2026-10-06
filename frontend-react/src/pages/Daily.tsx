// 哔咔「每日精选」首页。
//
// 内容完全由服务端决定：每天随机生成一次并缓存一整天，前端这里只发一次 GET，
// 不做挑选、不做轮询——把重活留在后端，页面本身只有一次请求的开销。
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome'
import { api } from '../api'
import { ComicGrid, CenterLoading, EmptyState, ErrorState, useAsync } from '../components'
import type { ComicSummary } from '../types'

export default function Daily() {
  const list = useAsync(() => api.get<ComicSummary[]>('/api/v2/jm/daily'), [])

  return (
    <Box>
      <Typography
        variant="h5"
        fontWeight={800}
        mb={0.5}
        sx={{ display: 'flex', alignItems: 'center', gap: 1 }}
      >
        <AutoAwesomeIcon color="primary" /> 每日精选
      </Typography>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 2 }}>
        每天随机换一批，明天再来看看
      </Typography>

      {list.loading ? (
        <CenterLoading />
      ) : list.error ? (
        <ErrorState message={list.error} onRetry={list.reload} />
      ) : !list.data || list.data.length === 0 ? (
        <EmptyState text="今天还没有内容" icon={<AutoAwesomeIcon />} />
      ) : (
        <ComicGrid items={list.data} />
      )}
    </Box>
  )
}
