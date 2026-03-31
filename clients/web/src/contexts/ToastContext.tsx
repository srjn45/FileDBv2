import {
  createContext,
  useCallback,
  useContext,
  useRef,
  useState,
} from 'react'

export type ToastType = 'success' | 'info' | 'error'

export interface Toast {
  id: number
  type: ToastType
  message: string
}

interface ToastContextValue {
  toasts: Toast[]
  showToast: (type: ToastType, message: string) => void
  dismiss: (id: number) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

let nextId = 0

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const timers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map())

  const dismiss = useCallback((id: number) => {
    clearTimeout(timers.current.get(id))
    timers.current.delete(id)
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const showToast = useCallback(
    (type: ToastType, message: string) => {
      const id = nextId++
      setToasts((prev) => {
        const next = [...prev, { id, type, message }]
        // Keep at most 3
        return next.length > 3 ? next.slice(next.length - 3) : next
      })
      const timer = setTimeout(() => dismiss(id), 4000)
      timers.current.set(id, timer)
    },
    [dismiss],
  )

  return (
    <ToastContext.Provider value={{ toasts, showToast, dismiss }}>
      {children}
    </ToastContext.Provider>
  )
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside ToastProvider')
  return ctx
}
