import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import type { FormEvent } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { SearchableCombobox } from './SearchableCombobox'

const options = [
  { value: 'alpha', label: 'Alpha document' },
  { value: 'beta', label: 'Beta document' },
]

function ControlledCombobox({ initialValue = '' }: { initialValue?: string }) {
  const [value, setValue] = useState(initialValue)
  return (
    <SearchableCombobox
      options={options}
      value={value}
      onChange={setValue}
      placeholder="选择文档"
    />
  )
}

describe('SearchableCombobox query synchronization', () => {
  it('shows the chosen label when a selection is submitted and reopened', () => {
    render(<ControlledCombobox />)
    const input = screen.getByRole('combobox') as HTMLInputElement
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: 'beta' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(input.value).toBe('Beta document')
    expect(input.getAttribute('aria-expanded')).toBe('false')
    fireEvent.focus(input)
    expect(input.value).toBe('Beta document')
  })

  it('restores the selected label on Escape instead of retaining a search', () => {
    render(<ControlledCombobox initialValue="alpha" />)
    const input = screen.getByRole('combobox') as HTMLInputElement
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: 'beta' } })
    fireEvent.keyDown(input, { key: 'Escape' })

    expect(input.value).toBe('Alpha document')
    expect(input.getAttribute('aria-expanded')).toBe('false')
  })

  it('tracks external value updates and clears the query when selection is removed', () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <SearchableCombobox options={options} value="alpha" onChange={onChange} placeholder="选择文档" />,
    )
    const input = screen.getByRole('combobox') as HTMLInputElement
    expect(input.value).toBe('Alpha document')

    rerender(<SearchableCombobox options={options} value="beta" onChange={onChange} placeholder="选择文档" />)
    expect(input.value).toBe('Beta document')

    rerender(<SearchableCombobox options={options} value="" onChange={onChange} placeholder="选择文档" />)
    expect(input.value).toBe('')
  })

  it('prevents Enter submission and only chooses an open valid option', () => {
    const onSubmit = vi.fn((event: FormEvent) => event.preventDefault())
    const onChange = vi.fn()
    render(
      <form onSubmit={onSubmit}>
        <SearchableCombobox options={options} value="" onChange={onChange} placeholder="选择文档" />
      </form>,
    )
    const input = screen.getByRole('combobox')

    expect(fireEvent.keyDown(input, { key: 'Enter' })).toBe(false)
    expect(onSubmit).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()

    fireEvent.focus(input)
    fireEvent.keyDown(input, { key: 'End' })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onChange).toHaveBeenCalledWith('beta')
  })

  it('supports Home and End and clamps the active option when results shrink', () => {
    const onChange = vi.fn()
    const expandedOptions = [
      ...options,
      { value: 'gamma', label: 'Gamma document' },
    ]
    const { rerender } = render(
      <SearchableCombobox options={expandedOptions} value="" onChange={onChange} placeholder="选择文档" />,
    )
    const input = screen.getByRole('combobox')
    fireEvent.focus(input)
    fireEvent.keyDown(input, { key: 'End' })
    expect(input.getAttribute('aria-activedescendant')).toContain('option-2')

    rerender(<SearchableCombobox options={options} value="" onChange={onChange} placeholder="选择文档" />)
    expect(input.getAttribute('aria-activedescendant')).toContain('option-1')

    fireEvent.keyDown(input, { key: 'Home' })
    expect(input.getAttribute('aria-activedescendant')).toContain('option-0')
  })

  it('closes on Tab and restores the selected label without blocking focus navigation', () => {
    render(<ControlledCombobox initialValue="alpha" />)
    const input = screen.getByRole('combobox') as HTMLInputElement
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: 'beta' } })

    expect(fireEvent.keyDown(input, { key: 'Tab' })).toBe(true)
    expect(input.value).toBe('Alpha document')
    expect(input.getAttribute('aria-expanded')).toBe('false')
  })
})
