import { useToast } from '../contexts/ToastContext'

const TYPE_STYLES = {
  success: 'bg-green-800 border-green-600 text-green-100',
  info:    'bg-blue-800 border-blue-600 text-blue-100',
  error:   'bg-red-800 border-red-600 text-red-100',
} as const

const TYPE_ICONS = {
  success: '✓',
  info:    'ℹ',
  error:   '✕',
} as const

export default function ToastContainer() {
  const { toasts, dismiss } = useToast()

  if (toasts.length === 0) return null

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 w-80">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={`flex items-start gap-3 rounded-lg border px-4 py-3 shadow-lg text-sm ${TYPE_STYLES[toast.type]}`}
        >
          <span className="mt-0.5 shrink-0 font-bold">{TYPE_ICONS[toast.type]}</span>
          <span className="flex-1 break-words">{toast.message}</span>
          <button
            onClick={() => dismiss(toast.id)}
            className="ml-auto shrink-0 opacity-70 hover:opacity-100 transition-opacity"
            aria-label="Dismiss"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  )
}
