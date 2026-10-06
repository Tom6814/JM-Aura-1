// API 客户端：统一处理后端信封 {st,msg,data}、{detail} 错误体与登录门 st=1014。
// 所有响应 HTTP 状态恒为 200（除 FastAPI 风格 4xx），以 st 判定业务结果。

import { apiSourcePath, sourceFromPath } from './source'

export const API_BASE = import.meta.env.VITE_API_BASE ?? ''

export const STATUS_OK = 1001
export const STATUS_NOT_LOGIN = 1014

export const UNAUTHORIZED_EVENT = 'aura:unauthorized'

export class ApiError extends Error {
  readonly st: number
  readonly httpStatus: number

  constructor(st: number, message: string, httpStatus = 0) {
    super(message)
    this.name = 'ApiError'
    this.st = st
    this.httpStatus = httpStatus
  }
}

interface EnvelopeBody {
  st?: number
  msg?: string
  data?: unknown
  detail?: unknown
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  // 多源：页面里写死的 /api/v2/jm/... 跟随当前 URL 前缀切换到对应源。
  path = apiSourcePath(path)
  init = withHistorySource(path, init)
  let res: Response
  try {
    res = await fetch(API_BASE + path, { credentials: 'same-origin', ...init })
  } catch {
    throw new ApiError(-1, '网络错误：无法连接服务器')
  }
  let body: EnvelopeBody = {}
  try {
    body = (await res.json()) as EnvelopeBody
  } catch {
    /* 空响应体或非 JSON */
  }
  if (body.detail !== undefined || (!res.ok && body.st === undefined)) {
    const detail =
      typeof body.detail === 'string' ? body.detail : `请求失败（HTTP ${res.status}）`
    throw new ApiError(-1, detail, res.status)
  }
  const st = typeof body.st === 'number' ? body.st : res.ok ? STATUS_OK : -1
  if (st === STATUS_NOT_LOGIN) {
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
    throw new ApiError(STATUS_NOT_LOGIN, '请先登录 JM 账号', res.status)
  }
  if (st !== STATUS_OK) {
    throw new ApiError(st, body.msg || `请求失败（st=${st}）`, res.status)
  }
  // 信封端点返回 data；legacy 裸响应（如 /api/config，st 内嵌且无 data 键）返回整个响应体。
  return (body.data !== undefined ? body.data : body) as T
}

function jsonInit(method: string, body?: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? '' : JSON.stringify(body),
  }
}

// 阅读历史需要记录自己属于哪个源，否则历史页无法跳回正确的源。
// 在客户端统一注入，避免每个页面（Reader / ComicDetail / NovelReader）重复处理。
const HISTORY_PATH = '/api/aura/library/history'

function withHistorySource(path: string, init: RequestInit): RequestInit {
  if (path !== HISTORY_PATH || typeof init.body !== 'string' || init.body === '') return init
  try {
    const body = JSON.parse(init.body) as Record<string, unknown>
    if (body && typeof body === 'object' && body.source === undefined) {
      body.source = sourceFromPath()
      return { ...init, body: JSON.stringify(body) }
    }
  } catch {
    /* 非 JSON body 原样透传 */
  }
  return init
}

function qs(params: Record<string, string | number | undefined | null>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) => request<T>(path, jsonInit('POST', body)),
  put: <T>(path: string, body?: unknown) => request<T>(path, jsonInit('PUT', body)),
  del: <T>(path: string, body?: unknown) => request<T>(path, jsonInit('DELETE', body)),
  postForm: <T>(path: string, form: FormData) =>
    request<T>(path, { method: 'POST', body: form }),
  qs,
  url: (path: string) => API_BASE + path,
}

/**
 * 封面/列表图：经图片代理按需缩到 width 宽（后端缩放并缓存），
 * 避免浏览器解码整张大图导致的巨大内存占用（实测首页封面可省 ~95% 位图内存）。
 */
export function imgUrl(rawUrl: string | null | undefined, width = 320): string | undefined {
  if (!rawUrl) return undefined
  const path = rawUrl.startsWith('http')
    ? `/api/image-proxy?url=${encodeURIComponent(rawUrl)}&w=${width}`
    : rawUrl
  return API_BASE + path
}
