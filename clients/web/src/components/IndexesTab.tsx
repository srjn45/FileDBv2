import { useCallback, useEffect, useState } from 'react'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props { collection: string }

export default function IndexesTab({ collection }: Props) {
  const { client } = useApp()
  const { showToast } = useToast()
  const [fields, setFields] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [newField, setNewField] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setFields(await client.listIndexes(collection))
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to load indexes')
    } finally {
      setLoading(false)
    }
  }, [client, collection, showToast])

  useEffect(() => {
    void load()
  }, [load])

  async function handleEnsure(e: React.FormEvent) {
    e.preventDefault()
    const field = newField.trim()
    if (!field) return
    try {
      await client.ensureIndex(collection, field)
      showToast('success', `Index on "${field}" ensured`)
      setNewField('')
      await load()
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to ensure index')
    }
  }

  async function handleDrop(field: string) {
    if (!window.confirm(`Drop index on "${field}"?`)) return
    try {
      await client.dropIndex(collection, field)
      showToast('success', `Index on "${field}" dropped`)
      await load()
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to drop index')
    }
  }

  return (
    <div className="flex flex-col gap-4 max-w-lg">
      <p className="text-xs text-gray-500">
        Indexed fields accelerate eq-filter queries from O(n) → O(1)
      </p>

      {/* Ensure index form */}
      <form onSubmit={(e) => void handleEnsure(e)} className="flex gap-2">
        <input
          type="text"
          value={newField}
          onChange={(e) => setNewField(e.target.value)}
          placeholder="field name"
          className="flex-1 rounded bg-gray-800 border border-gray-700 px-3 py-1.5 text-sm text-gray-100 placeholder-gray-600 focus:outline-none focus:border-blue-500"
        />
        <button
          type="submit"
          className="rounded bg-blue-600 hover:bg-blue-500 px-4 py-1.5 text-sm font-medium text-white transition-colors"
        >
          Ensure Index
        </button>
      </form>

      {/* Index list */}
      {loading && <p className="text-xs text-gray-500">Loading…</p>}
      {fields.length === 0 && !loading && (
        <p className="text-xs text-gray-600">No secondary indexes</p>
      )}
      <ul className="flex flex-col gap-1">
        {fields.map((field) => (
          <li
            key={field}
            className="flex items-center justify-between rounded bg-gray-800 border border-gray-700 px-3 py-2"
          >
            <span className="text-sm text-gray-200 font-mono">{field}</span>
            <button
              onClick={() => void handleDrop(field)}
              className="text-xs text-red-500 hover:text-red-400 transition-colors"
            >
              Drop
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
