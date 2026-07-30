import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ReportView } from './ReportView'
import { deleteReport, fetchChatConfig, generateReport, listReports, getReport } from '../api/client'
import type { ChatConfig, ReportMeta, ReportResponse, WorkspaceConfig } from '../api/types'

vi.mock('../api/client', () => ({
  fetchChatConfig: vi.fn(),
  generateReport: vi.fn(),
  listReports: vi.fn(),
  getReport: vi.fn(),
  deleteReport: vi.fn(),
}))

const workspaces: WorkspaceConfig[] = [{ alias: 'ws1', path: '/a', color: '#0063f8' }]

function readyConfig(): ChatConfig {
  return {
    active_provider: 'anthropic',
    providers: {
      anthropic: { api_key: 'sk-c****umMM', api_base: '', model: 'claude-3-5-sonnet', temperature: 0.7, max_tokens: 4096, thinking: 'auto' },
    },
  }
}

function emptyConfig(): ChatConfig {
  return {
    active_provider: 'anthropic',
    providers: {
      anthropic: { api_key: '', api_base: '', model: '', temperature: 0.7, max_tokens: 4096, thinking: 'auto' },
    },
  }
}

describe('ReportView', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.mocked(fetchChatConfig).mockReset()
    vi.mocked(generateReport).mockReset()
    vi.mocked(listReports).mockReset()
    vi.mocked(getReport).mockReset()
    vi.mocked(deleteReport).mockReset()
    vi.mocked(listReports).mockResolvedValue([])
  })

  it('renders the parameter controls once the provider gate passes', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    await screen.findByTestId('report-generate')
    expect(screen.getByTestId('report-type-weekly')).toBeTruthy()
    expect(screen.getByTestId('report-type-monthly')).toBeTruthy()
    expect(screen.getByTestId('report-start')).toBeTruthy()
    expect(screen.getByTestId('report-end')).toBeTruthy()
    expect(screen.getByTestId('report-workspace')).toBeTruthy()
  })

  it('shows a gate prompt guiding the user to Settings when no provider api_key is configured', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(emptyConfig())
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    await screen.findByTestId('report-gate')
    expect(screen.getByText(/请先在.*设置中配置/)).toBeTruthy()
    expect(screen.queryByTestId('report-generate')).toBeNull()
    expect(generateReport).not.toHaveBeenCalled()
  })

  it('calls generateReport and shows a progress state while the request is in flight', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    const { promise, resolve } = Promise.withResolvers<ReportResponse>()
    vi.mocked(generateReport).mockReturnValue(promise)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    await screen.findByTestId('report-generate')
    fireEvent.click(screen.getByTestId('report-generate'))

    await waitFor(() => expect(generateReport).toHaveBeenCalledTimes(1))
    expect((screen.getByTestId('report-generate') as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByTestId('report-generate').textContent).toBe('生成中…')
    expect(screen.getByTestId('report-progress')).toBeTruthy()

    resolve({ format: 'md', body: '# 周报\n内容' })
    await waitFor(() => expect(screen.queryByTestId('report-progress')).toBeNull())
  })

  it('labels a successful result with the workspace captured for that request', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    const { promise, resolve } = Promise.withResolvers<ReportResponse>()
    vi.mocked(generateReport).mockReturnValue(promise)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))
    fireEvent.change(screen.getByTestId('report-workspace'), { target: { value: '' } })
    resolve({ format: 'md', body: '# 已生成', inputDocumentCount: 1 })

    const coverage = await screen.findByTestId('report-coverage')
    expect(coverage.textContent).toContain('Workspace ws1')
  })

  it('clears the previous workspace coverage when a later generation fails', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    const failed = Promise.withResolvers<ReportResponse>()
    vi.mocked(generateReport)
      .mockResolvedValueOnce({ format: 'md', body: '# 旧结果', inputDocumentCount: 1 })
      .mockReturnValueOnce(failed.promise)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))
    expect((await screen.findByTestId('report-coverage')).textContent).toContain('Workspace ws1')

    fireEvent.change(screen.getByTestId('report-workspace'), { target: { value: '' } })
    fireEvent.click(screen.getByTestId('report-generate'))
    expect(screen.queryByTestId('report-coverage')).toBeNull()
    failed.reject(new Error('生成失败'))

    await screen.findByText('生成失败')
    expect(screen.queryByTestId('report-coverage')).toBeNull()
  })

  it('renders a weekly markdown result via MarkdownViewer body prop', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(generateReport).mockResolvedValue({
      format: 'md',
      body: '# 周报标题\n正文内容',
      inputDocumentCount: 12,
      clusterCount: 3,
      coverage: {
        sourceDocuments: 12,
        contextDocuments: 2,
        readableDocuments: 14,
        truncatedDocuments: 0,
        missingEmbeddings: 1,
        clusteringMode: 'hybrid',
      },
    })
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    await screen.findByTestId('report-generate')
    fireEvent.click(screen.getByTestId('report-generate'))

    await screen.findByText('周报标题')
    expect(screen.getByTestId('report-download')).toBeTruthy()
    expect(screen.getByTestId('report-coverage').textContent).toContain('输入文档 12')
    expect(screen.getByTestId('report-coverage').textContent).toContain('缺少嵌入 1')
  })

  it('renders every coverage field, including zero counts and failure details', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(generateReport).mockResolvedValue({
      format: 'md',
      body: '# 完整覆盖率',
      inputDocumentCount: 9,
      clusterCount: 0,
      sourceReportIDs: [],
      coverage: {
        totalDocuments: 11,
        sourceDocuments: 9,
        contextDocuments: 0,
        readableDocuments: 8,
        truncatedDocuments: 2,
        missingEmbeddings: 0,
        failedWorkspaces: ['offline-ws'],
        skippedDocuments: [{ path: 'broken.md', error: 'permission denied' }],
        clusteringMode: 'lexical',
      },
    } as ReportResponse)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))

    const coverage = await screen.findByTestId('report-coverage')
    expect(coverage.textContent).toContain('文档总数 11')
    expect(coverage.textContent).toContain('来源文档 9')
    expect(coverage.textContent).toContain('背景文档 0')
    expect(coverage.textContent).toContain('可读文档 8')
    expect(coverage.textContent).toContain('截断文档 2')
    expect(coverage.textContent).toContain('缺少嵌入 0')
    expect(coverage.textContent).toContain('跳过文档 1')
    expect(coverage.textContent).toContain('失败 Workspace 1')
    expect(coverage.textContent).toContain('主题 0')
    expect(coverage.textContent).toContain('聚类模式 lexical')
    expect(coverage.textContent).toContain('复用周报 0')
    expect(screen.getByTestId('report-failed-workspaces').textContent).toContain('offline-ws')
    expect(screen.getByTestId('report-skipped-documents').textContent).toContain('broken.md：permission denied')
  })

  it('safely renders an old response with undefined coverage fields', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(generateReport).mockResolvedValue({
      format: 'md',
      body: '# 旧报告',
      inputDocumentCount: 0,
    })
    render(<ReportView workspace={null} workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))

    const coverage = await screen.findByTestId('report-coverage')
    expect(coverage.textContent).toContain('输入文档 0')
    expect(coverage.textContent).not.toContain('文档总数')
    expect(coverage.textContent).toContain('来源文档 0')
    expect(coverage.textContent).toContain('截断文档 0')
    expect(coverage.textContent).toContain('跳过文档 0')
    expect(coverage.textContent).toContain('失败 Workspace 0')
    expect(coverage.textContent).toContain('聚类模式 未提供')
    expect(screen.queryByTestId('report-failed-workspaces')).toBeNull()
    expect(screen.queryByTestId('report-skipped-documents')).toBeNull()
  })

  it('renders a monthly html result inside an iframe via srcDoc', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(generateReport).mockResolvedValue({ format: 'html', body: '<html><body>月报内容</body></html>' })
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    await screen.findByTestId('report-generate')
    fireEvent.click(screen.getByTestId('report-type-monthly'))
    fireEvent.click(screen.getByTestId('report-generate'))

    const frame = (await screen.findByTestId('report-result-frame')) as HTMLIFrameElement
    expect(frame.srcdoc).toBe('<html><body>月报内容</body></html>')
    expect(frame.getAttribute('sandbox')).toBe('')
  })

  it('surfaces a generate error inline without crashing', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(generateReport).mockRejectedValue(new Error('provider 未配置'))
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    await screen.findByTestId('report-generate')
    fireEvent.click(screen.getByTestId('report-generate'))

    await screen.findByText('provider 未配置')
    expect(screen.queryByTestId('report-progress')).toBeNull()
  })

  it('shows history loading and failure states', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    const { promise, reject } = Promise.withResolvers<ReportMeta[]>()
    vi.mocked(listReports).mockReturnValue(promise)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    expect((await screen.findByRole('status')).textContent).toContain('正在加载历史记录')
    reject(new Error('历史存储不可用'))

    expect((await screen.findByTestId('report-history-error')).textContent).toContain('历史存储不可用')
  })

  it('disables history actions and labels a report while it is opening', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([
      { name: 'weekly.md', type: 'weekly', start: '2026-01-01', end: '2026-01-07', createdAt: '2026-01-08T00:00:00Z' },
      { name: 'monthly.html', type: 'monthly', start: '2026-01-01', end: '2026-01-31', createdAt: '2026-02-01T00:00:00Z' },
    ])
    const { promise, resolve } = Promise.withResolvers<ReportResponse>()
    vi.mocked(getReport).mockReturnValue(promise)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    const [openingItem, idleItem] = await screen.findAllByTestId('report-history-item')
    fireEvent.click(openingItem)

    expect((openingItem as HTMLButtonElement).disabled).toBe(true)
    expect((idleItem as HTMLButtonElement).disabled).toBe(false)
    expect(openingItem.textContent).toContain('正在打开…')
    resolve({ format: 'md', body: '# 已打开' })
    await screen.findByText('已打开')
  })

  it('ignores an older history success and keeps the latest item opening', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([
      { name: 'older.md', type: 'weekly', start: '2026-01-01', end: '2026-01-07', createdAt: '2026-01-08T00:00:00Z' },
      { name: 'latest.md', type: 'weekly', start: '2026-01-08', end: '2026-01-14', createdAt: '2026-01-15T00:00:00Z' },
    ])
    const older = Promise.withResolvers<ReportResponse>()
    const latest = Promise.withResolvers<ReportResponse>()
    vi.mocked(getReport)
      .mockReturnValueOnce(older.promise)
      .mockReturnValueOnce(latest.promise)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    const [olderItem, latestItem] = await screen.findAllByTestId('report-history-item')
    fireEvent.click(olderItem)
    fireEvent.click(latestItem)
    older.resolve({ format: 'md', body: '# 过期结果' })

    await waitFor(() => expect((latestItem as HTMLButtonElement).disabled).toBe(true))
    expect(latestItem.textContent).toContain('正在打开…')
    expect(screen.queryByText('过期结果')).toBeNull()

    latest.resolve({ format: 'md', body: '# 最新结果' })
    await screen.findByText('最新结果')
    expect(screen.queryByText('过期结果')).toBeNull()
  })

  it('ignores an older history failure and does not clear the latest opening state', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([
      { name: 'older.md', type: 'weekly', start: '2026-01-01', end: '2026-01-07', createdAt: '2026-01-08T00:00:00Z' },
      { name: 'latest.md', type: 'weekly', start: '2026-01-08', end: '2026-01-14', createdAt: '2026-01-15T00:00:00Z' },
    ])
    const older = Promise.withResolvers<ReportResponse>()
    const latest = Promise.withResolvers<ReportResponse>()
    vi.mocked(getReport)
      .mockReturnValueOnce(older.promise)
      .mockReturnValueOnce(latest.promise)
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    const [olderItem, latestItem] = await screen.findAllByTestId('report-history-item')
    fireEvent.click(olderItem)
    fireEvent.click(latestItem)
    older.reject(new Error('过期失败'))

    await waitFor(() => expect((latestItem as HTMLButtonElement).disabled).toBe(true))
    expect(latestItem.textContent).toContain('正在打开…')
    expect(screen.queryByText('过期失败')).toBeNull()

    latest.resolve({ format: 'md', body: '# 最新结果' })
    await screen.findByText('最新结果')
    expect(screen.queryByTestId('report-error')).toBeNull()
  })

  it('shows an explicit placeholder for an invalid history timestamp', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([
      { name: 'weekly.md', type: 'weekly', start: '2026-01-01', end: '2026-01-07', createdAt: 'not-a-date' },
    ])
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    expect(await screen.findByText('未知时间')).toBeTruthy()
  })

  it('exposes a disabled download state while preparing the file', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(generateReport).mockResolvedValue({ format: 'md', body: '# 可下载' })
    const createObjectURL = vi.fn().mockReturnValue('blob:report')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))
    const download = await screen.findByTestId('report-download')
    fireEvent.click(download)

    expect((download as HTMLButtonElement).disabled).toBe(true)
    expect(download.textContent).toContain('下载中…')
    await waitFor(() => expect((download as HTMLButtonElement).disabled).toBe(false))
    expect(createObjectURL).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:report')
    expect(click).toHaveBeenCalled()
  })

  it('revokes the blob URL even when starting the download throws', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(generateReport).mockResolvedValue({ format: 'html', body: '<html>可下载</html>' })
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn().mockReturnValue('blob:report-error') })
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {
      throw new Error('下载启动失败')
    })
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))
    fireEvent.click(await screen.findByTestId('report-download'))

    expect((await screen.findByTestId('report-error')).textContent).toContain('下载启动失败')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:report-error')
  })

  it('loads the report history list and reloads a past report when clicked', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([
      { name: 'weekly-2026-01-01_2026-01-07-123.md', type: 'weekly', start: '2026-01-01', end: '2026-01-07', createdAt: '2026-01-08T00:00:00Z' },
    ])
    vi.mocked(getReport).mockResolvedValue({ format: 'md', body: '# 历史周报\n旧内容' })
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    await screen.findByTestId('report-generate')
    const item = await screen.findByTestId('report-history-item')
    fireEvent.click(item)

    await waitFor(() => expect(getReport).toHaveBeenCalledWith('weekly-2026-01-01_2026-01-07-123.md'))
    await screen.findByText('历史周报')
  })
  it('cancels report deletion without calling the API', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([
      { name: 'weekly.md', type: 'weekly', start: '2026-01-01', end: '2026-01-07', createdAt: '2026-01-08T00:00:00Z' },
    ])
    vi.mocked(generateReport).mockResolvedValue({ format: 'md', body: '# 待删除', savedName: 'weekly.md' })
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))
    fireEvent.click(await screen.findByTestId('report-delete'))
    expect(screen.getByRole('dialog', { name: '确认删除报告' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    expect(deleteReport).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog', { name: '确认删除报告' })).toBeNull()
    expect(screen.getByText('待删除')).toBeTruthy()
  })

  it('reports when a generated report is missing from history at delete time', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([])
    vi.mocked(generateReport).mockResolvedValue({ format: 'md', body: '# 待删除', savedName: 'missing.md' })
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))
    fireEvent.click(await screen.findByTestId('report-delete'))
    fireEvent.click(screen.getByTestId('report-delete-confirm-btn'))

    expect((await screen.findByTestId('report-error')).textContent).toContain('找不到要删除的历史报告')
    expect(deleteReport).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog', { name: '确认删除报告' })).toBeNull()
  })

  it('deletes a report only after shared-modal confirmation', async () => {
    vi.mocked(fetchChatConfig).mockResolvedValue(readyConfig())
    vi.mocked(listReports).mockResolvedValue([
      { name: 'weekly.md', type: 'weekly', start: '2026-01-01', end: '2026-01-07', createdAt: '2026-01-08T00:00:00Z' },
    ])
    vi.mocked(generateReport).mockResolvedValue({ format: 'md', body: '# 待删除', savedName: 'weekly.md' })
    vi.mocked(deleteReport).mockResolvedValue()
    render(<ReportView workspace="ws1" workspaces={workspaces} />)

    fireEvent.click(await screen.findByTestId('report-generate'))
    fireEvent.click(await screen.findByTestId('report-delete'))
    fireEvent.click(screen.getByTestId('report-delete-confirm-btn'))

    await waitFor(() => expect(deleteReport).toHaveBeenCalledWith('weekly.md'))
    expect(screen.queryByRole('dialog', { name: '确认删除报告' })).toBeNull()
    expect(screen.getByTestId('report-empty-state')).toBeTruthy()
  })

})
