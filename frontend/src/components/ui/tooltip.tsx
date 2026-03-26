import { useState, useRef, type ReactNode, type HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type TooltipProps = HTMLAttributes<HTMLDivElement> & {
  content: ReactNode
  side?: 'top' | 'bottom'
  children: ReactNode
}

export function Tooltip({
  content,
  side = 'top',
  children,
  className,
  ...props
}: TooltipProps) {
  const [visible, setVisible] = useState(false)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const show = () => {
    if (timeoutRef.current !== null) {
      clearTimeout(timeoutRef.current)
    }
    timeoutRef.current = setTimeout(() => setVisible(true), 200)
  }

  const hide = () => {
    if (timeoutRef.current !== null) {
      clearTimeout(timeoutRef.current)
    }
    setVisible(false)
  }

  return (
    <div
      className={cn('relative inline-flex', className)}
      onMouseEnter={show}
      onMouseLeave={hide}
      onFocus={show}
      onBlur={hide}
      {...props}
    >
      {children}
      {visible && (
        <div
          role="tooltip"
          className={cn(
            'pointer-events-none absolute left-1/2 z-50 -translate-x-1/2 whitespace-nowrap border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-2.5 py-1 font-[family-name:var(--font-mono)] text-[10px] font-bold uppercase tracking-[0.06em] text-[var(--sys-home-fg)]',
            side === 'top' && 'bottom-full mb-1.5',
            side === 'bottom' && 'top-full mt-1.5',
          )}
        >
          {content}
        </div>
      )}
    </div>
  )
}
