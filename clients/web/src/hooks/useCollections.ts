import { useCallback, useEffect, useState } from 'react'
import { useApp } from '../contexts/AppContext'

export interface CollectionItem {
  name: string
}

export function useCollections() {
  const { client, connected } = useApp()
  const [collections, setCollections] = useState<CollectionItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!connected) return
    setLoading(true)
    setError(null)
    try {
      const names = await client.listCollections()
      setCollections(names.map((name) => ({ name })))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load collections')
    } finally {
      setLoading(false)
    }
  }, [client, connected])

  useEffect(() => {
    void refresh()
  }, [refresh])

  return { collections, loading, error, refresh }
}
