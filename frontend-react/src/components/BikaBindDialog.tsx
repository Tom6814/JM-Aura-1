// 「绑定哔咔账号」弹窗：未绑定哔咔的用户首次切到哔咔时弹出，内容与设置页的哔咔卡片一致。
// 可随时关闭；勾选「不再提示」后永久不再自动弹出。
import { useEffect, useState } from 'react'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControlLabel from '@mui/material/FormControlLabel'
import Typography from '@mui/material/Typography'
import { BikaAccountPanel } from './BikaAccountPanel'

export function BikaBindDialog({
  open,
  onClose,
}: {
  open: boolean
  onClose: (dontAsk: boolean) => void
}) {
  const [dontAsk, setDontAsk] = useState(false)

  // 每次重新打开都复位勾选态，避免上次的选择被沿用。
  useEffect(() => {
    if (open) setDontAsk(false)
  }, [open])

  return (
    <Dialog open={open} onClose={() => onClose(dontAsk)} fullWidth maxWidth="sm">
      <DialogTitle sx={{ fontWeight: 700 }}>绑定哔咔账号</DialogTitle>
      <DialogContent>
        <BikaAccountPanel onBound={() => onClose(dontAsk)} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2, justifyContent: 'space-between' }}>
        <FormControlLabel
          control={
            <Checkbox
              size="small"
              checked={dontAsk}
              onChange={(e) => setDontAsk(e.target.checked)}
            />
          }
          label={<Typography variant="caption">不再提示</Typography>}
        />
        <Button onClick={() => onClose(dontAsk)}>关闭</Button>
      </DialogActions>
    </Dialog>
  )
}
