import { useCallback, useEffect, useRef, useState } from 'react'
import type { WatchEvent } from '../api/types'
import { useApp } from '../contexts/AppContext'

const MAX_EVENTS = 200

export function useWatch(collection: string, active: boolean) {
  const { client } = useApp()
  const [events, setEvents] = useState<WatchEvent[]>([])
  const [watching, setWatching] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const unsubRef = useRef<(() => void) | null>(null)

  const stop = useCallback(() => {
    unsubRef.current?.()
    unsubRef.current = null
    setWatching(false)
  }, [])

  const start = useCallback(() => {
    if (unsubRef.current) return  // already watching
    setError(null)
    setWatching(true)
    unsubRef.current = client.watch(
      collection,
      (event) => {
        setEvents((prev) => {
          const next = [event, ...prev]
          return next.length > MAX_EVENTS ? next.slice(0, MAX_EVENTS) : next
        })
      },
      (err) => {
        setError(err.message)
        setWatching(false)
        unsubRef.current = null
      },
    )
  }, [client, collection])

  // Auto-start when active, stop when inactive or unmounted
  useEffect(() => {
    if (active) start()
    return stop
  }, [active, start, stop])

  const clear = useCallback(() => setEvents([]), [])

  return { events, watching, error, stop, start, clear }
}
