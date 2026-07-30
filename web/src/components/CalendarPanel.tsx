import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { copyText } from '../utils/clipboard'
import { TYPE_COLORS } from './graphPalette'
import { useContextMenu } from './ContextMenu'
import { Icon } from './icons'

interface DayItem {
  id: string
  title: string
  type: string
  workspace: string
  path: string
  updatedAt: string
}

interface MonthData {
  year: number
  month: number
  days: Record<string, number>
}

const WEEKDAYS = ['一', '二', '三', '四', '五', '六', '日']
const MONTH_NAMES = ['1月', '2月', '3月', '4月', '5月', '6月', '7月', '8月', '9月', '10月', '11月', '12月']

interface CalendarPanelProps {
  onOpen: (path: string) => void
}

export function CalendarPanel({ onOpen }: CalendarPanelProps) {
  const ctx = useContextMenu()
  const now = new Date()
  const [year, setYear] = useState(now.getFullYear())
  const [quarter, setQuarter] = useState(Math.ceil((now.getMonth() + 1) / 3))
  const [months, setMonths] = useState<MonthData[]>([])
  const [monthsLoading, setMonthsLoading] = useState(true)
  const [monthsError, setMonthsError] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const [items, setItems] = useState<DayItem[]>([])
  const [itemsLoading, setItemsLoading] = useState(false)
  const [itemsError, setItemsError] = useState(false)
  const [copyError, setCopyError] = useState<string | null>(null)
  const monthRequestRef = useRef(0)
  const dayRequestRef = useRef(0)

  const monthsInQuarter = useMemo(
    () => [(quarter - 1) * 3 + 1, (quarter - 1) * 3 + 2, (quarter - 1) * 3 + 3],
    [quarter],
  )

  useEffect(() => {
    const requestId = ++monthRequestRef.current
    setMonthsLoading(true)
    setMonthsError(false)

    Promise.all(
      monthsInQuarter.map(async (month) => {
        const response = await fetch(`/api/wiki/calendar/month?year=${year}&month=${month}`)
        if (!response.ok) throw new Error('month load failed')
        return (await response.json()) as MonthData
      }),
    )
      .then((data) => {
        if (requestId !== monthRequestRef.current) return
        setMonths(data)
        setMonthsError(false)
      })
      .catch(() => {
        if (requestId !== monthRequestRef.current) return
        setMonths([])
        setMonthsError(true)
      })
      .finally(() => {
        if (requestId !== monthRequestRef.current) return
        setMonthsLoading(false)
      })
  }, [monthsInQuarter, year])

  const handleOpen = useCallback(
    (path: string) => {
      onOpen(path)
    },
    [onOpen],
  )
  const handleCopy = useCallback((text: string) => {
    void copyText(text)
      .then(() => setCopyError(null))
      .catch(() => setCopyError('复制失败，请手动复制'))
  }, [])

  const handleSelect = useCallback((date: string) => {
    setSelected(date)
    setItems([])
    setItemsLoading(true)
    setItemsError(false)
    const requestId = ++dayRequestRef.current
    fetch(`/api/wiki/calendar/day?date=${date}`)
      .then((response) => {
        if (!response.ok) throw new Error('day load failed')
        return response.json()
      })
      .then((data) => {
        if (requestId !== dayRequestRef.current) return
        setItems(Array.isArray(data) ? data : [])
      })
      .catch(() => {
        if (requestId !== dayRequestRef.current) return
        setItems([])
        setItemsError(true)
      })
      .finally(() => {
        if (requestId !== dayRequestRef.current) return
        setItemsLoading(false)
      })
  }, [])

  const resetSelection = useCallback(() => {
    dayRequestRef.current += 1
    setSelected(null)
    setItems([])
    setItemsLoading(false)
    setItemsError(false)
  }, [])

  const prevQuarter = useCallback(() => {
    if (quarter === 1) {
      setYear((current) => current - 1)
      setQuarter(4)
    } else {
      setQuarter((current) => current - 1)
    }
    resetSelection()
  }, [quarter, resetSelection])

  const nextQuarter = useCallback(() => {
    if (quarter === 4) {
      setYear((current) => current + 1)
      setQuarter(1)
    } else {
      setQuarter((current) => current + 1)
    }
    resetSelection()
  }, [quarter, resetSelection])

  const groupedItems = useMemo(() => {
    const groups: Record<string, DayItem[]> = {}
    items.forEach((item) => {
      ;(groups[item.workspace] ??= []).push(item)
    })
    return Object.entries(groups).sort(([left], [right]) => left.localeCompare(right))
  }, [items])

  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`

  const renderMonth = (data: MonthData) => {
    const firstDay = new Date(data.year, data.month - 1, 1)
    const lastDay = new Date(data.year, data.month, 0)
    const startDow = (firstDay.getDay() + 6) % 7
    const days: Array<number | null> = []
    for (let i = 0; i < startDow; i += 1) days.push(null)
    for (let day = 1; day <= lastDay.getDate(); day += 1) days.push(day)

    return (
      <div key={data.month} className="border border-[var(--color-border)] bg-[var(--color-surface)] overflow-hidden">
        <div className="border-b border-[var(--color-border)] bg-[var(--color-layer)] px-3 py-2 text-[length:var(--type-body)] font-semibold text-[var(--color-text-primary)]">
          {data.year}年 {MONTH_NAMES[data.month - 1]}
        </div>
        <div className="grid grid-cols-7 border-b border-[var(--color-border-subtle)] px-2 py-1 text-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          {WEEKDAYS.map((weekday) => (
            <span key={weekday}>{weekday}</span>
          ))}
        </div>
        <div className="grid grid-cols-7 px-2 py-2 text-center">
          {days.map((day, index) => {
            if (day === null) return <span key={`empty-${index}`} className="py-2" />
            const dateKey = `${data.year}-${String(data.month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
            const count = data.days[dateKey] ?? 0
            const hasArtifact = count > 0
            const isToday = dateKey === today
            const isSelected = dateKey === selected
            return (
              <button
                key={dateKey}
                type="button"
                onClick={() => handleSelect(dateKey)}
                data-testid={`calendar-day-${dateKey}`}
                aria-label={
                  hasArtifact
                    ? `${data.year}年${data.month}月${day}日，${count} 个产物`
                    : `${data.year}年${data.month}月${day}日，无产物`
                }
                aria-pressed={isSelected}
                title={hasArtifact ? `${count} 个产物` : undefined}
                className={[
                  'relative flex min-h-[44px] items-center justify-center border px-1 py-1 text-[length:var(--type-caption)] transition-colors',
                  isSelected
                    ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
                    : 'border-transparent text-[var(--color-text-primary)] hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)]',
                  isToday && !isSelected ? 'font-semibold text-[var(--color-accent)]' : '',
                ].join(' ')}
              >
                <span>{day}</span>
                {hasArtifact && !isSelected && (
                  <span
                    data-testid={`calendar-dot-${dateKey}`}
                    aria-hidden="true"
                    className="absolute bottom-1 h-1 w-1 rounded-full bg-[var(--color-danger)]"
                  />
                )}
              </button>
            )
          })}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4 p-4">
      <div className="flex items-center justify-between gap-3">
        <button
          type="button"
          onClick={prevQuarter}
          className="inline-flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
        >
          <Icon name="chevron-left" size={14} />
          上一季度
        </button>
        <h2 className="flex items-center gap-2 text-[length:var(--type-heading)] font-semibold text-[var(--color-text-primary)]">
          <Icon name="calendar" size={18} />
          <span>
            {year}年 第{quarter}季度
          </span>
        </h2>
        <button
          type="button"
          onClick={nextQuarter}
          className="inline-flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
        >
          下一季度
          <Icon name="chevron-right" size={14} />
        </button>
      </div>

      {monthsLoading ? (
        <div className="border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-8 text-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          加载日历中…
        </div>
      ) : monthsError ? (
        <div className="border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-8 text-center text-[length:var(--type-caption)] text-[var(--color-danger)]">
          加载日历失败
        </div>
      ) : (
        <div className="grid grid-cols-3 gap-4">{months.map(renderMonth)}</div>
      )}

      {selected ? (
        <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <h3 className="flex items-center gap-2 text-[length:var(--type-body)] font-semibold text-[var(--color-text-primary)]">
              <Icon name="calendar" size={16} />
              <span>{selected}</span>
            </h3>
            <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
              {items.length} 个产物
            </span>
          </div>

          {itemsLoading ? (
            <p className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">加载中…</p>
          ) : itemsError ? (
            <p className="text-[length:var(--type-caption)] text-[var(--color-danger)]">加载当天产物失败</p>
          ) : groupedItems.length === 0 ? (
            <p className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">当天无产物</p>
          ) : (
            <div className="space-y-4">
              {groupedItems.map(([workspace, workspaceItems]) => (
                <section key={workspace} className="space-y-2">
                  <div className="flex items-center justify-between border-b border-[var(--color-border-subtle)] pb-1">
                    <h4 className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)]">
                      {workspace}
                    </h4>
                    <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                      {workspaceItems.length} 项
                    </span>
                  </div>
                  <div className="flex flex-col gap-2">
                    {workspaceItems.map((item) => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => handleOpen(item.path)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter' || event.key === ' ') {
                            event.preventDefault()
                            handleOpen(item.path)
                          }
                        }}
                        onContextMenu={ctx.onContextMenu([
                          { id: 'open', label: '打开', run: () => handleOpen(item.path) },
                          { id: 'copy-path', label: '复制路径', run: () => handleCopy(item.path) },
                          { id: 'copy-title', label: '复制标题', run: () => handleCopy(item.title) },
                        ])}
                        className="flex items-center gap-3 border border-[var(--color-border)] px-3 py-2 text-left text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
                      >
                        <span
                          className="shrink-0 border border-transparent px-2 py-1 text-[length:var(--type-caption)] font-medium text-[var(--color-text-on-color)]"
                          style={{ backgroundColor: TYPE_COLORS[item.type] ?? 'var(--color-text-secondary)' }}
                        >
                          {item.type}
                        </span>
                        <span className="flex-1 truncate font-medium">{item.title}</span>
                        <span className="shrink-0 text-[var(--color-text-secondary)]">打开</span>
                      </button>
                    ))}
                  </div>
                </section>
              ))}
            </div>
          )}
        </div>
      ) : (
        <div className="border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-8 text-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          点击日期查看当天产物
        </div>
      )}
      {copyError && <div role="alert" className="text-center text-[length:var(--type-caption)] text-[var(--color-danger)]">{copyError}</div>}
      {ctx.renderMenu}
    </div>
  )
}
