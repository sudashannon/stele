import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ReportView } from './ReportView'
import { fetchChatConfig, generateReport, listReports, getReport } from '../api/client'
import type { ChatConfig, ReportResponse, WorkspaceConfig } from '../api/types'

vi.mock('../api/client', () => ({
  fetchChatConfig: vi.fn(),
  generateReport: vi.fn(),
  listReports: vi.fn(),
  getReport: vi.fn(),
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
    vi.mocked(fetchChatConfig).mockReset()
    vi.mocked(generateReport).mockReset()
    vi.mocked(listReports).mockReset()
    vi.mocked(getReport).mockReset()
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
    expect(screen.getByTestId('report-progress')).toBeTruthy()

    resolve({ format: 'md', body: '# 周报\n内容' })
    await waitFor(() => expect(screen.queryByTestId('report-progress')).toBeNull())
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
    expect(screen.getByTestId('report-coverage').textContent).toContain('词法降级 1')
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
})
