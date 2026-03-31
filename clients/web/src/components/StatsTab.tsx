import { useCallback, useEffect, useState } from 'react'
import type { CollectionStats } from '../api/types'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props { collection: string }

function formatBytes(bytes: string): string {
  const n = parseInt(bytes, 10)
  if (isNaN(n)) return bytes
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

export default function StatsTab({ collection }: Props) {
  const { client } = useApp()
  const { showToast } = useToast()
  const [stats, setStats] = useState<CollectionStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setStats(await client.collectionStats(collection))
      setLastRefresh(new Date())
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to load stats')
    } finally {
      setLoading(false)
    }
  }, [client, collection, showToast])

  useEffect(() => {
    void load()
    const id = setInterval(() => void load(), 30_000)
    return () => clearInterval(id)
  }, [load])

  const dirtyPct =
    stats && parseInt(stats.record_count, 10) > 0
      ? (parseInt(stats.dirty_entries, 10) / parseInt(stats.record_count, 10)) * 100
      : 0

  return (
    <div className="flex flex-col gap-4 max-w-xl">
      <div className="flex items-center gap-3">
        <button
          onClick={() => void load()}
          disabled={loading}
          className="text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-50 px-3 py-1.5 text-gray-300 transition-colors"
        >
          {loading ? 'Refreshing…' : '↻ Refresh'}
        </button>
        {lastRefresh && (
          <span className="text-xs text-gray-600">
            Last refreshed: {lastRefresh.toLocaleTimeString()}
          </span>
        )}
      </div>

      {stats && (
        <div className="grid grid-cols-2 gap-4">
          <div className="rounded-lg bg-gray-800 border border-gray-700 p-4">
            <p className="text-xs text-gray-500 mb-1">Records</p>
            <p className="text-2xl font-bold text-gray-100">{parseInt(stats.record_count, 10).toLocaleString()}</p>
          </div>
          <div className="rounded-lg bg-gray-800 border border-gray-700 p-4">
            <p className="text-xs text-gray-500 mb-1">Segments</p>
            <p className="text-2xl font-bold text-gray-100">{parseInt(stats.segment_count, 10).toLocaleString()}</p>
          </div>
          <div className="rounded-lg bg-gray-800 border border-gray-700 p-4">
            <p className="text-xs text-gray-500 mb-1">Dirty Entries</p>
            <p className={`text-2xl font-bold ${dirtyPct > 20 ? 'text-amber-400' : 'text-gray-100'}`}>
              {parseInt(stats.dirty_entries, 10).toLocaleString()}
            </p>
            {parseInt(stats.record_count, 10) > 0 && (
              <div className="mt-2">
                <div className="h-1.5 rounded-full bg-gray-700 overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${dirtyPct > 20 ? 'bg-amber-500' : 'bg-blue-500'}`}
                    style={{ width: `${Math.min(100, dirtyPct).toFixed(1)}%` }}
                  />
                </div>
                <p className="text-xs text-gray-600 mt-1">{dirtyPct.toFixed(1)}% of records</p>
              </div>
            )}
          </div>
          <div className="rounded-lg bg-gray-800 border border-gray-700 p-4">
            <p className="text-xs text-gray-500 mb-1">Size on Disk</p>
            <p className="text-2xl font-bold text-gray-100">{formatBytes(stats.size_bytes)}</p>
          </div>
        </div>
      )}
    </div>
  )
}
