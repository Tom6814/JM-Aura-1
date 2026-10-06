import { useMemo } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogContent from '@mui/material/DialogContent'
import Divider from '@mui/material/Divider'
import Link from '@mui/material/Link'
import Typography from '@mui/material/Typography'

// 本模块被 Announcement.tsx 动态 import，独立成 chunk，不进主包首屏。
// 公告由运营方经环境变量填写（可信内容），但仍不走 dangerouslySetInnerHTML：
// 自写极简 Markdown + 白名单 HTML（a/img/br/b/strong/i/em/code），杜绝脚本注入面。

const CODE_STYLE: CSSProperties = {
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
  fontSize: '0.9em',
  padding: '1px 5px',
  borderRadius: 5,
  background: 'rgba(127,127,127,0.16)',
}

const IMG_STYLE: CSSProperties = {
  display: 'block',
  margin: '12px auto',
  maxWidth: '100%',
  borderRadius: 8,
}

const VOID_TAGS = new Set(['img', 'br'])
const CONTAINER_TAGS = new Set(['a', 'b', 'strong', 'i', 'em', 'code'])

const TAG_RE =
  /^<(\/?)([a-zA-Z][a-zA-Z0-9]*)((?:\s+[a-zA-Z_:][a-zA-Z0-9_:.-]*(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'>]+))?)*)\s*(\/?)>/
const ATTR_RE = /([a-zA-Z_:][a-zA-Z0-9_:.-]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?/g

interface ParsedTag {
  name: string
  attrs: Record<string, string>
  closing: boolean
  selfClosing: boolean
  length: number
}

interface KeyRef {
  n: number
}

/** 只放行 http(s)、协议相对、站内相对链接，其余（如 javascript:）一律拒绝 */
function safeUrl(raw: string | undefined): string | null {
  const url = (raw ?? '').trim()
  if (!url) return null
  if (/^https?:\/\//i.test(url) || url.startsWith('//') || url.startsWith('/')) return url
  return null
}

function readTag(src: string): ParsedTag | null {
  const m = TAG_RE.exec(src)
  if (!m) return null
  const attrs: Record<string, string> = {}
  const attrSrc = m[3]
  if (attrSrc) {
    ATTR_RE.lastIndex = 0
    let am: RegExpExecArray | null
    while ((am = ATTR_RE.exec(attrSrc)) !== null) {
      attrs[am[1].toLowerCase()] = am[2] ?? am[3] ?? am[4] ?? ''
    }
  }
  return { name: m[2].toLowerCase(), attrs, closing: m[1] === '/', selfClosing: m[4] === '/', length: m[0].length }
}

/** 配对最近的同名闭合标签（支持同名嵌套） */
function findClose(src: string, name: string, from: number): { inner: string; end: number } | null {
  let depth = 1
  let i = from
  while (i < src.length) {
    const lt = src.indexOf('<', i)
    if (lt < 0) return null
    const tag = readTag(src.slice(lt))
    if (!tag) {
      i = lt + 1
      continue
    }
    if (tag.name === name && !tag.selfClosing) {
      if (tag.closing) {
        depth -= 1
        if (depth === 0) return { inner: src.slice(from, lt), end: lt + tag.length }
      } else {
        depth += 1
      }
    }
    i = lt + tag.length
  }
  return null
}

interface LinkParts {
  text: string
  url: string
  end: number
}

function readLink(src: string, start: number): LinkParts | null {
  const closeBracket = src.indexOf(']', start + 1)
  if (closeBracket < 0 || src[closeBracket + 1] !== '(') return null
  const closeParen = src.indexOf(')', closeBracket + 2)
  if (closeParen < 0) return null
  let url = src.slice(closeBracket + 2, closeParen).trim()
  const sp = url.search(/\s/)
  if (sp >= 0) url = url.slice(0, sp)
  return { text: src.slice(start + 1, closeBracket), url, end: closeParen + 1 }
}

function renderVoidTag(tag: ParsedTag, key: string): ReactNode {
  if (tag.name === 'br') return <br key={key} />
  const src = safeUrl(tag.attrs.src)
  if (!src) return null
  const w = Number(tag.attrs.width)
  const h = Number(tag.attrs.height)
  return (
    <img
      key={key}
      src={src}
      alt={tag.attrs.alt ?? ''}
      width={Number.isFinite(w) && w > 0 ? w : undefined}
      height={Number.isFinite(h) && h > 0 ? h : undefined}
      loading="lazy"
      decoding="async"
      style={IMG_STYLE}
    />
  )
}

function renderContainerTag(tag: ParsedTag, inner: string, keyRef: KeyRef, key: string): ReactNode {
  const children = renderInline(inner, keyRef)
  switch (tag.name) {
    case 'a': {
      const href = safeUrl(tag.attrs.href)
      if (!href) return <span key={key}>{children}</span>
      // 跳转链接一律新开标签页；显式写了 target 则尊重原值
      const target = tag.attrs.target || '_blank'
      return (
        <Link
          key={key}
          href={href}
          target={target}
          rel={target === '_blank' ? 'noopener noreferrer' : undefined}
          title={tag.attrs.title}
          underline="hover"
          sx={{ fontWeight: 600 }}
        >
          {children}
        </Link>
      )
    }
    case 'b':
    case 'strong':
      return (
        <strong key={key} style={{ fontWeight: 700 }}>
          {children}
        </strong>
      )
    case 'i':
    case 'em':
      return <em key={key}>{children}</em>
    case 'code':
      return (
        <code key={key} style={CODE_STYLE}>
          {children}
        </code>
      )
    default:
      return <span key={key}>{children}</span>
  }
}

function renderInline(src: string, keyRef: KeyRef): ReactNode[] {
  const out: ReactNode[] = []
  const nextKey = () => `i${keyRef.n++}`
  let buf = ''
  let i = 0
  const flush = () => {
    if (buf) {
      out.push(buf)
      buf = ''
    }
  }

  while (i < src.length) {
    const ch = src[i]

    if (ch === '\\' && i + 1 < src.length && '\\`*_[]()!<>'.includes(src[i + 1])) {
      buf += src[i + 1]
      i += 2
      continue
    }

    if (ch === '`') {
      const end = src.indexOf('`', i + 1)
      if (end > i) {
        flush()
        out.push(
          <code key={nextKey()} style={CODE_STYLE}>
            {src.slice(i + 1, end)}
          </code>,
        )
        i = end + 1
        continue
      }
    }

    if (ch === '!' && src[i + 1] === '[') {
      const link = readLink(src, i + 1)
      if (link) {
        flush()
        const url = safeUrl(link.url)
        if (url) {
          out.push(
            <img key={nextKey()} src={url} alt={link.text} loading="lazy" decoding="async" style={IMG_STYLE} />,
          )
        } else {
          out.push(link.text)
        }
        i = link.end
        continue
      }
    }

    if (ch === '[') {
      const link = readLink(src, i)
      if (link) {
        flush()
        const url = safeUrl(link.url)
        if (url) {
          out.push(
            <Link
              key={nextKey()}
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              underline="hover"
              sx={{ fontWeight: 600, wordBreak: 'break-all' }}
            >
              {link.text}
            </Link>,
          )
        } else {
          out.push(link.text)
        }
        i = link.end
        continue
      }
    }

    if (ch === '*' && src[i + 1] === '*') {
      const end = src.indexOf('**', i + 2)
      if (end > i + 2) {
        flush()
        out.push(
          <strong key={nextKey()} style={{ fontWeight: 700 }}>
            {renderInline(src.slice(i + 2, end), keyRef)}
          </strong>,
        )
        i = end + 2
        continue
      }
    }

    if (ch === '*') {
      const end = src.indexOf('*', i + 1)
      if (end > i + 1) {
        flush()
        out.push(<em key={nextKey()}>{renderInline(src.slice(i + 1, end), keyRef)}</em>)
        i = end + 1
        continue
      }
    }

    if (ch === '<') {
      const tag = readTag(src.slice(i))
      if (tag && VOID_TAGS.has(tag.name) && !tag.closing) {
        flush()
        out.push(renderVoidTag(tag, nextKey()))
        i += tag.length
        continue
      }
      if (tag && CONTAINER_TAGS.has(tag.name) && !tag.closing && !tag.selfClosing) {
        const close = findClose(src, tag.name, i + tag.length)
        if (close) {
          flush()
          out.push(renderContainerTag(tag, close.inner, keyRef, nextKey()))
          i = close.end
          continue
        }
      }
    }

    buf += ch
    i += 1
  }

  flush()
  return out
}

const HEADING_TAGS = ['h1', 'h2', 'h3', 'h4', 'h5', 'h6'] as const
const HEADING_SIZE: Record<number, number> = { 1: 22, 2: 19, 3: 17, 4: 15.5, 5: 14.5, 6: 14 }
const HR_RE = /^(?:-{3,}|\*{3,}|_{3,})$/
const UL_RE = /^[-*+]\s+/
const OL_RE = /^\d+\.\s+/
const HEADING_RE = /^(#{1,6})\s+(.*)$/

function isBlockStart(line: string): boolean {
  return HEADING_RE.test(line) || line.startsWith('>') || UL_RE.test(line) || OL_RE.test(line) || HR_RE.test(line)
}

function renderMarkdown(src: string): ReactNode {
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  const blocks: ReactNode[] = []
  const keyRef: KeyRef = { n: 0 }
  let b = 0
  const bkey = () => `b${b++}`
  let i = 0

  while (i < lines.length) {
    const trimmed = lines[i].trim()
    if (!trimmed) {
      i += 1
      continue
    }

    if (HR_RE.test(trimmed)) {
      blocks.push(<Divider key={bkey()} sx={{ my: 1.5 }} />)
      i += 1
      continue
    }

    const heading = HEADING_RE.exec(trimmed)
    if (heading) {
      const level = heading[1].length
      blocks.push(
        <Typography
          key={bkey()}
          component={HEADING_TAGS[level - 1]}
          sx={{ fontSize: HEADING_SIZE[level], fontWeight: 700, mt: 1.5, mb: 0.5, lineHeight: 1.35 }}
        >
          {renderInline(heading[2], keyRef)}
        </Typography>,
      )
      i += 1
      continue
    }

    if (trimmed.startsWith('>')) {
      const quote: string[] = []
      while (i < lines.length && lines[i].trim().startsWith('>')) {
        quote.push(lines[i].trim().replace(/^>\s?/, ''))
        i += 1
      }
      blocks.push(
        <Box
          key={bkey()}
          sx={{ borderLeft: '3px solid', borderColor: 'primary.main', pl: 1.5, my: 1, color: 'text.secondary' }}
        >
          {quote.map((line, idx) => (
            <Typography key={idx} variant="body2" sx={{ my: 0.25 }}>
              {renderInline(line, keyRef)}
            </Typography>
          ))}
        </Box>,
      )
      continue
    }

    if (UL_RE.test(trimmed) || OL_RE.test(trimmed)) {
      const ordered = OL_RE.test(trimmed)
      const itemRe = ordered ? OL_RE : UL_RE
      const items: string[] = []
      while (i < lines.length && itemRe.test(lines[i].trim())) {
        items.push(lines[i].trim().replace(itemRe, ''))
        i += 1
      }
      blocks.push(
        <Box key={bkey()} component={ordered ? 'ol' : 'ul'} sx={{ my: 0.75, pl: 3, '& li': { mb: 0.25 } }}>
          {items.map((item, idx) => (
            <Box component="li" key={idx}>
              {renderInline(item, keyRef)}
            </Box>
          ))}
        </Box>,
      )
      continue
    }

    const para: string[] = []
    while (i < lines.length) {
      const line = lines[i].trim()
      if (!line || isBlockStart(line)) break
      para.push(line)
      i += 1
    }
    const nodes: ReactNode[] = []
    para.forEach((line, idx) => {
      if (idx > 0) nodes.push(<br key={`lb${idx}`} />)
      nodes.push(...renderInline(line, keyRef))
    })
    blocks.push(
      <Typography key={bkey()} component="div" variant="body1" sx={{ my: 0.75, lineHeight: 1.7 }}>
        {nodes}
      </Typography>,
    )
  }

  return <>{blocks}</>
}

interface AnnouncementDialogProps {
  content: string
  onDismiss: () => void
}

export default function AnnouncementDialog({ content, onDismiss }: AnnouncementDialogProps) {
  const rendered = useMemo(() => renderMarkdown(content), [content])

  return (
    <Dialog
      open
      onClose={onDismiss}
      fullWidth
      maxWidth="sm"
      slotProps={{
        paper: {
          sx: {
            display: 'flex',
            flexDirection: 'column',
            maxHeight: { xs: '82dvh', sm: '70dvh' },
            borderRadius: 4,
            overflow: 'hidden',
          },
        },
      }}
    >
      <DialogContent sx={{ flex: '1 1 auto', minHeight: 0, overflowY: 'auto', p: { xs: 2.5, sm: 3.5 } }}>
        {rendered}
      </DialogContent>
      <Button
        fullWidth
        onClick={onDismiss}
        variant="contained"
        disableElevation
        sx={{ flexShrink: 0, borderRadius: 0, py: 1.2 }}
      >
        不再提示
      </Button>
    </Dialog>
  )
}
