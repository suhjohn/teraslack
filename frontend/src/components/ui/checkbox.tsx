import type { InputHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export function Checkbox({
  className,
  ...props
}: Omit<InputHTMLAttributes<HTMLInputElement>, 'type'>) {
  return (
    <input
      data-slot="checkbox"
      type="checkbox"
      className={cn(
        'h-4 w-4 rounded-none border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] accent-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    />
  )
}
