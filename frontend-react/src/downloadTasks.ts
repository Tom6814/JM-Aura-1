// 下载任务前端共用：状态展示映射 + localStorage 任务登记。
// 后端无「任务列表」端点，已创建任务由前端登记并轮询追踪。

export interface TaskEntry {
  task_id: string
  title: string
}

/** 漫画整本下载（Downloads 页）登记键 */
export const COMIC_DL_LS_KEY = 'aura.dl.tasks'
/** 小说导出（NovelDetail 发起、Downloads 页追踪）登记键 */
export const NOVEL_EXPORT_LS_KEY = 'aura.novel.export.tasks'

export function loadEntries(key: string): TaskEntry[] {
  try {
    const arr = JSON.parse(localStorage.getItem(key) ?? '[]')
    if (!Array.isArray(arr)) return []
    return arr
      .filter((e) => e && typeof e.task_id === 'string')
      .map((e) => ({ task_id: e.task_id as string, title: String(e.title ?? '') }))
  } catch {
    return []
  }
}

export function persistEntries(key: string, list: TaskEntry[]) {
  localStorage.setItem(key, JSON.stringify(list))
}

export function addEntry(key: string, entry: TaskEntry): TaskEntry[] {
  const next = [entry, ...loadEntries(key).filter((e) => e.task_id !== entry.task_id)]
  persistEntries(key, next)
  return next
}

export function removeEntry(key: string, taskId: string): TaskEntry[] {
  const next = loadEntries(key).filter((e) => e.task_id !== taskId)
  persistEntries(key, next)
  return next
}

export const STATUS_LABEL: Record<string, string> = {
  queued: '排队中',
  downloading: '下载中',
  zipping: '打包中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

export function statusColor(status: string): 'default' | 'primary' | 'success' | 'error' | 'warning' {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'error'
  if (status === 'cancelled') return 'default'
  if (status === 'queued') return 'warning'
  return 'primary'
}

/** 统一把后端可能返回的 0–1 或 0–100 百分比归一为 0–100 */
export function percentOf(percent: number): number {
  return Math.min(100, percent * (percent <= 1 ? 100 : 1))
}
