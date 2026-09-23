import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { WorkflowWork } from '../api/types'
import { WorkflowWorksList } from './WorkflowWorksList'

afterEach(() => vi.restoreAllMocks())

describe('WorkflowWorksList', () => {
  it('renders worktree Markdown as a standalone document without wiki-only context', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      text: async () => '# Session content\n\nA real worktree artifact.',
    } as Response)
    const work: WorkflowWork = {
      name: 'workflow item',
      paths: ['/tmp/worktree'],
      branch: 'feature/workflow',
      kind: 'repo',
      worktrees: 1,
      idleDays: 0,
      goal: 'Read real workflow artifacts',
      current: 'Inspect session',
      dirty: 0,
      artifacts: [{ path: 'SESSION.md', kind: 'index', worktree: '/tmp/worktree' }],
      sessions: [],
      notes: [],
      state: 'active',
      mergeState: '',
      merged: false,
      next: '',
      workspace: '',
    }

    render(<WorkflowWorksList data={{ enabled: true, works: [work] }} works={[work]} />)
    fireEvent.click(screen.getByRole('button', { name: 'workflow item' }))
    fireEvent.click(screen.getByRole('button', { name: /SESSION\.md/ }))

    expect(await screen.findByRole('heading', { name: 'Session content' })).toBeTruthy()
    expect(screen.queryByTestId('markdown-context-rail')).toBeNull()
    expect(screen.queryByText('引用加载失败，请稍后重试')).toBeNull()
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })
})
