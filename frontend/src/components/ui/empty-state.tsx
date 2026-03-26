import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '../../lib/utils'

type EmptyStateProps = HTMLAttributes<HTMLDivElement> & {
  icon?: ReactNode
  heading?: string
  description?: string
}

export function EmptyState({
  icon,
  heading,
  description,
  className,
  children,
  ...props
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex flex-col items-center gap-3 border border-dashed border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-6 py-10 text-center font-[family-name:var(--font-mono)]',
        className,
      )}
      {...props}
    >
      {icon && (
        <span className="text-[var(--sys-home-muted)]">{icon}</span>
      )}
      {heading && (
        <p className="text-[12px] font-bold uppercase tracking-[0.06em] text-[var(--sys-home-fg)]">{heading}</p>
      )}
      {description && (
        <p className="max-w-xs text-[12px] text-[var(--sys-home-muted)]">{description}</p>
      )}
      {children}
    </div>
  )
}
