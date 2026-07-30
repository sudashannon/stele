
import { useEffect, useId, useMemo, useState } from 'react'
import {
  fetchChatConfig,
  updateChatConfig,
  fetchChatProviders,
  fetchSyncConfig,
  updateSyncConfig,
  triggerSync,
} from '../api/client'
import type { ChatProviderConfig, ChatProviderInfo } from '../api/types'
import { Icon } from './icons'

// 独立的 provider 设置面板：原先内嵌在 ChatBubble 里，现抽出为独立组件，
// 由 App 通过统一 Modal 包裹。这里只负责弹层内容，不再自行嵌套 modal。
export function SettingsPanel({ onClose }: { onClose: () => void }) {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [providers, setProviders] = useState<ChatProviderInfo[]>([])
  const [providerConfigs, setProviderConfigs] = useState<Record<string, ChatProviderConfig>>({})
  const [providerName, setProviderName] = useState('')
  const [model, setModel] = useState('')
  const [apiKeyPlaceholder, setApiKeyPlaceholder] = useState('')
  const [apiKeyInput, setApiKeyInput] = useState('')
  const [temperatureInput, setTemperatureInput] = useState('0.7')
  const [maxTokensInput, setMaxTokensInput] = useState('4096')
  const [thinking, setThinking] = useState('auto')
  const [apiBase, setApiBase] = useState('')
  const [syncRemote, setSyncRemote] = useState('')
  const [syncRemoteInput, setSyncRemoteInput] = useState('')
  const [syncSaving, setSyncSaving] = useState(false)
  const [syncError, setSyncError] = useState('')
  const [syncing, setSyncing] = useState(false)
  const [syncResultMsg, setSyncResultMsg] = useState('')

  const providerId = useId()
  const modelId = useId()
  const apiBaseId = useId()
  const apiKeyId = useId()
  const temperatureId = useId()
  const maxTokensId = useId()
  const thinkingId = useId()
  const syncRemoteId = useId()
  const errorId = useId()
  const syncErrorId = useId()
  const saveLabel = saving ? '保存中…' : '保存'

  useEffect(() => {
    let cancelled = false
    setError('')
    setLoading(true)

    Promise.all([fetchChatProviders(), fetchChatConfig()])
      .then(([providersResp, config]) => {
        if (cancelled) return
        setProviders(providersResp.providers ?? [])
        setProviderConfigs(config.providers ?? {})
        const active = config.active_provider || providersResp.active
        const activeConfig = config.providers?.[active]
        setProviderName(active)
        setModel(activeConfig?.model ?? providersResp.providers?.find((provider) => provider.name === active)?.models?.[0] ?? '')
        setApiKeyPlaceholder(activeConfig?.api_key ?? '')
        setApiBase(activeConfig?.api_base ?? '')
        setApiKeyInput('')
        setTemperatureInput(String(activeConfig?.temperature ?? 0.7))
        setMaxTokensInput(String(activeConfig?.max_tokens ?? 4096))
        setThinking(activeConfig?.thinking ?? 'auto')
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    fetchSyncConfig()
      .then((cfg) => {
        if (cancelled) return
        setSyncRemote(cfg.remote)
        setSyncRemoteInput(cfg.remote)
      })
      .catch((err) => {
        if (!cancelled) setSyncError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      cancelled = true
    }
  }, [])

  const selectedProvider = useMemo(
    () => providers.find((provider) => provider.name === providerName) ?? null,
    [providers, providerName],
  )

  function handleProviderChange(name: string) {
    setProviderName(name)
    const saved = providerConfigs[name]
    const info = providers.find((provider) => provider.name === name)
    setModel(saved?.model ?? info?.models?.[0] ?? '')
    setApiKeyPlaceholder(saved?.api_key ?? '')
    setApiBase(saved?.api_base ?? '')
    setApiKeyInput('')
    setTemperatureInput(String(saved?.temperature ?? 0.7))
    setMaxTokensInput(String(saved?.max_tokens ?? 4096))
    setThinking(saved?.thinking ?? 'auto')
  }

  async function handleSaveSettings(event?: React.FormEvent) {
    event?.preventDefault()
    setError('')

    const temperature = Number(temperatureInput)
    const maxTokens = Number(maxTokensInput)
    if (
      temperatureInput.trim() === ''
      || !Number.isFinite(temperature)
      || temperature < 0
      || temperature > 2
      || maxTokensInput.trim() === ''
      || !Number.isFinite(maxTokens)
      || !Number.isInteger(maxTokens)
      || maxTokens < 1
    ) {
      setError('请填写有效的数值设置：Temperature 需在 0–2 之间，Max Tokens 需为不小于 1 的整数。')
      return
    }

    setSaving(true)
    try {
      const patch: Parameters<typeof updateChatConfig>[0] = {
        active_provider: providerName,
        providers: {
          [providerName]: {
            api_base: apiBase,
            model,
            temperature,
            max_tokens: maxTokens,
            thinking,
            ...(apiKeyInput.trim() ? { api_key: apiKeyInput.trim() } : {}),
          },
        },
      }
      await updateChatConfig(patch)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  async function handleSaveSyncRemote() {
    setSyncError('')
    setSyncSaving(true)
    try {
      const cfg = await updateSyncConfig(syncRemoteInput.trim())
      setSyncRemote(cfg.remote)
      setSyncRemoteInput(cfg.remote)
    } catch (err) {
      setSyncError(err instanceof Error ? err.message : String(err))
    } finally {
      setSyncSaving(false)
    }
  }

  async function handleTriggerSync() {
    setSyncError('')
    setSyncResultMsg('')
    setSyncing(true)
    try {
      const result = await triggerSync()
      const actionLabel: Record<string, string> = {
        pushed: '已推送',
        pulled: '已拉取',
        merged: '已合并',
        'up-to-date': '已是最新',
        error: '同步失败',
      }
      const label = actionLabel[result.action] ?? result.action
      const suffix = result.filesChanged > 0 ? `（${result.filesChanged} 个文件变更）` : ''
      setSyncResultMsg(`${label}${suffix}${result.message ? ' - ' + result.message : ''}`)
    } catch (err) {
      setSyncError(err instanceof Error ? err.message : String(err))
    } finally {
      setSyncing(false)
    }
  }

  return (
    <div data-testid="chat-settings-panel" className="space-y-4 p-4 text-sm">
      {loading ? (
        <div className="flex items-center gap-2 text-[var(--color-text-secondary)]" role="status">
          <Icon name="spinner" className="animate-spin" />
          <span>加载中…</span>
        </div>
      ) : (
        <>
          <form className="space-y-4" onSubmit={handleSaveSettings} aria-describedby={error ? errorId : undefined}>
            <section className="space-y-3">
              <div>
                <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">聊天 Provider</h3>
                <p className="mt-1 text-[var(--type-caption)] text-[var(--color-text-secondary)]">
                  这些设置只影响当前面板发起的聊天与摘要能力。
                </p>
              </div>

              <label className="block" htmlFor={providerId}>
                <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">Provider</span>
                <select
                  autoFocus
                  id={providerId}
                  data-testid="chat-settings-provider"
                  value={providerName}
                  onChange={(e) => handleProviderChange(e.target.value)}
                  className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-sm"
                >
                  {providers.map((provider) => (
                    <option key={provider.name} value={provider.name}>
                      {provider.name}
                    </option>
                  ))}
                </select>
              </label>

              <label className="block" htmlFor={modelId}>
                <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">Model</span>
                <select
                  id={modelId}
                  data-testid="chat-settings-model"
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-sm"
                >
                  {(selectedProvider?.models ?? []).map((providerModel) => (
                    <option key={providerModel} value={providerModel}>
                      {providerModel}
                    </option>
                  ))}
                </select>
              </label>

              <label className="block" htmlFor={apiBaseId}>
                <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">API Base</span>
                <input
                  id={apiBaseId}
                  data-testid="chat-settings-api-base"
                  type="text"
                  value={apiBase}
                  onChange={(e) => setApiBase(e.target.value)}
                  placeholder="https://api.minimaxi.com"
                  className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] p-2 font-mono text-sm"
                />
              </label>

              <label className="block" htmlFor={apiKeyId}>
                <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">API Key</span>
                <input
                  id={apiKeyId}
                  data-testid="chat-settings-api-key"
                  type="password"
                  value={apiKeyInput}
                  onChange={(e) => setApiKeyInput(e.target.value)}
                  placeholder={apiKeyPlaceholder || '未配置'}
                  className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-sm"
                />
              </label>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <label className="block" htmlFor={temperatureId}>
                  <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">Temperature</span>
                  <input
                    id={temperatureId}
                    data-testid="chat-settings-temperature"
                    type="number"
                    step="0.1"
                    min="0"
                    max="2"
                    value={temperatureInput}
                    onChange={(e) => setTemperatureInput(e.target.value)}
                    className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-sm"
                  />
                </label>
                <label className="block" htmlFor={maxTokensId}>
                  <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">Max Tokens</span>
                  <input
                    id={maxTokensId}
                    data-testid="chat-settings-max-tokens"
                    type="number"
                    min="1"
                    value={maxTokensInput}
                    onChange={(e) => setMaxTokensInput(e.target.value)}
                    className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-sm"
                  />
                </label>
              </div>

              <label className="block" htmlFor={thinkingId}>
                <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">Thinking</span>
                <select
                  id={thinkingId}
                  data-testid="chat-settings-thinking"
                  value={thinking}
                  onChange={(e) => setThinking(e.target.value)}
                  className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-sm"
                >
                  <option value="auto">auto</option>
                  <option value="disabled">disabled</option>
                </select>
              </label>
            </section>

            {error && (
              <div data-testid="chat-settings-error" id={errorId} role="alert" className="text-[var(--type-caption)] text-[var(--color-danger)]">
                {error}
              </div>
            )}

            <div className="flex justify-end gap-2 border-t border-[var(--color-border)] pt-3">
              <button
                type="button"
                data-testid="chat-settings-cancel"
                onClick={onClose}
                disabled={saving}
                className="px-3 py-2 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:opacity-50"
              >
                取消
              </button>
              <button
                type="submit"
                data-testid="chat-settings-save"
                disabled={saving}
                className="border border-[var(--color-accent)] bg-[var(--color-accent)] px-3 py-2 text-sm text-[var(--color-text-on-color)] disabled:cursor-not-allowed disabled:opacity-50"
              >
                {saveLabel}
              </button>
            </div>
          </form>

          <section className="space-y-3 border-t border-[var(--color-border)] pt-4" aria-describedby={syncError ? syncErrorId : undefined}>
            <div>
              <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">知识库同步</h3>
              <p className="mt-1 text-[var(--type-caption)] text-[var(--color-text-secondary)]">
                保存远端后可手动同步知识库索引与文档副本。
              </p>
            </div>

            <label className="block" htmlFor={syncRemoteId}>
              <span className="mb-1 block text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)]">Git Remote URL</span>
              <div className="flex gap-2">
                <input
                  id={syncRemoteId}
                  data-testid="sync-remote-input"
                  type="text"
                  value={syncRemoteInput}
                  onChange={(e) => setSyncRemoteInput(e.target.value)}
                  placeholder="git@github.com:user/repo.git"
                  className="flex-1 border border-[var(--color-border)] bg-[var(--color-surface)] p-2 font-mono text-sm"
                />
                <button
                  type="button"
                  data-testid="sync-remote-save"
                  onClick={handleSaveSyncRemote}
                  disabled={syncSaving || syncRemoteInput.trim() === syncRemote}
                  className="border border-[var(--color-border)] px-3 py-2 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {syncSaving ? '保存中…' : '保存'}
                </button>
              </div>
            </label>

            {syncError && (
              <div data-testid="sync-error" id={syncErrorId} role="alert" className="text-[var(--type-caption)] text-[var(--color-danger)]">
                {syncError}
              </div>
            )}

            <button
              type="button"
              data-testid="sync-trigger-button"
              onClick={handleTriggerSync}
              disabled={syncing}
              className="flex w-full items-center justify-center gap-2 border border-[var(--color-border)] bg-[var(--color-layer)] px-3 py-2 text-sm text-[var(--color-text-primary)] disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Icon name="refresh" />
              <span>{syncing ? '同步中…' : '同步知识库'}</span>
            </button>

            {syncResultMsg && (
              <div data-testid="sync-result" role="status" className="text-[var(--type-caption)] text-[var(--color-text-secondary)]">
                {syncResultMsg}
              </div>
            )}
          </section>
        </>
      )}
    </div>
  )
}
