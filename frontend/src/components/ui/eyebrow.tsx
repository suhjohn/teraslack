import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export function Eyebrow({
  className,
  ...props
}: HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={cn(
        'font-[family-name:var(--font-mono)] text-[10px] font-bold uppercase tracking-[0.14em] text-[var(--sys-home-muted)]',
        className,
      )}
      {...props}
    />
  )
}
