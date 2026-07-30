import { useEffect, useId, useMemo, useState } from 'react'

export interface SearchableComboboxOption {
  value: string
  label: string
  description?: string
  group?: string
  keywords?: string
}

export interface SearchableComboboxProps {
  options: SearchableComboboxOption[]
  value: string
  onChange: (value: string) => void
  placeholder: string
  ariaLabel?: string
  emptyText?: string
  disabled?: boolean
  maxResults?: number
  'data-testid'?: string
}

export function SearchableCombobox({
  options,
  value,
  onChange,
  placeholder,
  ariaLabel = placeholder,
  emptyText = '没有匹配项',
  disabled = false,
  maxResults = 20,
  'data-testid': testId,
}: SearchableComboboxProps) {
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const listboxId = useId()

  const selectedLabel = options.find((option) => option.value === value)?.label ?? ''

  const results = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase()
    return options
      .filter((option) => !normalized || [
        option.label,
        option.description,
        option.group,
        option.keywords,
      ].some((field) => field?.toLocaleLowerCase().includes(normalized)))
      .slice(0, maxResults)
  }, [maxResults, options, query])

  useEffect(() => {
    setQuery(selectedLabel)
  }, [selectedLabel])

  useEffect(() => {
    setActiveIndex(0)
  }, [query])

  useEffect(() => {
    setActiveIndex((index) => Math.min(index, Math.max(results.length - 1, 0)))
  }, [options.length, results.length])

  const choose = (option: SearchableComboboxOption) => {
    onChange(option.value)
    setQuery(option.label)
    setOpen(false)
  }

  return (
    <div
      className="relative"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) {
          setQuery(selectedLabel)
          setOpen(false)
        }
      }}
    >
      <input
        data-testid={testId}
        role="combobox"
        aria-label={ariaLabel}
        aria-expanded={open}
        aria-controls={listboxId}
        aria-autocomplete="list"
        aria-activedescendant={open && results[activeIndex] ? `${listboxId}-option-${activeIndex}` : undefined}
        disabled={disabled}
        value={query}
        onFocus={() => setOpen(true)}
        onChange={(event) => {
          setQuery(event.target.value)
          setOpen(true)
        }}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault()
            setOpen(true)
            setActiveIndex((index) => Math.min(index + 1, Math.max(0, results.length - 1)))
          } else if (event.key === 'ArrowUp') {
            event.preventDefault()
            setOpen(true)
            setActiveIndex((index) => Math.max(0, index - 1))
          } else if (event.key === 'Home') {
            event.preventDefault()
            setOpen(true)
            setActiveIndex(0)
          } else if (event.key === 'End') {
            event.preventDefault()
            setOpen(true)
            setActiveIndex(Math.max(results.length - 1, 0))
          } else if (event.key === 'Enter') {
            event.preventDefault()
            if (open && results[activeIndex]) choose(results[activeIndex])
          } else if (event.key === 'Tab') {
            setQuery(selectedLabel)
            setOpen(false)
          } else if (event.key === 'Escape') {
            event.preventDefault()
            setQuery(selectedLabel)
            setOpen(false)
          }
        }}
        placeholder={placeholder}
        className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-xs text-[var(--color-text-primary)] focus:border-[var(--color-accent)] focus:outline-none disabled:opacity-50"
      />
      {open && (
        <div
          id={listboxId}
          role="listbox"
          className="absolute z-20 mt-1 max-h-64 w-full overflow-y-auto border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-overlay)]"
        >
          {results.length === 0 ? (
            <div className="px-2 py-2 text-xs text-[var(--color-text-tertiary)]">{emptyText}</div>
          ) : (
            [...new Set(results.map((option) => option.group ?? ''))].map((group) => (
              <div key={group || '__ungrouped'}>
                {group && (
                  <div className="border-b border-[var(--color-border-subtle)] bg-[var(--color-bg)] px-2 py-1 text-xs font-semibold text-[var(--color-text-secondary)]">
                    {group}
                  </div>
                )}
                {results.map((option, index) => option.group === (group || undefined) && (
                  <button
                    key={option.value}
                    id={`${listboxId}-option-${index}`}
                    role="option"
                    aria-selected={index === activeIndex}
                    type="button"
                    tabIndex={-1}
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => setActiveIndex(index)}
                    onClick={() => choose(option)}
                    className={`block w-full px-2 py-1.5 text-left text-xs ${
                      index === activeIndex
                        ? 'bg-[var(--palette-highlight)] text-[var(--color-text-primary)]'
                        : 'text-[var(--color-text-secondary)]'
                    }`}
                  >
                    <span className="block truncate">{option.label}</span>
                    {option.description && (
                      <span className="block truncate text-[var(--color-text-tertiary)]">{option.description}</span>
                    )}
                  </button>
                ))}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}
