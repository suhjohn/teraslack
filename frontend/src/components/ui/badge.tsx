import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type BadgeProps = HTMLAttributes<HTMLSpanElement> & {
  variant?: 'default' | 'muted' | 'success' | 'warning' | 'destructive'
}

const variantClasses: Record<NonNullable<BadgeProps['variant']>, string> = {
  default:
    'border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] text-[var(--sys-home-fg)]',
  muted:
    'border-[var(--sys-home-border)] bg-transparent text-[var(--sys-home-muted)]',
  success:
    'border-[#16a34a] bg-transparent text-[#16a34a]',
  warning:
    'border-[#ca8a04] bg-transparent text-[#ca8a04]',
  destructive:
    'border-[#dc2626] bg-transparent text-[#dc2626]',
}

export function Badge({
  className,
  variant = 'default',
  ...props
}: BadgeProps) {
  return (
    <span
      data-slot="badge"
      className={cn(
        'inline-flex items-center rounded-none border px-2 py-0.5 font-[family-name:var(--font-mono)] text-[10px] font-bold uppercase tracking-[0.08em]',
        variantClasses[variant],
        className,
      )}
      {...props}
    />
  )
}
