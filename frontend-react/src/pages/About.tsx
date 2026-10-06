// 关于：面向 C 端的极简介绍 + 爱发电捐助入口 + 最近赞助者感谢名单。
import Avatar from '@mui/material/Avatar'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import FavoriteIcon from '@mui/icons-material/Favorite'
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined'
import VolunteerActivismIcon from '@mui/icons-material/VolunteerActivism'
import { api, imgUrl } from '../api'
import { SectionTitle, useAsync } from '../components'
import { AFDIAN_URL, BRAND_GRADIENT } from '../theme'

interface Sponsor {
  name: string
  avatar: string
  all_sum_amount: string
  last_pay_time: number
  plan_name: string
}

interface SponsorData {
  configured: boolean
  sponsors: Sponsor[]
  total_count: number
}

/** 最近赞助者感谢名单：由后端 /api/afdian/sponsors 代理拉取，未配置凭证时整块隐藏 */
function SponsorWall() {
  const sponsors = useAsync<SponsorData | null>(async () => {
    try {
      return await api.get<SponsorData>('/api/afdian/sponsors')
    } catch {
      return null
    }
  }, [])

  const data = sponsors.data
  if (sponsors.loading || !data || !data.configured) return null
  const list = data.sponsors ?? []

  return (
    <>
      <Divider sx={{ my: 3 }} />

      <Stack direction="row" spacing={1.5} alignItems="flex-start" sx={{ mb: 2 }}>
        <VolunteerActivismIcon color="primary" fontSize="small" sx={{ mt: 0.4 }} />
        <Typography sx={{ lineHeight: 1.9 }}>
          最近的赞助者
          <Box component="span" sx={{ fontWeight: 700 }}>
            ，谢谢你们喵！
          </Box>
        </Typography>
      </Stack>

      {list.length === 0 ? (
        <Typography color="text.secondary" variant="body2" sx={{ lineHeight: 1.9 }}>
          还没有赞助者，等你来当第一个喵！
        </Typography>
      ) : (
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
          {list.map((s, i) => (
            <Stack
              key={`${s.name}-${s.last_pay_time}-${i}`}
              direction="row"
              spacing={0.75}
              alignItems="center"
              sx={{ pl: 0.5, pr: 1.25, py: 0.5, borderRadius: 999, bgcolor: 'action.hover' }}
            >
              <Avatar
                src={imgUrl(s.avatar, 128)}
                alt={s.name}
                sx={{ width: 26, height: 26, fontSize: 13 }}
              >
                {s.name.slice(0, 1)}
              </Avatar>
              <Typography variant="caption" fontWeight={600} noWrap sx={{ maxWidth: 110 }}>
                {s.name}
              </Typography>
            </Stack>
          ))}
        </Box>
      )}
    </>
  )
}

export default function About() {
  return (
    <Box sx={{ maxWidth: 720, mx: 'auto' }}>
      <SectionTitle>
        <InfoOutlinedIcon sx={{ color: 'primary.main' }} /> 关于
      </SectionTitle>

      <Card sx={{ borderRadius: 5 }}>
        <CardContent sx={{ p: { xs: 3, md: 4 } }}>
          <Typography variant="h4" sx={{ fontWeight: 700, letterSpacing: '-0.02em', mb: 2 }}>
            JM
            <Box
              component="span"
              sx={{
                background: BRAND_GRADIENT,
                WebkitBackgroundClip: 'text',
                backgroundClip: 'text',
                color: 'transparent',
              }}
            >
              -Aura
            </Box>
          </Typography>

          <Typography color="text.secondary" sx={{ lineHeight: 1.9 }}>
            JM-Aura 是一个 禁漫天堂JMComic 的在线阅读、下载平台，希望让你哪里都能能舒服顺畅地读完喜欢的本子。
          </Typography>
          <Typography color="text.secondary" sx={{ lineHeight: 1.9, mt: 1.5 }}>
            由一位喜欢看本子的同学在闲余时间独立开发维护，只想给同好们留一个纯净的小角落。
          </Typography>

          <Divider sx={{ my: 3 }} />

          <Stack direction="row" spacing={1.5} alignItems="flex-start" sx={{ mb: 2 }}>
            <FavoriteIcon color="error" fontSize="small" sx={{ mt: 0.4 }} />
            <Typography sx={{ lineHeight: 1.9 }}>
              也许有一天它会关闭，但
              <Box component="span" sx={{ fontWeight: 700 }}>
                永远不会变质
              </Box>
              ：我会努力保持无广告喵！
            </Typography>
          </Stack>

          <Stack direction="row" spacing={1.5} alignItems="flex-start">
            <VolunteerActivismIcon color="primary" fontSize="small" sx={{ mt: 0.4 }} />
            <Typography color="text.secondary" sx={{ lineHeight: 1.9 }}>
              但说实话，作者只是一名穷学生，服务器费用并不便宜喵（约120/月）+ 流量费用（约0.8/GB，好贵！），目前大概只能再撑 2–3 个月。如果你愿意，一点点心意就能让它多陪大家一阵子喵！
            </Typography>
          </Stack>

          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={1.5}
            alignItems="center"
            sx={{ mt: 3.5 }}
          >
            <Button
              variant="contained"
              size="large"
              startIcon={<VolunteerActivismIcon />}
              href={AFDIAN_URL}
              target="_blank"
              rel="noreferrer"
              sx={{
                borderRadius: 3,
                fontWeight: 700,
                background: BRAND_GRADIENT,
                width: { xs: '100%', sm: 'auto' },
              }}
            >
              捐助 Tori 喵！
            </Button>
            <Chip label="谢谢喵！" variant="outlined" />
          </Stack>

          <SponsorWall />
        </CardContent>
      </Card>

      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', textAlign: 'center', mt: 3 }}>
        JM-Aura
      </Typography>
    </Box>
  )
}
