import { useState } from 'react'
import { useCollections } from '../hooks/useCollections'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props {
  activeCollection: string | null
  onSelect: (name: string) => void
}

export default function Sidebar({ activeCollection, onSelect }: Props) {
  const { client } = useApp()
  const { showToast } = useToast()
  const { collections, loading, error, refresh } = useCollections()
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [nameError, setNameError] = useState('')

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const name = newName.trim()
    if (!name) return
    if (!/^[a-zA-Z0-9_]+$/.test(name)) {
      setNameError('Only letters, digits, and underscores')
      return
    }
    try {
      await client.createCollection(name)
      showToast('success', `Collection "${name}" created`)
      setNewName('')
      setCreating(false)
      setNameError('')
      await refresh()
      onSelect(name)
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to create collection')
    }
  }

  return (
    <aside className="w-48 shrink-0 bg-gray-900 border-r border-gray-800 flex flex-col">
      <div className="px-3 pt-3 pb-1 text-xs font-semibold text-gray-500 uppercase tracking-wider">
        Collections
      </div>

      {loading && (
        <div className="px-3 py-2 text-xs text-gray-500">Loading…</div>
      )}

      {error && (
        <div className="px-3 py-2 text-xs text-red-400">{error}</div>
      )}

      <ul className="flex-1 overflow-y-auto py-1">
        {collections.map((col) => (
          <li key={col.name}>
            <button
              onClick={() => onSelect(col.name)}
              className={`w-full text-left px-3 py-1.5 text-sm truncate transition-colors ${
                activeCollection === col.name
                  ? 'bg-blue-700 text-white'
                  : 'text-gray-300 hover:bg-gray-800'
              }`}
            >
              {col.name}
            </button>
          </li>
        ))}
      </ul>

      <div className="border-t border-gray-800 p-2">
        {creating ? (
          <form onSubmit={handleCreate} className="flex flex-col gap-1">
            <input
              autoFocus
              type="text"
              value={newName}
              onChange={(e) => { setNewName(e.target.value); setNameError('') }}
              placeholder="collection_name"
              className="rounded bg-gray-800 border border-gray-600 px-2 py-1 text-xs text-gray-100 focus:outline-none focus:border-blue-500"
            />
            {nameError && <p className="text-xs text-red-400">{nameError}</p>}
            <div className="flex gap-1">
              <button
                type="submit"
                className="flex-1 rounded bg-blue-600 hover:bg-blue-500 px-2 py-1 text-xs text-white transition-colors"
              >
                Create
              </button>
              <button
                type="button"
                onClick={() => { setCreating(false); setNewName(''); setNameError('') }}
                className="flex-1 rounded bg-gray-700 hover:bg-gray-600 px-2 py-1 text-xs text-gray-300 transition-colors"
              >
                Cancel
              </button>
            </div>
          </form>
        ) : (
          <button
            onClick={() => setCreating(true)}
            className="w-full text-left text-xs text-gray-400 hover:text-gray-100 transition-colors px-1 py-1"
          >
            + New Collection
          </button>
        )}
      </div>
    </aside>
  )
}
