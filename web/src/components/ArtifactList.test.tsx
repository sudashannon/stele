import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { ArtifactList } from './ArtifactList'

afterEach(() => vi.restoreAllMocks())

describe('ArtifactList', () => {
  it('renders an existing artifact as clickable and a missing one as non-clickable', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        name: 'rx101-x',
        workflow: 'full',
        phase: 'design',
        archived: false,
        tasksCompleted: 0,
        tasksTotal: 0,
        verifyResult: 'pending',
        createdAt: '2026-05-29',
        phases: [
          {
            key: 'design',
            label: '2. Design',
            status: 'current',
            artifacts: [
              { file: 'design_doc', label: 'design doc', exists: true, path: '/x/design.md' },
              { file: 'handoff', label: 'handoff/context', exists: false },
            ],
          },
        ],
      }),
    } as Response)

    const onSelectArtifact = vi.fn()
    render(<ArtifactList changeName="rx101-x" onSelectArtifact={onSelectArtifact} />)

    const existing = await screen.findByRole('button', { name: 'design doc' })
    expect(existing).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'handoff/context' })).toBeNull()
    const missing = screen.getByText('handoff/context')
    expect(missing.className).toContain('min-w-0')
    expect(missing.className).toContain('flex-1')
    expect(missing.className).toContain('truncate')
    expect(screen.getByText('未生成').className).toContain('shrink-0')

    existing.click()
    expect(onSelectArtifact).toHaveBeenCalledWith('/x/design.md')

    await waitFor(() => {})
  })

  it('hides phases that have no existing artifacts', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        name: 'rx101-x',
        workflow: 'full',
        phase: 'open',
        archived: false,
        tasksCompleted: 0,
        tasksTotal: 0,
        verifyResult: 'pending',
        createdAt: '2026-05-29',
        phases: [
          {
            key: 'open',
            label: '1. Open',
            status: 'done',
            artifacts: [{ file: 'proposal.md', label: 'proposal.md', exists: false }],
          },
          {
            key: 'design',
            label: '2. Design',
            status: 'current',
            artifacts: [{ file: 'design_doc', label: 'design doc', exists: true, path: '/x/design.md' }],
          },
        ],
      }),
    } as Response)

    render(<ArtifactList changeName="rx101-x" onSelectArtifact={vi.fn()} />)

    await screen.findByText('2. Design')
    expect(screen.queryByText('1. Open')).toBeNull()
  })

  it('forwards the workspace prop through fetchChangeDetail to the request URL', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        name: 'rx101-x',
        workflow: 'full',
        phase: 'design',
        archived: false,
        tasksCompleted: 0,
        tasksTotal: 0,
        verifyResult: 'pending',
        createdAt: '2026-05-29',
        phases: [],
      }),
    } as Response)

    render(<ArtifactList changeName="rx101-x" workspace="rx101" onSelectArtifact={vi.fn()} />)

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith('/api/changes/rx101-x?workspace=rx101'))
  })

  it('shows a text-and-icon error state when artifact loading fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
    } as Response)

    render(<ArtifactList changeName="broken-change" onSelectArtifact={vi.fn()} />)

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('产出物加载失败')
  })
})
