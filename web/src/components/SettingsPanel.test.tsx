import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { SettingsPanel } from './SettingsPanel'
import { fetchChatConfig, updateChatConfig, fetchChatProviders, fetchSyncConfig, updateSyncConfig, triggerSync } from '../api/client'

vi.mock('../api/client', () => ({
  fetchChatProviders: vi.fn(),
  fetchChatConfig: vi.fn(),
  updateChatConfig: vi.fn(),
  fetchSyncConfig: vi.fn(),
  updateSyncConfig: vi.fn(),
  triggerSync: vi.fn(),
}))

describe('SettingsPanel', () => {
  beforeEach(() => {
    vi.mocked(fetchChatProviders).mockReset()
    vi.mocked(fetchChatConfig).mockReset()
    vi.mocked(updateChatConfig).mockReset()
    vi.mocked(fetchSyncConfig).mockReset()
    vi.mocked(updateSyncConfig).mockReset()
    vi.mocked(triggerSync).mockReset()

    vi.mocked(fetchChatProviders).mockResolvedValue({
      active: 'anthropic',
      providers: [{ name: 'anthropic', models: ['claude-3-5-sonnet', 'claude-3-opus'], supports_images: true }],
    })
    vi.mocked(fetchChatConfig).mockResolvedValue({
      active_provider: 'anthropic',
      providers: {
        anthropic: {
          api_key: 'sk-c****umMM',
          api_base: '',
          model: 'claude-3-5-sonnet',
          temperature: 0.7,
          max_tokens: 4096,
          thinking: 'auto',
        },
      },
    })
    vi.mocked(updateChatConfig).mockResolvedValue({
      active_provider: 'anthropic',
      providers: {
        anthropic: {
          api_key: 'sk-c****umMM',
          api_base: '',
          model: 'claude-3-opus',
          temperature: 0.7,
          max_tokens: 4096,
          thinking: 'auto',
        },
      },
    })
    vi.mocked(fetchSyncConfig).mockResolvedValue({ enabled: true, remote: 'git@github.com:user/repo.git' })
  })

  it('loads and shows provider/model/apiKey fields', async () => {
    render(<SettingsPanel onClose={() => {}} />)

    await waitFor(() => expect(fetchChatProviders).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(fetchChatConfig).toHaveBeenCalledTimes(1))

    expect(screen.getByTestId('chat-settings-panel')).toBeTruthy()
    await waitFor(() => expect((screen.getByTestId('chat-settings-provider') as HTMLSelectElement).value).toBe('anthropic'))
    expect(document.activeElement).toBe(screen.getByTestId('chat-settings-provider'))
    expect((screen.getByTestId('chat-settings-model') as HTMLSelectElement).value).toBe('claude-3-5-sonnet')
    expect((screen.getByTestId('chat-settings-api-key') as HTMLInputElement).placeholder).toBe('sk-c****umMM')
    expect((screen.getByTestId('chat-settings-api-base') as HTMLInputElement).value).toBe('')
    expect((screen.getByTestId('chat-settings-temperature') as HTMLInputElement).value).toBe('0.7')
  })

  it('save calls updateChatConfig and closes on success', async () => {
    const onClose = vi.fn()
    render(<SettingsPanel onClose={onClose} />)

    await waitFor(() => expect(screen.getByTestId('chat-settings-panel')).toBeTruthy())
    await waitFor(() => expect((screen.getByTestId('chat-settings-provider') as HTMLSelectElement).value).toBe('anthropic'))

    fireEvent.change(screen.getByTestId('chat-settings-model'), { target: { value: 'claude-3-opus' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-settings-save'))
    })

    await waitFor(() => expect(updateChatConfig).toHaveBeenCalledTimes(1))
    const patch = vi.mocked(updateChatConfig).mock.calls[0][0]
    expect(patch).toEqual({
      active_provider: 'anthropic',
      providers: {
        anthropic: {
          model: 'claude-3-opus',
          api_base: '',
          temperature: 0.7,
          max_tokens: 4096,
          thinking: 'auto',
        },
      },
    })
    expect(patch.providers?.anthropic).not.toHaveProperty('api_key')
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  it('typing a new api_base sends it in the patch', async () => {
    render(<SettingsPanel onClose={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('chat-settings-panel')).toBeTruthy())
    await waitFor(() => expect((screen.getByTestId('chat-settings-api-base') as HTMLInputElement).value).toBe(''))
    fireEvent.change(screen.getByTestId('chat-settings-api-base'), { target: { value: 'https://api.minimaxi.com' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-settings-save'))
    })
    await waitFor(() => expect(updateChatConfig).toHaveBeenCalledTimes(1))
    const patch = vi.mocked(updateChatConfig).mock.calls[0][0]
    expect(patch.providers?.anthropic?.api_base).toBe('https://api.minimaxi.com')
  })

  it('typing a new api_key sends it in the patch', async () => {
    render(<SettingsPanel onClose={() => {}} />)

    await waitFor(() => expect(screen.getByTestId('chat-settings-panel')).toBeTruthy())
    await waitFor(() => expect((screen.getByTestId('chat-settings-provider') as HTMLSelectElement).value).toBe('anthropic'))

    fireEvent.change(screen.getByTestId('chat-settings-api-key'), { target: { value: 'sk-newkey123' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-settings-save'))
    })

    await waitFor(() => expect(updateChatConfig).toHaveBeenCalledTimes(1))
    const patch = vi.mocked(updateChatConfig).mock.calls[0][0]
    expect(patch.providers?.anthropic?.api_key).toBe('sk-newkey123')
  })

  it('restores each provider saved settings when switching back and forth', async () => {
    vi.mocked(fetchChatProviders).mockResolvedValue({
      active: 'anthropic',
      providers: [
        { name: 'anthropic', models: ['claude-3-5-sonnet', 'claude-3-opus'], supports_images: true },
        { name: 'openai', models: ['gpt-4.1', 'gpt-4o'], supports_images: true },
      ],
    })
    vi.mocked(fetchChatConfig).mockResolvedValue({
      active_provider: 'anthropic',
      providers: {
        anthropic: {
          api_key: 'sk-ant-****',
          api_base: 'https://api.anthropic.test',
          model: 'claude-3-opus',
          temperature: 0.4,
          max_tokens: 2048,
          thinking: 'disabled',
        },
        openai: {
          api_key: 'sk-openai-****',
          api_base: 'https://api.openai.test',
          model: 'gpt-4o',
          temperature: 1.2,
          max_tokens: 8192,
          thinking: 'auto',
        },
      },
    })

    render(<SettingsPanel onClose={() => {}} />)
    const provider = await screen.findByTestId('chat-settings-provider') as HTMLSelectElement
    await waitFor(() => expect(provider.value).toBe('anthropic'))

    fireEvent.change(provider, { target: { value: 'openai' } })
    expect((screen.getByTestId('chat-settings-model') as HTMLSelectElement).value).toBe('gpt-4o')
    expect((screen.getByTestId('chat-settings-api-key') as HTMLInputElement).placeholder).toBe('sk-openai-****')
    expect((screen.getByTestId('chat-settings-api-base') as HTMLInputElement).value).toBe('https://api.openai.test')
    expect((screen.getByTestId('chat-settings-temperature') as HTMLInputElement).value).toBe('1.2')
    expect((screen.getByTestId('chat-settings-max-tokens') as HTMLInputElement).value).toBe('8192')
    expect((screen.getByTestId('chat-settings-thinking') as HTMLSelectElement).value).toBe('auto')

    fireEvent.change(provider, { target: { value: 'anthropic' } })
    expect((screen.getByTestId('chat-settings-model') as HTMLSelectElement).value).toBe('claude-3-opus')
    expect((screen.getByTestId('chat-settings-api-key') as HTMLInputElement).placeholder).toBe('sk-ant-****')
    expect((screen.getByTestId('chat-settings-api-base') as HTMLInputElement).value).toBe('https://api.anthropic.test')
    expect((screen.getByTestId('chat-settings-temperature') as HTMLInputElement).value).toBe('0.4')
    expect((screen.getByTestId('chat-settings-max-tokens') as HTMLInputElement).value).toBe('2048')
    expect((screen.getByTestId('chat-settings-thinking') as HTMLSelectElement).value).toBe('disabled')
  })

  it('does not save empty or out-of-range numeric values', async () => {
    render(<SettingsPanel onClose={() => {}} />)
    await waitFor(() => expect((screen.getByTestId('chat-settings-provider') as HTMLSelectElement).value).toBe('anthropic'))
    const form = screen.getByTestId('chat-settings-save').closest('form')
    expect(form).not.toBeNull()

    fireEvent.change(screen.getByTestId('chat-settings-temperature'), { target: { value: '' } })
    fireEvent.submit(form!)
    expect(updateChatConfig).not.toHaveBeenCalled()
    expect(screen.getByTestId('chat-settings-error').textContent).toContain('有效的数值设置')

    fireEvent.change(screen.getByTestId('chat-settings-temperature'), { target: { value: '3' } })
    fireEvent.change(screen.getByTestId('chat-settings-max-tokens'), { target: { value: '0' } })
    fireEvent.submit(form!)
    expect(updateChatConfig).not.toHaveBeenCalled()
  })

  it('cancel button calls onClose without saving', async () => {
    const onClose = vi.fn()
    render(<SettingsPanel onClose={onClose} />)

    await waitFor(() => expect(screen.getByTestId('chat-settings-panel')).toBeTruthy())
    fireEvent.click(screen.getByTestId('chat-settings-cancel'))

    expect(onClose).toHaveBeenCalledTimes(1)
    expect(updateChatConfig).not.toHaveBeenCalled()
  })

  it('loads and shows the sync remote input', async () => {
    render(<SettingsPanel onClose={() => {}} />)

    await waitFor(() => expect(fetchSyncConfig).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect((screen.getByTestId('sync-remote-input') as HTMLInputElement).value).toBe('git@github.com:user/repo.git'),
    )
  })

  it('saving a new remote calls updateSyncConfig and reflects the result', async () => {
    vi.mocked(updateSyncConfig).mockResolvedValue({ enabled: true, remote: 'git@github.com:user/new-repo.git' })
    render(<SettingsPanel onClose={() => {}} />)

    await waitFor(() => expect((screen.getByTestId('sync-remote-input') as HTMLInputElement).value).toBe('git@github.com:user/repo.git'))

    fireEvent.change(screen.getByTestId('sync-remote-input'), { target: { value: 'git@github.com:user/new-repo.git' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('sync-remote-save'))
    })

    await waitFor(() => expect(updateSyncConfig).toHaveBeenCalledWith('git@github.com:user/new-repo.git'))
    await waitFor(() => expect((screen.getByTestId('sync-remote-input') as HTMLInputElement).value).toBe('git@github.com:user/new-repo.git'))
  })

  it('clicking the sync button triggers sync and shows the result message', async () => {
    vi.mocked(triggerSync).mockResolvedValue({ action: 'pulled', filesChanged: 3, message: '拉取远端更新，还原了 3 个文件' })
    render(<SettingsPanel onClose={() => {}} />)

    await waitFor(() => expect((screen.getByTestId('sync-remote-input') as HTMLInputElement).value).toBe('git@github.com:user/repo.git'))

    await act(async () => {
      fireEvent.click(screen.getByTestId('sync-trigger-button'))
    })

    await waitFor(() => expect(triggerSync).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByTestId('sync-result').textContent).toContain('已拉取'))
    expect(screen.getByTestId('sync-result').textContent).toContain('3 个文件')
  })

  it('shows a sync error when triggerSync rejects', async () => {
    vi.mocked(triggerSync).mockRejectedValue(new Error('triggerSync failed: 500'))
    render(<SettingsPanel onClose={() => {}} />)

    await waitFor(() => expect((screen.getByTestId('sync-remote-input') as HTMLInputElement).value).toBe('git@github.com:user/repo.git'))

    await act(async () => {
      fireEvent.click(screen.getByTestId('sync-trigger-button'))
    })

    await waitFor(() => expect(screen.getByTestId('sync-error').textContent).toContain('triggerSync failed'))
  })
})
