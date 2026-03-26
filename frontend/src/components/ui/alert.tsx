import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type AlertProps = HTMLAttributes<HTMLDivElement> & {
  variant?: 'default' | 'destructive'
}

const variantClasses: Record<NonNullable<AlertProps['variant']>, string> = {
  default:
    'border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] text-[var(--sys-home-muted)]',
  destructive:
    'border-[#dc2626] bg-transparent text-[#dc2626]',
}

export function Alert({
  className,
  variant = 'default',
  ...props
}: AlertProps) {
  return (
    <div
      data-slot="alert"
      className={cn(
        'flex items-start gap-3 border px-4 py-3 font-[family-name:var(--font-mono)] text-[12px] uppercase tracking-[0.03em]',
        variantClasses[variant],
        className,
      )}
      {...props}
    />
  )
}
