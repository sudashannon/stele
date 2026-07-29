import { useEffect, useState } from 'react'
import { fetchChatConfig, generateReport, listReports, getReport, deleteReport } from '../api/client'
import type { ChatConfig, ReportMeta, ReportResponse, ReportType, WorkspaceConfig } from '../api/types'
import { MarkdownViewer } from './MarkdownViewer'

interface Props {
  workspace: string | null
  workspaces: WorkspaceConfig[]
  // Owned by the SideRail ⚙ wiring (settings modal); ReportView only needs
  // a hook to jump there from the gate prompt, not the modal state itself.
  onOpenSettings?: () => void
}

// Mirrors the gate check the Go backend performs before POST /api/report:
// no api_key on the active provider means the request would 400 anyway, so
// the UI short-circuits to a guidance card instead of round-tripping first.
function isProviderReady(cfg: ChatConfig | null): boolean {
  if (!cfg) return false
  const active = cfg.active_provider
  const pcfg = cfg.providers?.[active]
  return !!(pcfg?.api_key && pcfg.api_key !== '')
}

export function ReportView({ workspace, workspaces, onOpenSettings }: Props) {
  const [configLoading, setConfigLoading] = useState(true)
  const [providerReady, setProviderReady] = useState(false)

  const [type, setType] = useState<ReportType>('weekly')
  const [start, setStart] = useState(() => {
    const d = new Date()
    d.setDate(d.getDate() - 7)
    return d.toISOString().slice(0, 10)
  })
  const [end, setEnd] = useState(() => new Date().toISOString().slice(0, 10))
  const [reportWorkspace, setReportWorkspace] = useState<string>(workspace ?? '')

  const [generating, setGenerating] = useState(false)
  const [result, setResult] = useState<ReportResponse | null>(null)
  const [loadedName, setLoadedName] = useState<string | null>(null)
  const [error, setError] = useState('')

  const [history, setHistory] = useState<ReportMeta[]>([])

  useEffect(() => {
    setConfigLoading(true)
    fetchChatConfig()
      .then((cfg) => setProviderReady(isProviderReady(cfg)))
      .catch(() => setProviderReady(false))
      .finally(() => setConfigLoading(false))
  }, [])

  function reloadHistory() {
    listReports()
      .then(setHistory)
      .catch(() => setHistory([]))
  }

  useEffect(() => {
    reloadHistory()
  }, [])

  async function handleGenerate() {
    setError('')
    setGenerating(true)
    setResult(null)
    try {
      const resp = await generateReport({
        type,
        start,
        end,
        ...(reportWorkspace ? { workspace: reportWorkspace } : {}),
      })
      setResult(resp)
      setLoadedName(resp.savedName ?? null)
      reloadHistory()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setGenerating(false)
    }
  }

  function handleHistoryClick(item: ReportMeta) {
    setError('')
    setLoadedName(item.name)
    getReport(item.name)
      .then(setResult)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  function handleDownload() {
    if (!result) return
    const ext = result.format === 'html' ? 'html' : 'md'
    const mime = result.format === 'html' ? 'text/html' : 'text/markdown'
    const blob = new Blob([result.body], { type: mime })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${type}-report-${end}.${ext}`
    a.click()
    URL.revokeObjectURL(url)
  }

  function handleDelete() {
    if (!result) return
    // Find the history item matching current result (best-effort: last loaded)
    const item = history.find((h) => h.name === loadedName)
    if (!item) return
    deleteReport(item.name)
      .then(() => {
        setResult(null)
        setLoadedName(null)
        listReports().then(setHistory).catch(() => {})
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  if (configLoading) {
    return <div className="p-4 text-sm text-[var(--color-text-secondary)]">加载中…</div>
  }

  if (!providerReady) {
    return (
      <div
        data-testid="report-gate"
        className="flex flex-col items-center justify-center gap-3 text-center border border-dashed border-[var(--color-border)] bg-white py-24 px-6"
        style={{ boxShadow: 'var(--shadow-modal)' }}
      >
        <span className="text-4xl text-[var(--color-text-tertiary)]" aria-hidden="true">
          📊
        </span>
        <p className="text-sm font-medium text-[var(--color-text-primary)]">请先在 ⚙ 设置中配置 LLM provider</p>
        <p className="text-xs text-[var(--color-text-secondary)]">生成报告需要一个已配置 API Key 的 provider</p>
        {onOpenSettings && (
          <button
            type="button"
            onClick={onOpenSettings}
            className="mt-1 text-sm font-medium px-3 py-1.5 bg-[var(--color-accent)] text-white"
            style={{ boxShadow: '0 6px 14px color-mix(in_srgb,var(--color-accent)_35%,transparent)' }}
          >
            去设置
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 h-full min-h-0">
      <div
        data-testid="report-params"
        className="bg-white p-4 flex flex-col gap-3 shrink-0"
        style={{ boxShadow: 'var(--shadow-modal)' }}
      >
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-1.5 text-sm">
            <input
              data-testid="report-type-weekly"
              type="radio"
              name="report-type"
              checked={type === 'weekly'}
              onChange={() => setType('weekly')}
            />
            周报
          </label>
          <label className="flex items-center gap-1.5 text-sm">
            <input
              data-testid="report-type-monthly"
              type="radio"
              name="report-type"
              checked={type === 'monthly'}
              onChange={() => setType('monthly')}
            />
            月报
          </label>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <label className="flex items-center gap-1.5 text-xs text-[var(--color-text-secondary)]">
            起始
            <input
              data-testid="report-start"
              type="date"
              value={start}
              onChange={(e) => setStart(e.target.value)}
              className="border border-[var(--color-border)] p-1.5 text-sm"
            />
          </label>
          <label className="flex items-center gap-1.5 text-xs text-[var(--color-text-secondary)]">
            截止
            <input
              data-testid="report-end"
              type="date"
              value={end}
              onChange={(e) => setEnd(e.target.value)}
              className="border border-[var(--color-border)] p-1.5 text-sm"
            />
          </label>
          <label className="flex items-center gap-1.5 text-xs text-[var(--color-text-secondary)]">
            Workspace
            <select
              data-testid="report-workspace"
              value={reportWorkspace}
              onChange={(e) => setReportWorkspace(e.target.value)}
              className="border border-[var(--color-border)] p-1.5 text-sm"
            >
              <option value="">全部</option>
              {workspaces.map((w) => (
                <option key={w.alias} value={w.alias}>
                  {w.alias}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            data-testid="report-generate"
            disabled={generating}
            onClick={handleGenerate}
            className="text-sm font-medium px-3 py-1.5 bg-[var(--color-accent)] text-white disabled:opacity-50"
            style={{ boxShadow: '0 6px 14px color-mix(in_srgb,var(--color-accent)_35%,transparent)' }}
          >
            生成
          </button>
        </div>
        {generating && (
          <div data-testid="report-progress" className="text-xs text-[var(--color-text-secondary)]">
            正在读取 Wiki 文档、聚类并分层合成…
          </div>
        )}
        {error && (
          <div data-testid="report-error" className="text-xs text-[var(--color-danger)]">
            {error}
          </div>
        )}
      </div>

      <div className="flex-1 min-h-0 flex gap-4">
        <div className="flex-1 min-h-0">
          {result ? (
            <div className="h-full min-h-0 flex flex-col gap-2">
              <div className="flex justify-end gap-2 shrink-0">
                <button
                  type="button"
                  data-testid="report-download"
                  onClick={handleDownload}
                  className="text-xs font-medium px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-bg)]"
                >
                  ⬇ 下载
                </button>
                {loadedName && (
                  <button
                    type="button"
                    data-testid="report-delete"
                    onClick={handleDelete}
                    className="text-xs font-medium px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-danger)] hover:bg-[color-mix(in_srgb,var(--color-danger)_10%,var(--color-surface))]"
                  >
                    🗑 删除
                  </button>
                )}
              </div>
              {result.inputDocumentCount !== undefined && (
                <div
                  data-testid="report-coverage"
                  className="shrink-0 flex flex-wrap items-center gap-x-4 gap-y-1 border border-[var(--color-border)] bg-white px-3 py-2 text-xs text-[var(--color-text-secondary)]"
                >
                  <span>输入文档 {result.inputDocumentCount}</span>
                  <span>主题 {result.clusterCount ?? 0}</span>
                  {result.coverage?.clusteringMode && <span>聚类 {result.coverage.clusteringMode}</span>}
                  {!!result.coverage?.contextDocuments && <span>背景文档 {result.coverage.contextDocuments}</span>}
                  {!!result.coverage?.missingEmbeddings && <span>词法降级 {result.coverage.missingEmbeddings}</span>}
                  {!!result.sourceReportIDs?.length && <span>复用周报 {result.sourceReportIDs.length}</span>}
                </div>
              )}
              <div className="flex-1 min-h-0">
                {result.format === 'html' ? (
                  <iframe data-testid="report-result-frame" srcDoc={result.body} className="w-full h-full border border-[var(--color-border)] bg-white" />
                ) : (
                  <MarkdownViewer path={null} body={result.body} onClose={() => setResult(null)} />
                )}
              </div>
            </div>
          ) : (
            <div
              data-testid="report-empty-state"
              className="h-full flex flex-col items-center justify-center gap-2 text-center border border-dashed border-[var(--color-border)] bg-white py-24 px-6"
            >
              <p className="text-sm text-[var(--color-text-secondary)]">选择参数后点击「生成」，或从右侧历史记录中选择</p>
            </div>
          )}
        </div>

        <aside
          data-testid="report-history"
          className="w-56 shrink-0 bg-white p-3 overflow-y-auto"
          style={{ boxShadow: 'var(--shadow-modal)' }}
        >
          <div className="text-xs font-semibold text-[var(--color-text-secondary)] mb-2">历史记录</div>
          {history.length === 0 ? (
            <div className="text-xs text-[var(--color-text-tertiary)]">暂无记录</div>
          ) : (
            <ul className="space-y-1">
              {history.map((item) => (
                <li key={item.name}>
                  <button
                    type="button"
                    data-testid="report-history-item"
                    onClick={() => handleHistoryClick(item)}
                    className="w-full text-left text-xs text-[var(--color-text-primary)] hover:text-[var(--color-accent)] hover:bg-[var(--color-bg)] rounded px-2 py-1.5 truncate"
                    title={item.name}
                  >
                    {item.type === 'weekly' ? '周报' : '月报'} {item.start} ~ {item.end}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </aside>
      </div>
    </div>
  )
}
