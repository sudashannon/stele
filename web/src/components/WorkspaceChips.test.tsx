import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { WorkspaceChips } from './WorkspaceChips'

const workspaces = [
  { alias: 'miao', path: '/x/miao', color: '#0063f8' },
  { alias: 'wan2_2_deploy', path: '/x/wan', color: '#16a34a' },
]

describe('WorkspaceChips', () => {
  it('renders an "全部" chip plus one chip per workspace', () => {
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={vi.fn()} />)
    expect(screen.getByText('全部')).toBeTruthy()
    expect(screen.getByText('miao')).toBeTruthy()
    expect(screen.getByText('wan2_2_deploy')).toBeTruthy()
  })

  it('calls onSelect with the alias when a chip is clicked, null for 全部', () => {
    const onSelect = vi.fn()
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={onSelect} onAdd={vi.fn()} />)
    fireEvent.click(screen.getByText('miao'))
    expect(onSelect).toHaveBeenCalledWith('miao')
    fireEvent.click(screen.getByText('全部'))
    expect(onSelect).toHaveBeenCalledWith(null)
  })

  it('opens an add-workspace form and submits it', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={onAdd} />)
    fireEvent.click(screen.getByText('+ 添加'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'new-ws' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/new' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))
    expect(onAdd).toHaveBeenCalledWith(expect.objectContaining({ alias: 'new-ws', path: '/x/new' }))
  })


  it('submits the selected Trellis source type', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={onAdd} />)
    fireEvent.click(screen.getByText('+ 添加'))
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
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={onAdd} />)
    fireEvent.click(screen.getByText('+ 添加'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'ideas' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/ideas' } })
    fireEvent.change(screen.getByTestId('add-ws-type'), { target: { value: 'superpowers' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))
    await waitFor(() =>
      expect(onAdd).toHaveBeenCalledWith(expect.objectContaining({ type: 'superpowers' })),
    )
  })

  it('disables submit until both alias and path are filled', () => {
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={vi.fn()} />)
    fireEvent.click(screen.getByText('+ 添加'))
    const submitBtn = screen.getByTestId('add-ws-submit') as HTMLButtonElement
    expect(submitBtn.disabled).toBe(true)
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'new-ws' } })
    expect(submitBtn.disabled).toBe(true)
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/new' } })
    expect(submitBtn.disabled).toBe(false)
  })

  it('hides the form when 取消 is clicked without calling onAdd', () => {
    const onAdd = vi.fn()
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={onAdd} />)
    fireEvent.click(screen.getByText('+ 添加'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'new-ws' } })
    fireEvent.click(screen.getByText('取消'))
    expect(screen.queryByTestId('add-ws-alias')).toBeNull()
    expect(onAdd).not.toHaveBeenCalled()
  })

  it('shows the inline error and keeps the form open (with values retained) when onAdd rejects', async () => {
    const onAdd = vi.fn().mockRejectedValue(new Error('该路径下未找到 openspec/changes'))
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={onAdd} />)
    fireEvent.click(screen.getByText('+ 添加'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'bad-ws' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/bad' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))

    await waitFor(() =>
      expect(screen.getByTestId('add-ws-error').textContent).toBe('该路径下未找到 openspec/changes'),
    )
    // Form stays open with the user's input retained, not cleared/closed.
    expect((screen.getByTestId('add-ws-alias') as HTMLInputElement).value).toBe('bad-ws')
    expect((screen.getByTestId('add-ws-path') as HTMLInputElement).value).toBe('/x/bad')
  })

  it('closes the form and shows no error when onAdd resolves', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(<WorkspaceChips workspaces={workspaces} active={null} onSelect={vi.fn()} onAdd={onAdd} />)
    fireEvent.click(screen.getByText('+ 添加'))
    fireEvent.change(screen.getByTestId('add-ws-alias'), { target: { value: 'good-ws' } })
    fireEvent.change(screen.getByTestId('add-ws-path'), { target: { value: '/x/good' } })
    fireEvent.click(screen.getByTestId('add-ws-submit'))

    await waitFor(() => expect(screen.queryByTestId('add-ws-alias')).toBeNull())
    expect(screen.queryByTestId('add-ws-error')).toBeNull()
  })
})
