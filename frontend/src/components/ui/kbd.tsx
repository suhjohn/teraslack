import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export function Kbd({
  className,
  ...props
}: HTMLAttributes<HTMLElement>) {
  return (
    <kbd
      className={cn(
        'inline-flex h-5 min-w-5 items-center justify-center border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-1.5 font-[family-name:var(--font-mono)] text-[10px] font-bold text-[var(--sys-home-muted)]',
        className,
      )}
      {...props}
    />
  )
}
