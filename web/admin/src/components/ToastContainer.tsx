import { useCallback } from 'preact/compat'
import { useI18n } from '../i18n'
import type { ToastItem, ToastType } from '../hooks/useToast'

const ICONS: Record<ToastType, string> = {
  success: '✓',
  error: '✕',
  warning: '▲',
  info: 'ℹ',
}

interface ToastContainerProps {
  toasts: ToastItem[]
  onClose: (id: string) => void
}

export function ToastContainer({ toasts, onClose }: ToastContainerProps) {
  if (toasts.length === 0) return null

  return (
    <div class="toast-container">
      {toasts.map((toast) => (
        <Toast key={toast.id} toast={toast} onClose={onClose} />
      ))}
    </div>
  )
}

function Toast({ toast, onClose }: { toast: ToastItem; onClose: (id: string) => void }) {
  const { t } = useI18n()
  const handleClose = useCallback(() => {
    onClose(toast.id)
  }, [onClose, toast.id])

  return (
    <div class={`toast-item ${toast.type}`} role="alert">
      <span class="toast-icon">{ICONS[toast.type]}</span>
      <span class="toast-message">{toast.message}</span>
      <button type="button" class="toast-close" onClick={handleClose} aria-label={t('toast.close')}>
        ×
      </button>
    </div>
  )
}
