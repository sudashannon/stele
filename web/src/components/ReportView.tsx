import { useEffect, useRef, useState } from 'react'
import { fetchChatConfig, generateReport, listReports, getReport, deleteReport } from '../api/client'
import type { ChatConfig, ReportCoverage, ReportMeta, ReportResponse, ReportType, WorkspaceConfig } from '../api/types'
import { MarkdownViewer } from './MarkdownViewer'
import { Icon } from './icons'
import { Modal } from './Modal'

interface Props {
  workspace: string | null
  workspaces: WorkspaceConfig[]
  // Owned by the SideRail settings wiring; ReportView only needs
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
type CompatibleReportCoverage = ReportCoverage & { totalDocuments?: number }

function formatReportDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '未知时间' : date.toLocaleString('zh-CN', { dateStyle: 'medium', timeStyle: 'short' })
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
  const [resultWorkspace, setResultWorkspace] = useState<string | null>(null)
  const [error, setError] = useState('')

  const [history, setHistory] = useState<ReportMeta[]>([])
  const [historyLoading, setHistoryLoading] = useState(true)
  const [historyError, setHistoryError] = useState('')
  const [openingName, setOpeningName] = useState<string | null>(null)
  const [downloading, setDownloading] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const historyRequestId = useRef(0)

  useEffect(() => {
    setConfigLoading(true)
    fetchChatConfig()
      .then((cfg) => setProviderReady(isProviderReady(cfg)))
      .catch(() => setProviderReady(false))
      .finally(() => setConfigLoading(false))
  }, [])

  function reloadHistory() {
    setHistoryLoading(true)
    setHistoryError('')
    listReports()
      .then(setHistory)
      .catch((err) => {
        setHistory([])
        setHistoryError(err instanceof Error ? err.message : '历史记录加载失败')
      })
      .finally(() => setHistoryLoading(false))
  }

  useEffect(() => {
    reloadHistory()
  }, [])

  async function handleGenerate() {
    const requestedWorkspace = reportWorkspace || '全部'
    setResultWorkspace(null)
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
      setResultWorkspace(requestedWorkspace)
      setLoadedName(resp.savedName ?? null)
      reloadHistory()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setGenerating(false)
    }
  }

  async function handleHistoryClick(item: ReportMeta) {
    const requestId = ++historyRequestId.current
    setError('')
    setOpeningName(item.name)
    try {
      const report = await getReport(item.name)
      if (historyRequestId.current !== requestId) return
      setLoadedName(item.name)
      setResultWorkspace(null)
      setResult(report)
    } catch (err) {
      if (historyRequestId.current !== requestId) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (historyRequestId.current === requestId) {
        setOpeningName(null)
      }
    }
  }

  async function handleDownload() {
    if (!result || downloading) return
    setError('')
    setDownloading(true)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    let url: string | null = null
    try {
      const ext = result.format === 'html' ? 'html' : 'md'
      const mime = result.format === 'html' ? 'text/html' : 'text/markdown'
      const blob = new Blob([result.body], { type: mime })
      url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${type}-report-${end}.${ext}`
      a.click()
    } catch (err) {
      setError(err instanceof Error ? err.message : '下载失败')
    } finally {
      if (url) URL.revokeObjectURL(url)
      setDownloading(false)
    }
  }

  async function handleDelete() {
    if (!result || deleting) return
    const item = history.find((h) => h.name === loadedName)
    if (!item) {
      setConfirmingDelete(false)
      setError('找不到要删除的历史报告')
      return
    }
    setDeleting(true)
    setError('')
    try {
      await deleteReport(item.name)
      setResult(null)
      setLoadedName(null)
      setConfirmingDelete(false)
      reloadHistory()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setConfirmingDelete(false)
    } finally {
      setDeleting(false)
    }
  }


  if (configLoading) {
    return <div className="p-4 text-sm text-[var(--color-text-secondary)]">加载中…</div>
  }

  if (!providerReady) {
    return (
      <div
        data-testid="report-gate"
        className="flex flex-col items-center justify-center gap-3 text-center border border-dashed border-[var(--color-border)] bg-[var(--color-surface)] py-24 px-6"
        style={{ boxShadow: 'var(--shadow-modal)' }}
      >
        <Icon name="report" size={32} className="text-[var(--color-text-tertiary)]" />
        <p className="text-sm font-medium text-[var(--color-text-primary)]">请先在设置中配置 LLM provider</p>
        <p className="text-xs text-[var(--color-text-secondary)]">生成报告需要一个已配置 API Key 的 provider</p>
        {onOpenSettings && (
          <button
            type="button"
            onClick={onOpenSettings}
            className="mt-1 text-sm font-medium px-3 py-1.5 bg-[var(--color-accent)] text-[var(--color-text-on-color)]"
            style={{ boxShadow: '0 6px 14px color-mix(in_srgb,var(--color-accent)_35%,transparent)' }}
          >
            去设置
          </button>
        )}
      </div>
    )
  }

  const coverage = result?.coverage as CompatibleReportCoverage | undefined
  const hasCoverage = !!result && (
    result.inputDocumentCount !== undefined ||
    result.clusterCount !== undefined ||
    result.sourceReportIDs !== undefined ||
    coverage !== undefined
  )

  return (
    <div className="flex flex-col gap-4 h-full min-h-0">
      <div
        data-testid="report-params"
        className="bg-[var(--color-surface)] p-4 flex flex-col gap-3 shrink-0"
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
            disabled={generating || openingName !== null}
            onClick={handleGenerate}
            className="text-sm font-medium px-3 py-1.5 bg-[var(--color-accent)] text-[var(--color-text-on-color)] disabled:opacity-50"
            style={{ boxShadow: '0 6px 14px color-mix(in_srgb,var(--color-accent)_35%,transparent)' }}
          >
            {generating ? '生成中…' : '生成'}
          </button>
        </div>
        {generating && (
          <div data-testid="report-progress" className="text-xs text-[var(--color-text-secondary)]">
            正在读取 Wiki 文档、聚类并分层合成…
          </div>
        )}
        {error && (
          <div data-testid="report-error" role="alert" className="text-xs text-[var(--color-danger)]">
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
                  disabled={downloading}
                  onClick={handleDownload}
                  className="inline-flex items-center gap-1.5 text-xs font-medium px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-bg)] disabled:opacity-50"
                >
                  <Icon name={downloading ? 'spinner' : 'download'} size={14} className={downloading ? 'animate-spin' : undefined} />
                  {downloading ? '下载中…' : '下载'}
                </button>
                {loadedName && (
                  <button
                    type="button"
                    data-testid="report-delete"
                    disabled={deleting}
                    onClick={() => setConfirmingDelete(true)}
                    className="inline-flex items-center gap-1.5 text-xs font-medium px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-danger)] hover:bg-[color-mix(in_srgb,var(--color-danger)_10%,var(--color-surface))] disabled:opacity-50"
                  >
                    <Icon name={deleting ? 'spinner' : 'trash'} size={14} className={deleting ? 'animate-spin' : undefined} />
                    {deleting ? '删除中…' : '删除'}
                  </button>
                )}
              </div>
              {hasCoverage && (
                <div
                  data-testid="report-coverage"
                  className="shrink-0 flex flex-col gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2 text-xs text-[var(--color-text-secondary)]"
                >
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                    <span>输入文档 {result.inputDocumentCount ?? 0}</span>
                    {coverage?.totalDocuments !== undefined && <span>文档总数 {coverage.totalDocuments}</span>}
                    <span>Workspace {resultWorkspace ?? '历史报告未提供'}</span>
                    <span>来源文档 {coverage?.sourceDocuments ?? 0}</span>
                    <span>背景文档 {coverage?.contextDocuments ?? 0}</span>
                    <span>可读文档 {coverage?.readableDocuments ?? 0}</span>
                    <span>截断文档 {coverage?.truncatedDocuments ?? 0}</span>
                    <span>缺少嵌入 {coverage?.missingEmbeddings ?? 0}</span>
                    <span>跳过文档 {coverage?.skippedDocuments?.length ?? 0}</span>
                    <span>失败 Workspace {coverage?.failedWorkspaces?.length ?? 0}</span>
                    <span>主题 {result.clusterCount ?? 0}</span>
                    <span>聚类模式 {coverage?.clusteringMode ?? '未提供'}</span>
                    <span>复用周报 {result.sourceReportIDs?.length ?? 0}</span>
                  </div>
                  {!!coverage?.failedWorkspaces?.length && (
                    <p data-testid="report-failed-workspaces">
                      无法读取的 Workspace：{coverage.failedWorkspaces.join('、')}
                    </p>
                  )}
                  {!!coverage?.skippedDocuments?.length && (
                    <ul data-testid="report-skipped-documents" className="space-y-1">
                      {coverage.skippedDocuments.map((document) => (
                        <li key={`${document.path}:${document.error}`}>
                          跳过 {document.path}：{document.error}
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              )}
              <div className="flex-1 min-h-0">
                {result.format === 'html' ? (
                  <iframe data-testid="report-result-frame" sandbox="" srcDoc={result.body} className="w-full h-full border border-[var(--color-border)] bg-[var(--color-surface)]" />
                ) : (
                  <MarkdownViewer path={null} body={result.body} onClose={() => setResult(null)} />
                )}
              </div>
            </div>
          ) : (
            <div
              data-testid="report-empty-state"
              className="h-full flex flex-col items-center justify-center gap-2 text-center border border-dashed border-[var(--color-border)] bg-[var(--color-surface)] py-24 px-6"
            >
              <p className="text-sm text-[var(--color-text-secondary)]">选择参数后点击「生成」，或从右侧历史记录中选择</p>
            </div>
          )}
        </div>

        <aside
          data-testid="report-history"
          className="w-56 shrink-0 bg-[var(--color-surface)] p-3 overflow-y-auto"
          style={{ boxShadow: 'var(--shadow-modal)' }}
        >
          <div className="text-xs font-semibold text-[var(--color-text-secondary)] mb-2">历史记录</div>
          {historyLoading ? (
            <div role="status" className="text-xs text-[var(--color-text-secondary)]">正在加载历史记录…</div>
          ) : historyError ? (
            <div role="alert" data-testid="report-history-error" className="text-xs text-[var(--color-danger)]">
              历史记录加载失败：{historyError}
            </div>
          ) : history.length === 0 ? (
            <div className="text-xs text-[var(--color-text-tertiary)]">暂无记录</div>
          ) : (
            <ul className="space-y-1">
              {history.map((item) => {
                const opening = openingName === item.name
                return (
                  <li key={item.name}>
                    <button
                      type="button"
                      data-testid="report-history-item"
                      disabled={opening}
                      onClick={() => handleHistoryClick(item)}
                      className="w-full text-left text-xs text-[var(--color-text-primary)] hover:text-[var(--color-accent)] hover:bg-[var(--color-bg)] px-2 py-1.5 disabled:opacity-50"
                      title={item.name}
                    >
                      <span className="block font-medium">
                        {item.type === 'weekly' ? '周报' : '月报'} · {item.start} 至 {item.end}
                      </span>
                      <time className="block mt-0.5 text-[var(--color-text-tertiary)]" dateTime={item.createdAt}>
                        {formatReportDate(item.createdAt)}
                      </time>
                      {opening && <span role="status" className="block mt-0.5">正在打开…</span>}
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
        </aside>
      </div>
      {confirmingDelete && loadedName && (
        <Modal
          title="确认删除报告"
          onClose={() => setConfirmingDelete(false)}
          dismissible={!deleting}
          data-testid="report-delete-confirm"
        >
          <div className="p-4">
            <p className="text-sm text-[var(--color-text-secondary)]">
              删除“{loadedName}”后无法恢复。
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                disabled={deleting}
                onClick={() => setConfirmingDelete(false)}
                className="px-3 py-2 text-sm border border-[var(--color-border)] text-[var(--color-text-primary)] disabled:opacity-50"
              >
                取消
              </button>
              <button
                type="button"
                data-testid="report-delete-confirm-btn"
                disabled={deleting}
                onClick={handleDelete}
                className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-[var(--color-danger)] text-[var(--color-text-on-color)] disabled:opacity-50"
              >
                <Icon name={deleting ? 'spinner' : 'trash'} size={14} className={deleting ? 'animate-spin' : undefined} />
                {deleting ? '正在删除…' : '确认删除'}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}
