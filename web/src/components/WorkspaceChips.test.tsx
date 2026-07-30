import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { WorkspaceChips } from './WorkspaceChips'

const workspaces = [
  { alias: 'miao', path: '/x/miao', color: '#0063f8' },
  { alias: 'wan2_2_deploy', path: '/x/wan', color: '#16a34a' },
]

describe('WorkspaceChips', () => {
  it('renders an 全部 chip plus one chip per workspace', () => {
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={vi.fn()}
      />,
    )

    expect(screen.getByText('全部')).toBeTruthy()
    expect(screen.getByText('miao')).toBeTruthy()
    expect(screen.getByText('wan2_2_deploy')).toBeTruthy()
  })

  it('calls onSelect with the alias when a chip is clicked, null for 全部', () => {
    const onSelect = vi.fn()
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={onSelect}
        onAdd={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByText('miao'))
    expect(onSelect).toHaveBeenCalledWith('miao')

    fireEvent.click(screen.getByText('全部'))
    expect(onSelect).toHaveBeenCalledWith(null)
  })

  it('renders remove buttons when onRemove is provided', () => {
    const onRemove = vi.fn()
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onRemove={onRemove}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '移除 workspace miao' }))
    expect(onRemove).toHaveBeenCalledWith('miao')
  })

  it('disables remove for locked workspaces', () => {
    const onRemove = vi.fn()
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onRemove={onRemove}
        removeDisabledAliases={['miao']}
      />,
    )

    const button = screen.getByRole('button', { name: '移除 workspace miao' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(button.title).toBe('当前筛选或内容正在使用此 workspace，先切换后再移除')
    fireEvent.click(button)
    expect(onRemove).not.toHaveBeenCalled()
  })

  it('opens an add-workspace form and submits it', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={onAdd}
      />,
    )

    fireEvent.click(screen.getByText('添加 workspace'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'new-ws' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/new' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))

    await waitFor(() =>
      expect(onAdd).toHaveBeenCalledWith(
        expect.objectContaining({ alias: 'new-ws', path: '/x/new' }),
      ),
    )
    await waitFor(() => expect(screen.queryByTestId('add-ws-alias')).toBeNull())
  })

  it('submits the selected Trellis source type', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={onAdd}
      />,
    )

    fireEvent.click(screen.getByText('添加 workspace'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'trellis' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/trellis' } })
    fireEvent.change(screen.getByTestId('add-ws-type'), { target: { value: 'trellis' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))

    await waitFor(() =>
      expect(onAdd).toHaveBeenCalledWith(expect.objectContaining({ type: 'trellis' })),
    )
  })

  it('submits the selected Superpowers source type', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={onAdd}
      />,
    )

    fireEvent.click(screen.getByText('添加 workspace'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'ideas' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/ideas' } })
    fireEvent.change(screen.getByTestId('add-ws-type'), { target: { value: 'superpowers' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))

    await waitFor(() =>
      expect(onAdd).toHaveBeenCalledWith(expect.objectContaining({ type: 'superpowers' })),
    )
  })

  it('disables submit until both alias and path are filled', () => {
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByText('添加 workspace'))
    const submitButton = screen.getByTestId('add-ws-submit') as HTMLButtonElement
    expect(submitButton.disabled).toBe(true)

    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'new-ws' } })
    expect(submitButton.disabled).toBe(true)

    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/new' } })
    expect(submitButton.disabled).toBe(false)
  })

  it('hides the form when 取消 is clicked without calling onAdd', () => {
    const onAdd = vi.fn()
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={onAdd}
      />,
    )

    fireEvent.click(screen.getByText('添加 workspace'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'new-ws' } })
    fireEvent.click(screen.getByText('取消'))

    expect(screen.queryByTestId('add-ws-alias')).toBeNull()
    expect(onAdd).not.toHaveBeenCalled()
  })

  it('shows the inline error and keeps the form open when onAdd rejects', async () => {
    const onAdd = vi.fn().mockRejectedValue(new Error('该路径下未找到 openspec/changes'))
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={onAdd}
      />,
    )

    fireEvent.click(screen.getByText('添加 workspace'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'bad-ws' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/bad' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))

    await waitFor(() =>
      expect(screen.getByTestId('add-ws-error').textContent).toBe('该路径下未找到 openspec/changes'),
    )
    expect((screen.getByTestId('add-ws-alias') as HTMLInputElement).value).toBe('bad-ws')
    expect((screen.getByTestId('add-ws-path') as HTMLInputElement).value).toBe('/x/bad')
  })

  it('closes the form and clears the error when onAdd resolves', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(
      <WorkspaceChips
        workspaces={workspaces}
        active={null}
        onSelect={vi.fn()}
        onAdd={onAdd}
      />,
    )

    fireEvent.click(screen.getByText('添加 workspace'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'good-ws' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/good' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))

    await waitFor(() => expect(screen.queryByTestId('add-ws-alias')).toBeNull())
    expect(screen.queryByTestId('add-ws-error')).toBeNull()
  })
})
