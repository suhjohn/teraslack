import type { SelectHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export function Select({
  className,
  ...props
}: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      data-slot="select"
      className={cn(
        'flex min-h-9 w-full rounded-none border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-3 py-2 font-[family-name:var(--font-mono)] text-[13px] text-[var(--sys-home-fg)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    />
  )
}
