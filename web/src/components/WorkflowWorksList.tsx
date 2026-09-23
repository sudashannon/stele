import { lazy, Suspense, useRef, useState } from 'react'
import { fetchWorkflowArtifactContent } from '../api/client'
import type { WorkflowArtifact, WorkflowWork, WorkflowWorksResponse } from '../api/types'

const MarkdownViewer = lazy(() => import('./MarkdownViewer').then((module) => ({ default: module.MarkdownViewer })))

interface Props {
  data: WorkflowWorksResponse | null
  works: WorkflowWork[]
}

const stateGroups = [
  { key: 'active', label: '进行中' },
  { key: 'paused', label: '已暂停' },
  { key: 'delivered', label: '已交付' },
  { key: 'merged', label: '已合并' },
  { key: 'archived', label: '已归档' },
  { key: 'abandoned', label: '已放弃' },
  { key: 'undeclared', label: '未声明' },
]

function stateKey(state: string): string {
  return state === '-' || state === '' ? 'undeclared' : state
}

function stateLabel(state: string): string {
  return stateGroups.find((group) => group.key === stateKey(state))?.label ?? state
}

function groupWorks(works: WorkflowWork[]) {
  const groups = stateGroups
    .map((group) => ({ ...group, works: works.filter((work) => stateKey(work.state) === group.key) }))
    .filter((group) => group.works.length > 0)
  const other = works.filter((work) => !stateGroups.some((group) => group.key === stateKey(work.state)))
  if (other.length > 0) groups.push({ key: 'other', label: '其他状态', works: other })
  return groups
}

function artifactLabel(kind: WorkflowArtifact['kind']): string {
  return kind === 'index' ? 'SESSION.md' : kind === 'session' ? '会话日志' : 'Notes'
}

export function WorkflowWorksList({ data, works }: Props) {
  const [selectedWork, setSelectedWork] = useState<WorkflowWork | null>(null)
  const [activeArtifact, setActiveArtifact] = useState<WorkflowArtifact | null>(null)
  const [artifactContent, setArtifactContent] = useState<string | null>(null)
  const [artifactError, setArtifactError] = useState<string | null>(null)
  const [artifactLoading, setArtifactLoading] = useState(false)
  const artifactRequest = useRef(0)

  function openWork(work: WorkflowWork) {
    artifactRequest.current++
    setSelectedWork(work)
    setActiveArtifact(null)
    setArtifactContent(null)
    setArtifactError(null)
    setArtifactLoading(false)
  }

  async function openArtifact(artifact: WorkflowArtifact) {
    const request = ++artifactRequest.current
    setActiveArtifact(artifact)
    setArtifactContent(null)
    setArtifactError(null)
    setArtifactLoading(true)
    try {
      const content = await fetchWorkflowArtifactContent(artifact.worktree, artifact.path)
      if (request === artifactRequest.current) setArtifactContent(content)
    } catch (error) {
      if (request === artifactRequest.current) setArtifactError(error instanceof Error ? error.message : '产物读取失败')
    } finally {
      if (request === artifactRequest.current) setArtifactLoading(false)
    }
  }
  function closeWork() {
    artifactRequest.current++
    setSelectedWork(null)
    setActiveArtifact(null)
    setArtifactContent(null)
    setArtifactError(null)
    setArtifactLoading(false)
  }

  function closeArtifact() {
    artifactRequest.current++
    setActiveArtifact(null)
    setArtifactContent(null)
    setArtifactError(null)
    setArtifactLoading(false)
  }

  if (activeArtifact && selectedWork) {
    return (
      <section aria-label="Workflow 产物详情" className="space-y-3">
        <div className="flex items-center gap-3 border-b border-[var(--color-border)] pb-2">
          <button type="button" onClick={closeArtifact} className="text-[length:var(--type-caption)] text-[var(--color-accent)] hover:underline">← 返回 work</button>
          <span className="truncate font-[var(--font-mono)] text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{activeArtifact.path}</span>
        </div>
        {artifactLoading && <p role="status" className="py-6 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">正在读取产物…</p>}
        {artifactError && <p role="alert" className="py-6 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{artifactError}</p>}
        {artifactContent !== null && (
          <Suspense fallback={<p role="status" className="py-6 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">正在打开查看器…</p>}>
            <MarkdownViewer path={null} body={artifactContent} onClose={closeArtifact} />
          </Suspense>
        )}
      </section>
    )
  }

  if (selectedWork) {
    const work = selectedWork
    const artifactsByKind = (kind: WorkflowArtifact['kind']) => work.artifacts.filter((artifact) => artifact.kind === kind)
    return (
      <section aria-label="Workflow 工作详情" className="space-y-4">
        <div className="flex items-center gap-3 border-b border-[var(--color-border)] pb-3">
          <button type="button" onClick={closeWork} className="text-[length:var(--type-caption)] text-[var(--color-accent)] hover:underline">← 返回列表</button>
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-[length:var(--type-body)] font-semibold text-[var(--color-text-primary)]" title={work.name}>{work.name}</h2>
            <p className="truncate font-[var(--font-mono)] text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">{work.branch || '—'}</p>
          </div>
          <span className="shrink-0 rounded border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{stateLabel(work.state)}</span>
        </div>

        <dl className="grid grid-cols-2 gap-3 md:grid-cols-4">
          {[
            ['工作区', work.workspace || '未注册'],
            ['空闲', work.idleDays == null ? '—' : `${work.idleDays} 天`],
            ['未提交改动', String(work.dirty)],
            ['Worktree', String(work.worktrees)],
            ['合并状态', work.mergeState || '未声明'],
            ['类型', work.kind || '—'],
          ].map(([label, value]) => (
            <div key={label} className="border border-[var(--color-border-subtle)] bg-[var(--color-surface)] p-3">
              <dt className="text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">{label}</dt>
              <dd className="mt-1 break-words text-[length:var(--type-caption)] text-[var(--color-text-primary)]">{value}</dd>
            </div>
          ))}
        </dl>

        <div className="grid gap-3 md:grid-cols-3">
          {[
            ['目标', work.goal],
            ['当前', work.current],
            ['下一步', work.next],
          ].map(([label, value]) => (
            <section key={label} className="border border-[var(--color-border-subtle)] bg-[var(--color-surface)] p-3">
              <h3 className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">{label}</h3>
              <p className="mt-1 whitespace-pre-wrap text-[length:var(--type-caption)] text-[var(--color-text-primary)]">{value || '—'}</p>
            </section>
          ))}
        </div>

        <section className="space-y-2">
          <div>
            <h3 className="text-[length:var(--type-body)] font-semibold text-[var(--color-text-primary)]">真实工作流产物</h3>
            <p className="text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">读取该 worktree 中的 SESSION.md、会话日志与 notes 文件。</p>
          </div>
          {(['index', 'session', 'note'] as const).map((kind) => {
            const artifacts = artifactsByKind(kind)
            if (artifacts.length === 0) return null
            return (
              <div key={kind} className="border border-[var(--color-border-subtle)] bg-[var(--color-surface)]">
                <h4 className="border-b border-[var(--color-border-subtle)] px-3 py-2 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">{artifactLabel(kind)}</h4>
                <ul className="divide-y divide-[var(--color-border-subtle)]">
                  {artifacts.map((artifact) => (
                    <li key={`${artifact.worktree}:${artifact.path}`}>
                      <button type="button" onClick={() => void openArtifact(artifact)} className="w-full px-3 py-2 text-left text-[length:var(--type-caption)] text-[var(--color-accent)] hover:bg-[var(--color-hover)]">
                        <span className="break-all">{artifact.path}</span>
                        <span className="ml-2 text-[var(--color-text-tertiary)]">{artifact.worktree}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )
          })}
          {work.artifacts.length === 0 && <p className="border border-[var(--color-border-subtle)] bg-[var(--color-surface)] px-3 py-4 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">没有可预览的工作流产物。</p>}
        </section>
      </section>
    )
  }

  const groupedWorks = groupWorks(works)
  return (
    <section aria-label="Workflow 工作" className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-[length:var(--type-body)] font-semibold text-[var(--color-text-primary)]">Workflow 工作</h2>
        <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{works.length} 个 work</span>
      </div>

      {data === null ? (
        <p role="status" className="bg-[var(--color-surface)] px-3 py-4 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">正在读取工作列表…</p>
      ) : !data.enabled ? (
        <p role="status" className="bg-[var(--color-surface)] px-3 py-4 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{data.error || '工作列表暂不可用'}</p>
      ) : works.length === 0 ? (
        <p className="bg-[var(--color-surface)] px-3 py-4 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">当前工作区没有带 SESSION.md 的 work</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse bg-[var(--color-surface)]">
            <thead className="sticky top-0 z-10">
              <tr>
                {['Work / 分支', '工作区', '目标 / 当前', '状态', '闲置', '改动', 'Worktree'].map((label) => (
                  <th key={label} scope="col" className="h-7 whitespace-nowrap border-b border-[var(--color-border)] bg-[var(--color-layer)] px-3 text-left text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">{label}</th>
                ))}
              </tr>
            </thead>
            {groupedWorks.map((group) => (
              <tbody key={group.key}>
                <tr>
                  <th colSpan={7} className="border-b border-[var(--color-border)] bg-[var(--color-layer)] px-3 py-2 text-left text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
                    {group.label}<span className="ml-2 font-normal text-[var(--color-text-tertiary)]">{group.works.length}</span>
                  </th>
                </tr>
                {group.works.map((work) => (
                  <tr key={`${work.branch}:${work.paths[0] ?? work.name}`} onClick={() => openWork(work)} className="cursor-pointer hover:bg-[var(--color-hover)]">
                    <td className="border-b border-[var(--color-border-subtle)] px-3 py-2 align-top">
                      <button type="button" onClick={() => openWork(work)} className="block max-w-full truncate text-left text-[length:var(--type-caption)] font-medium text-[var(--color-accent)] hover:underline" title={work.name}>{work.name}</button>
                      <div className="truncate font-[var(--font-mono)] text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]" title={work.branch}>{work.branch || '—'}</div>
                    </td>
                    <td className="border-b border-[var(--color-border-subtle)] px-3 py-2 align-top text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{work.workspace || '未注册'}</td>
                    <td className="border-b border-[var(--color-border-subtle)] px-3 py-2 align-top text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                      <div className="truncate" title={work.goal}>{work.goal || '—'}</div>
                      <div className="truncate text-[var(--color-text-tertiary)]" title={work.current}>{work.current || '—'}</div>
                    </td>
                    <td className="border-b border-[var(--color-border-subtle)] px-3 py-2 align-top text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{stateLabel(work.state)}</td>
                    <td className="border-b border-[var(--color-border-subtle)] px-3 py-2 align-top text-right text-[length:var(--type-caption)] tabular-nums text-[var(--color-text-secondary)]">{work.idleDays == null ? '—' : `${work.idleDays} 天`}</td>
                    <td className="border-b border-[var(--color-border-subtle)] px-3 py-2 align-top text-right text-[length:var(--type-caption)] tabular-nums text-[var(--color-text-secondary)]">{work.dirty}</td>
                    <td className="border-b border-[var(--color-border-subtle)] px-3 py-2 align-top text-right text-[length:var(--type-caption)] tabular-nums text-[var(--color-text-secondary)]">{work.worktrees}</td>
                  </tr>
                ))}
              </tbody>
            ))}
          </table>
        </div>
      )}
    </section>
  )
}
