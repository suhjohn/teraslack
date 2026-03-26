import type { InputHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export function Input({
  className,
  type = 'text',
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      data-slot="input"
      type={type}
      className={cn(
        'flex min-h-9 w-full rounded-none border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-3 py-2 font-[family-name:var(--font-mono)] text-[13px] text-[var(--sys-home-fg)] placeholder:text-[var(--sys-home-muted)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    />
  )
}
