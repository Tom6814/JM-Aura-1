import { useEffect, useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { api, ApiError } from '../api'
import { useToast } from '../toast'
import type { SourceKind } from '../source'
import type { WorkKind } from '../types'

export const RECOMMEND_BODY_MAX = 1000

/** 阅读笔记发帖对话框：作品类型 + 作品 ID + 内容（纯文本，类似评论）。
 *  类型由用户显式选择，避免漫画 / 小说混淆；defaultComicId / defaultKind
 *  由详情页预填；提交成功后回调 onPosted 以便列表刷新。 */
export function RecommendDialog({
  open,
  onClose,
  defaultComicId = '',
  defaultKind = 'comic',
  source = 'jm',
  onPosted,
}: {
  open: boolean
  onClose: () => void
  defaultComicId?: string
  defaultKind?: WorkKind
  /** 作品所属源：哔咔没有小说，类型固定为漫画。 */
  source?: SourceKind
  onPosted?: () => void
}) {
  const { toast } = useToast()
  const isBika = source === 'bika'
  const [comicId, setComicId] = useState(defaultComicId)
  const [kind, setKind] = useState<WorkKind>(defaultKind)
  const [body, setBody] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (open) {
      setComicId(defaultComicId)
      setKind(isBika ? 'comic' : defaultKind)
    }
  }, [open, defaultComicId, defaultKind, isBika])

  const submit = async () => {
    const cid = comicId.trim()
    const idOk = isBika ? /^[0-9a-fA-F]{6,32}$/.test(cid) : /^\d{1,12}$/.test(cid)
    if (!idOk) {
      toast(isBika ? '请输入正确的哔咔作品 ID' : '请输入正确的作品 ID（纯数字）', 'warning')
      return
    }
    if (!body.trim()) {
      toast('请填写笔记内容', 'warning')
      return
    }
    setSaving(true)
    try {
      await api.post('/api/recommend', { comic_id: cid, kind, source, body: body.trim() })
      toast('发布成功')
      setBody('')
      onClose()
      onPosted?.()
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '发布失败', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onClose={saving ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle sx={{ fontWeight: 700 }}>发布笔记</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          {!isBika && (
            <Box>
              <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
                作品类型
              </Typography>
              <ToggleButtonGroup
                exclusive
                fullWidth
                size="small"
                value={kind}
                disabled={saving}
                onChange={(_, v: WorkKind | null) => {
                  if (v) setKind(v)
                }}
              >
                <ToggleButton value="comic">漫画</ToggleButton>
                <ToggleButton value="novel">小说</ToggleButton>
              </ToggleButtonGroup>
            </Box>
          )}
          <TextField
            label="作品 ID"
            value={comicId}
            onChange={(e) => setComicId(e.target.value)}
            placeholder="例如 422866"
            size="small"
            fullWidth
            disabled={saving}
            helperText={isBika ? '填入哔咔作品 ID（24 位十六进制）' : '填入 JM 链接中的数字 ID'}
          />
          <TextField
            label="笔记内容"
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="写下你的阅读笔记…"
            multiline
            minRows={4}
            maxRows={10}
            fullWidth
            disabled={saving}
            slotProps={{ htmlInput: { maxLength: RECOMMEND_BODY_MAX } }}
            helperText={`${body.length}/${RECOMMEND_BODY_MAX}`}
          />
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} disabled={saving}>
          取消
        </Button>
        <Button variant="contained" onClick={() => void submit()} disabled={saving}>
          {saving ? '发布中…' : '发布'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
