export function TaskDonut({ completed, total }: { completed: number; total: number }) {
  const pct = total > 0 ? Math.round((completed / total) * 100) : 0
  const color = pct >= 100 ? 'var(--color-success)' : 'var(--color-accent)'

  return (
    <div className="flex flex-col items-center justify-center">
      <div
        data-testid="donut-ring"
        className="w-[120px] h-[120px] rounded-full flex items-center justify-center"
        style={{ background: `conic-gradient(${color} 0% ${pct}%, var(--color-layer-accent) ${pct}% 100%)` }}
      >
        <div className="w-[88px] h-[88px] rounded-full bg-[var(--color-surface)] flex items-center justify-center">
          <div data-testid="donut-percent" className="text-2xl font-bold">
            {pct}%
          </div>
        </div>
      </div>
      <div data-testid="donut-fraction" className="mt-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
        {completed}/{total} 任务完成
      </div>
    </div>
  )
}
