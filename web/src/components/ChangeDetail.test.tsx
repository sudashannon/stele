import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { ChangeDetail } from './ChangeDetail'
import type { ChangeSummary } from '../api/types'

afterEach(() => vi.restoreAllMocks())

describe('ChangeDetail', () => {
  it('renders stepper, donut, and review badges for the given change', async () => {
    // BacklinksPanel and ArtifactList both fetch on mount; mock fetch so this
    // test stays hermetic and doesn't emit act() warnings from a real/unmocked
    // network call resolving after the test's synchronous assertions run
    // (same pattern as client.test.ts / BacklinksPanel.test.tsx). Branches by
    // URL since the two panels hit different endpoints with different
    // response shapes (/api/wiki/component/... vs /api/changes/...).
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return {
          ok: true,
          json: async () => ({ name: 'rx101-x', phases: [] }),
        } as Response
      }
      return {
        ok: true,
        json: async () => ({ component: {}, forward: [], backlinks: [] }),
      } as Response
    })

    const change: ChangeSummary = {
      name: 'rx101-x', workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 19, tasksTotal: 31, verifyResult: 'pending', createdAt: '2026-05-29',
      artifacts: {}, visualized: true, designReviewed: true, verifyReviewed: false,
      verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
    }
    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={() => {}} />)
    expect(screen.getByTestId('step-build').dataset.state).toBe('current')
    expect(screen.getByTestId('donut-fraction').textContent).toBe('19/31 任务完成')
    expect(screen.getByTestId('badge-visualized').dataset.tone).toBe('ok')

    // Flush BacklinksPanel's pending fetch-driven state update inside act()
    // before the test (and RTL's auto-cleanup/unmount) completes.
    await waitFor(() => {})
  })

  it('disables the guard button when tasks are incomplete at build phase', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return { ok: true, json: async () => ({ name: 'rx101-x', phases: [] }) } as Response
      }
      return { ok: true, json: async () => ({ component: {}, forward: [], backlinks: [] }) } as Response
    })

    const change: ChangeSummary = {
      name: 'rx101-x', workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 9, tasksTotal: 72, verifyResult: 'pending', createdAt: '2026-05-29',
      artifacts: {}, visualized: true, designReviewed: true, verifyReviewed: false,
      verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
    }
    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={() => {}} />)
    const trigger = screen.getByTestId('guard-trigger') as HTMLButtonElement
    expect(trigger.disabled).toBe(true)
    expect(trigger.title).toBe('任务未全部完成 (9/72)，无法进入验证')

    await waitFor(() => {})
  })

  it('calls onOpenArtifact (not its own viewer) when an artifact button is clicked', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return {
          ok: true,
          json: async () => ({
            name: 'rx101-x',
            phases: [
              {
                key: 'design',
                label: '设计',
                artifacts: [{ file: 'design.md', label: '设计文档', exists: true, path: '/x/rx101-x/design.md' }],
              },
            ],
          }),
        } as Response
      }
      return { ok: true, json: async () => ({ component: {}, forward: [], backlinks: [] }) } as Response
    })

    const change: ChangeSummary = {
      name: 'rx101-x', workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 19, tasksTotal: 31, verifyResult: 'pending', createdAt: '2026-05-29',
      artifacts: {}, visualized: true, designReviewed: true, verifyReviewed: false,
      verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
    }
    const onOpenArtifact = vi.fn()
    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={onOpenArtifact} />)

    const artifactButton = await screen.findByText('设计文档')
    artifactButton.click()

    expect(onOpenArtifact).toHaveBeenCalledWith('/x/rx101-x/design.md')
    // ChangeDetail no longer owns a viewer of its own.
    expect(screen.queryByRole('region', { name: 'design.md' })).toBeNull()

    await waitFor(() => {})
  })

  it('calls onArtifactsChanged with the flattened, existing-only artifact list for the current change', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return {
          ok: true,
          json: async () => ({
            name: 'rx101-x',
            phases: [
              {
                key: 'design',
                label: '设计',
                artifacts: [
                  { file: 'design.md', label: '设计文档', exists: true, path: '/x/rx101-x/design.md' },
                  { file: 'proposal.md', label: '提案', exists: false },
                ],
              },
              {
                key: 'build',
                label: '构建',
                artifacts: [{ file: 'tasks.md', label: '任务清单', exists: true, path: '/x/rx101-x/tasks.md' }],
              },
            ],
          }),
        } as Response
      }
      return { ok: true, json: async () => ({ component: {}, forward: [], backlinks: [] }) } as Response
    })

    const change: ChangeSummary = {
      name: 'rx101-x', workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 19, tasksTotal: 31, verifyResult: 'pending', createdAt: '2026-05-29',
      artifacts: {}, visualized: true, designReviewed: true, verifyReviewed: false,
      verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
    }
    const onArtifactsChanged = vi.fn()
    render(
      <ChangeDetail
        change={change}
        onChangeUpdated={() => {}}
        onOpenArtifact={() => {}}
        onArtifactsChanged={onArtifactsChanged}
      />,
    )

    await waitFor(() =>
      expect(onArtifactsChanged).toHaveBeenCalledWith([
        { path: '/x/rx101-x/design.md', label: '设计文档' },
        { path: '/x/rx101-x/tasks.md', label: '任务清单' },
      ]),
    )
  })

  it('renders Trellis lifecycle and backend-provided transition metadata', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return { ok: true, json: async () => ({ name: '07-26-beta', phases: [] }) } as Response
      }
      return { ok: true, json: async () => ({ component: {}, forward: [], backlinks: [] }) } as Response
    })
    const change: ChangeSummary = {
      name: '07-26-beta',
      title: 'Beta Task',
      sourceType: 'trellis',
      workflow: 'trellis',
      phase: 'in_progress',
      archived: false,
      tasksCompleted: 1,
      tasksTotal: 2,
      verifyResult: 'pending',
      createdAt: '2026-07-26',
      artifacts: {},
      visualized: false,
      designReviewed: false,
      verifyReviewed: false,
      verifiedAt: '',
      buildMode: '',
      reviewMode: '',
      tddMode: '',
      autoTransition: false,
      lifecycle: [
        { key: 'planning', label: '规划' },
        { key: 'in_progress', label: '执行' },
        { key: 'completed', label: '完成' },
      ],
      nextTransition: {
        target: 'completed',
        label: '完成并归档',
        command: 'python3 .trellis/scripts/task.py archive 07-26-beta --no-commit',
        blockedReason: '验收项未全部完成 (1/2)，无法归档',
      },
    }
    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={() => {}} />)
    expect(screen.getByTestId('step-in_progress').dataset.state).toBe('current')
    expect((screen.getByTestId('guard-trigger') as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByTestId('guard-trigger').textContent).toContain('完成并归档')
    expect(screen.queryByTestId('badge-visualized')).toBeNull()
    await waitFor(() => {})
  })
  it('renders standalone Superpowers as read-only without OpenSpec review controls, with todo button showing count', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return { ok: true, json: async () => ({ name: 'cache-redesign', phases: [] }) } as Response
      }
      return { ok: true, json: async () => ({ component: {}, forward: [], backlinks: [] }) } as Response
    })
    const change: ChangeSummary = {
      name: 'cache-redesign',
      title: 'Cache Redesign',
      workspace: 'superpowers-ws',
      sourceType: 'superpowers',
      workflow: 'superpowers',
      phase: 'design',
      archived: false,
      tasksCompleted: 0,
      tasksTotal: 0,
      verifyResult: 'pending',
      createdAt: '2026-07-26',
      artifacts: {},
      visualized: false,
      designReviewed: false,
      verifyReviewed: false,
      verifiedAt: '',
      buildMode: '',
      reviewMode: '',
      tddMode: '',
      autoTransition: false,
      lifecycle: [
        { key: 'design', label: '设计' },
        { key: 'plan', label: '计划' },
        { key: 'build', label: '执行' },
        { key: 'verify', label: '验证' },
        { key: 'completed', label: '完成' },
      ],
    }
    const onNavigateToTodos = vi.fn()
    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={() => {}} onNavigateToTodos={onNavigateToTodos} todoCount={3} />)
    expect(screen.getByTestId('step-design').dataset.state).toBe('current')
    expect(screen.queryByTestId('guard-trigger')).toBeNull()
    expect(screen.queryByTestId('badge-visualized')).toBeNull()
    const todoBtn = screen.getByTestId('change-todo-action')
    expect(todoBtn.textContent).toBe('待办 3')
    await waitFor(() => {})
  })

  it('calls onNavigateToTodos with workspace and change name when the todo action button is clicked', async () => {
    const onNavigateToTodos = vi.fn()
    const change: ChangeSummary = {
      name: 'super-change',
      title: 'Super Change',
      workspace: 'superpowers-ws',
      sourceType: 'superpowers',
      workflow: 'superpowers', phase: 'planning', archived: false,
      tasksCompleted: 10, tasksTotal: 20, verifyResult: 'pending', createdAt: '',
      artifacts: {}, visualized: false, designReviewed: false, verifyReviewed: false,
      verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
      lifecycle: [{ key: 'planning', label: '规划' }],
    }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return { ok: true, json: async () => ({ name: 'super-change', workflow: 'superpowers', phase: 'planning', phases: [] }) } as Response
      }
      return { ok: true, json: async () => ({ component: { id: '', title: '' }, forward: [], backlinks: [] }) } as Response
    })
    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={() => {}} onNavigateToTodos={onNavigateToTodos} />)
    const btn = screen.getByTestId('change-todo-action')
    fireEvent.click(btn)
    expect(onNavigateToTodos).toHaveBeenCalledWith('superpowers-ws', 'super-change')
    await waitFor(() => {})
  })

  it('renders workflow, source, mode, auto-transition, verification result, and local verified time metadata when present', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return { ok: true, json: async () => ({ name: 'metadata-change', phases: [] }) } as Response
      }
      return { ok: true, json: async () => ({ component: {}, forward: [], backlinks: [] }) } as Response
    })
    const verifiedAt = '2026-07-30T08:15:00Z'
    const change: ChangeSummary = {
      name: 'metadata-change',
      workspace: 'product',
      sourceType: 'openspec',
      workflow: 'full',
      phase: 'verify',
      archived: false,
      tasksCompleted: 4,
      tasksTotal: 4,
      verifyResult: 'pass',
      createdAt: '2026-07-29',
      artifacts: {},
      buildMode: 'subagent-driven-development',
      reviewMode: 'standard',
      tddMode: 'tdd',
      autoTransition: false,
      verifiedAt,
    }

    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={() => {}} />)

    expect(screen.getByTestId('metadata-workflow').textContent).toContain('full')
    expect(screen.getByTestId('metadata-source').textContent).toContain('OpenSpec')
    expect(screen.getByTestId('metadata-build-mode').textContent).toContain('subagent-driven-development')
    expect(screen.getByTestId('metadata-review-mode').textContent).toContain('standard')
    expect(screen.getByTestId('metadata-tdd-mode').textContent).toContain('tdd')
    expect(screen.getByTestId('metadata-auto-transition').textContent).toContain('已关闭')
    expect(screen.getByTestId('metadata-verified-at').textContent).toContain(new Date(verifiedAt).toLocaleString())
    expect(screen.getByTestId('change-verify-result').textContent).toContain('已通过')
    await waitFor(() => {})
  })

  it('omits optional mode, source, auto-transition, and verified-time metadata when absent', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/api/changes/')) {
        return { ok: true, json: async () => ({ name: 'legacy-change', phases: [] }) } as Response
      }
      return { ok: true, json: async () => ({ component: {}, forward: [], backlinks: [] }) } as Response
    })
    const change: ChangeSummary = {
      name: 'legacy-change',
      workflow: 'full',
      phase: 'open',
      archived: false,
      tasksCompleted: 0,
      tasksTotal: 0,
      verifyResult: 'backend_specific_unknown',
      createdAt: '',
      artifacts: {},
    }

    render(<ChangeDetail change={change} onChangeUpdated={() => {}} onOpenArtifact={() => {}} />)

    expect(screen.queryByTestId('metadata-source')).toBeNull()
    expect(screen.queryByTestId('metadata-build-mode')).toBeNull()
    expect(screen.queryByTestId('metadata-review-mode')).toBeNull()
    expect(screen.queryByTestId('metadata-tdd-mode')).toBeNull()
    expect(screen.queryByTestId('metadata-auto-transition')).toBeNull()
    expect(screen.queryByTestId('metadata-verified-at')).toBeNull()
    expect(screen.getByTestId('change-verify-result').textContent).toContain('未知')
    expect(screen.getByTestId('change-verify-result').textContent).not.toContain('backend_specific_unknown')
    await waitFor(() => {})
  })
})
