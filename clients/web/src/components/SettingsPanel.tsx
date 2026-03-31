import { useState } from 'react'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props {
  onClose: () => void
}

export default function SettingsPanel({ onClose }: Props) {
  const { settings, saveSettings, recheckConnection } = useApp()
  const { showToast } = useToast()
  const [url, setUrl] = useState(settings.url)
  const [apiKey, setApiKey] = useState(settings.apiKey)

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    saveSettings({ url: url.trim(), apiKey: apiKey.trim() })
    recheckConnection()
    showToast('success', 'Settings saved')
    onClose()
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end">
      {/* backdrop */}
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />
      <div className="relative z-10 flex flex-col w-80 bg-gray-900 border-l border-gray-700 shadow-xl">
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-700">
          <h2 className="text-sm font-semibold text-gray-100">Settings</h2>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-100 transition-colors"
            aria-label="Close settings"
          >
            ✕
          </button>
        </div>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4 p-4 flex-1">
          <div className="flex flex-col gap-1">
            <label className="text-xs text-gray-400">Server URL</label>
            <input
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              className="rounded bg-gray-800 border border-gray-600 px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-blue-500"
              required
            />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-gray-400">API Key</label>
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              className="rounded bg-gray-800 border border-gray-600 px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-blue-500"
              placeholder="(optional)"
            />
          </div>
          <button
            type="submit"
            className="mt-auto rounded bg-blue-600 hover:bg-blue-500 px-4 py-2 text-sm font-medium text-white transition-colors"
          >
            Save &amp; Reconnect
          </button>
        </form>
      </div>
    </div>
  )
}
