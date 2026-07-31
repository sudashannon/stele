import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { useAppZoom } from './useAppZoom'

afterEach(() => {
  localStorage.clear()
})

describe('useAppZoom', () => {
  it('loads the persisted zoom level on first render', () => {
    localStorage.setItem('stele-zoom', '1.3')

    const { result } = renderHook(() => useAppZoom())

    expect(result.current.zoom).toBe(1.3)
    expect(result.current.zoomPercent).toBe('130%')
  })

  it('falls back to 100% when storage is missing or invalid', () => {
    localStorage.setItem('stele-zoom', '9')

    const { result } = renderHook(() => useAppZoom())

    expect(result.current.zoom).toBe(1)
    expect(result.current.zoomPercent).toBe('100%')
  })

  it('zooms in and out in 10% steps and persists the value', () => {
    const { result } = renderHook(() => useAppZoom())

    act(() => result.current.zoomIn())
    expect(result.current.zoom).toBe(1.1)
    expect(localStorage.getItem('stele-zoom')).toBe('1.1')

    act(() => result.current.zoomOut())
    expect(result.current.zoom).toBe(1)
    expect(localStorage.getItem('stele-zoom')).toBe('1')
  })

  it('clamps zoom between 50% and 200%', () => {
    localStorage.setItem('stele-zoom', '1.9')
    const { result } = renderHook(() => useAppZoom())

    act(() => {
      result.current.zoomIn()
      result.current.zoomIn()
      result.current.zoomIn()
    })
    expect(result.current.zoom).toBe(2)

    act(() => {
      for (let index = 0; index < 20; index += 1) {
        result.current.zoomOut()
      }
    })
    expect(result.current.zoom).toBe(0.5)
  })

  it('resets back to the default zoom', () => {
    localStorage.setItem('stele-zoom', '1.4')
    const { result } = renderHook(() => useAppZoom())

    act(() => result.current.zoomReset())

    expect(result.current.zoom).toBe(1)
    expect(result.current.zoomPercent).toBe('100%')
    expect(localStorage.getItem('stele-zoom')).toBe('1')
  })
})
