import { useState } from 'react'
import type { Filter, FilterOp, FindRequest } from '../api/types'

const OPS: FilterOp[] = ['EQ', 'NEQ', 'GT', 'GTE', 'LT', 'LTE', 'CONTAINS', 'REGEX']

interface FilterRow {
  id: number
  field: string
  op: FilterOp
  value: string
}

interface Props {
  onQuery: (req: FindRequest) => void
}

let rowId = 0

function emptyRow(): FilterRow {
  return { id: rowId++, field: '', op: 'EQ', value: '' }
}

export default function FilterBar({ onQuery }: Props) {
  const [rows, setRows] = useState<FilterRow[]>([emptyRow()])
  const [logic, setLogic] = useState<'AND' | 'OR'>('AND')
  const [orderBy, setOrderBy] = useState('')
  const [descending, setDescending] = useState(false)
  const [limit, setLimit] = useState(20)

  function updateRow(id: number, patch: Partial<FilterRow>) {
    setRows((prev) => prev.map((r) => (r.id === id ? { ...r, ...patch } : r)))
  }

  function removeRow(id: number) {
    setRows((prev) => (prev.length > 1 ? prev.filter((r) => r.id !== id) : prev))
  }

  function buildFilter(): Filter | undefined {
    const filled = rows.filter((r) => r.field.trim() && r.value.trim())
    if (filled.length === 0) return undefined
    const conditions: Filter[] = filled.map((r) => ({
      field: { field: r.field.trim(), op: r.op, value: r.value.trim() },
    }))
    if (conditions.length === 1) return conditions[0]
    return logic === 'AND'
      ? { and: { filters: conditions } }
      : { or: { filters: conditions } }
  }

  function handleRun(e: React.FormEvent) {
    e.preventDefault()
    const req: FindRequest = {
      filter: buildFilter(),
      limit,
      offset: 0,
      order_by: orderBy.trim() || undefined,
      descending: orderBy.trim() ? descending : undefined,
    }
    onQuery(req)
  }

  function handleClear() {
    setRows([emptyRow()])
    setLogic('AND')
    setOrderBy('')
    setDescending(false)
    setLimit(20)
    onQuery({ limit: 20, offset: 0 })
  }

  return (
    <form onSubmit={handleRun} className="flex flex-col gap-2">
      {rows.map((row, idx) => (
        <div key={row.id} className="flex items-center gap-2">
          {idx > 0 && (
            <button
              type="button"
              onClick={() => setLogic((l) => (l === 'AND' ? 'OR' : 'AND'))}
              className="w-10 shrink-0 text-xs font-medium text-blue-400 hover:text-blue-300 transition-colors"
            >
              {logic}
            </button>
          )}
          {idx === 0 && <div className="w-10 shrink-0" />}
          <input
            type="text"
            value={row.field}
            onChange={(e) => updateRow(row.id, { field: e.target.value })}
            placeholder="field"
            className="w-32 rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-gray-100 placeholder-gray-600 focus:outline-none focus:border-blue-500"
          />
          <select
            value={row.op}
            onChange={(e) => updateRow(row.id, { op: e.target.value as FilterOp })}
            className="rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-gray-300 focus:outline-none focus:border-blue-500"
          >
            {OPS.map((op) => (
              <option key={op} value={op} className="bg-gray-800 text-gray-200">
                {op}
              </option>
            ))}
          </select>
          <input
            type="text"
            value={row.value}
            onChange={(e) => updateRow(row.id, { value: e.target.value })}
            placeholder="value"
            className="flex-1 rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-gray-100 placeholder-gray-600 focus:outline-none focus:border-blue-500"
          />
          <button
            type="button"
            onClick={() => removeRow(row.id)}
            className="text-gray-600 hover:text-red-400 transition-colors text-lg leading-none"
            aria-label="Remove filter row"
          >
            ×
          </button>
        </div>
      ))}

      {/* Add row button */}
      <button
        type="button"
        onClick={() => setRows((prev) => [...prev, emptyRow()])}
        className="self-start text-xs text-blue-400 hover:text-blue-300 transition-colors"
      >
        + Add filter
      </button>

      {/* Order / Limit */}
      <div className="flex items-center gap-3 mt-1">
        <span className="text-xs text-gray-500">Order by</span>
        <input
          type="text"
          value={orderBy}
          onChange={(e) => setOrderBy(e.target.value)}
          placeholder="field"
          className="w-28 rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-gray-100 placeholder-gray-600 focus:outline-none focus:border-blue-500"
        />
        <select
          value={descending ? 'desc' : 'asc'}
          onChange={(e) => setDescending(e.target.value === 'desc')}
          className="rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-gray-300 focus:outline-none focus:border-blue-500"
        >
          <option value="asc" className="bg-gray-800 text-gray-200">asc</option>
          <option value="desc" className="bg-gray-800 text-gray-200">desc</option>
        </select>
        <span className="text-xs text-gray-500 ml-2">Limit</span>
        <input
          type="number"
          value={limit}
          onChange={(e) => setLimit(Math.max(1, Number(e.target.value)))}
          min={1}
          max={1000}
          className="w-20 rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-gray-100 focus:outline-none focus:border-blue-500"
        />
        <button
          type="submit"
          className="ml-auto rounded bg-blue-600 hover:bg-blue-500 px-4 py-1.5 text-sm font-medium text-white transition-colors"
        >
          Run
        </button>
        <button
          type="button"
          onClick={handleClear}
          className="rounded bg-gray-700 hover:bg-gray-600 px-4 py-1.5 text-sm text-gray-300 transition-colors"
        >
          Clear
        </button>
      </div>
    </form>
  )
}
