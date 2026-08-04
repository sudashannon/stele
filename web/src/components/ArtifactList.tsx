import { useEffect, useState } from 'react'
import { fetchChangeDetail } from '../api/client'
import type { ArtifactInfo, PhaseInfo } from '../api/types'
import { Icon } from './icons'
import { StateBlock } from './StateBlock'
import { phaseColorToken } from './phasePalette'


interface Props {
  changeName: string
  workspace?: string
  onSelectArtifact: (path: string) => void
}


function ArtifactRow({ artifact, onSelectArtifact }: { artifact: ArtifactInfo; onSelectArtifact: (path: string) => void }) {
  return (
    <div className="flex items-center gap-2 py-1 text-[length:var(--type-caption)]">
      <span
        data-testid={`artifact-dot-${artifact.file}`}
        className={artifact.exists ? 'text-[var(--color-success-text)]' : 'text-[var(--color-text-tertiary)]'}
      >
        <Icon name={artifact.exists ? 'check' : 'info'} size={14} />
      </span>
      {artifact.exists && artifact.path ? (
        <button
          type="button"
          onClick={() => onSelectArtifact(artifact.path!)}
          className="truncate text-left text-[length:var(--type-caption)] font-medium text-[var(--color-link)] hover:underline"
        >
          {artifact.label}
        </button>
      ) : (
        <>
          <span className="min-w-0 flex-1 truncate text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{artifact.label}</span>
          <span className="shrink-0 text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">未生成</span>
        </>
      )}
    </div>
  )
}

function PhaseSection({ phase, onSelectArtifact }: { phase: PhaseInfo; onSelectArtifact: (path: string) => void }) {
  return (
    <details
      data-testid={`artifact-phase-${phase.key}`}
      open
      className="border-l-2 pl-2"
      style={{ borderColor: phaseColorToken(phase.key) }}
    >
      <summary className="cursor-pointer select-none text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)]">
        {phase.label}
        {phase.status && <span className="ml-2 font-normal text-[var(--color-text-secondary)]">{phase.status}</span>}
      </summary>
      <div className="mt-1 pl-2">
        {phase.artifacts.map((artifact) => (
          <ArtifactRow key={artifact.file} artifact={artifact} onSelectArtifact={onSelectArtifact} />
        ))}
      </div>
    </details>
  )
}

export function ArtifactList({ changeName, workspace, onSelectArtifact }: Props) {
  const [phases, setPhases] = useState<PhaseInfo[] | null>(null)
  const [loadError, setLoadError] = useState(false)

  useEffect(() => {
    let cancelled = false
    setPhases(null)
    setLoadError(false)
    fetchChangeDetail(changeName, workspace)
      .then((detail) => {
        if (!cancelled) setPhases(detail.phases ?? [])
      })
      .catch(() => {
        if (!cancelled) {
          setPhases([])
          setLoadError(true)
        }
      })
    return () => {
      cancelled = true
    }
  }, [changeName, workspace])

  if (phases === null) {
    return <StateBlock kind="loading" title="正在加载产出物" compact />
  }

  if (loadError) {
    return <StateBlock kind="error" title="产出物加载失败" compact />
  }

  const visiblePhases = phases.filter((p) => p.artifacts.some((a) => a.exists))

  if (visiblePhases.length === 0) {
    return <StateBlock kind="empty" title="暂无产出物" compact />
  }

  return (
    <div className="space-y-2">
      {visiblePhases.map((phase) => (
        <PhaseSection key={phase.key} phase={phase} onSelectArtifact={onSelectArtifact} />
      ))}
    </div>
  )
}
