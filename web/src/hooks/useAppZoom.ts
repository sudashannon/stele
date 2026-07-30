import { useCallback, useEffect, useMemo, useState } from 'react'

const ZOOM_KEY = 'comet-panel-zoom'
const MIN_ZOOM = 0.5
const MAX_ZOOM = 2.0
const DEFAULT_ZOOM = 1.0
const STEP = 0.1

function persistZoom(value: number) {
  try {
    localStorage.setItem(ZOOM_KEY, String(value))
  } catch {
    // ignore storage failures
  }
}

function loadZoom(): number {
  try {
    const value = localStorage.getItem(ZOOM_KEY)
    if (value) {
      const parsed = parseFloat(value)
      if (parsed >= MIN_ZOOM && parsed <= MAX_ZOOM) return parsed
    }
  } catch {
    // ignore storage failures
  }
  return DEFAULT_ZOOM
}

export interface UseAppZoomReturn {
  zoom: number
  zoomIn: () => void
  zoomOut: () => void
  zoomReset: () => void
  zoomPercent: string
}

export function useAppZoom(): UseAppZoomReturn {
  const [zoom, setZoom] = useState<number>(() => loadZoom())

  useEffect(() => {
    persistZoom(zoom)
  }, [zoom])

  const zoomIn = useCallback(() => {
    setZoom((previous) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, +(previous + STEP).toFixed(1))))
  }, [])

  const zoomOut = useCallback(() => {
    setZoom((previous) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, +(previous - STEP).toFixed(1))))
  }, [])

  const zoomReset = useCallback(() => {
    setZoom(DEFAULT_ZOOM)
  }, [])

  const zoomPercent = useMemo(() => `${Math.round(zoom * 100)}%`, [zoom])

  return { zoom, zoomIn, zoomOut, zoomReset, zoomPercent }
}
