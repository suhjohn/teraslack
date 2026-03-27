import type { ButtonHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type ListItemProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  selected?: boolean
}

export function ListItem({
  className,
  selected = false,
  ...props
}: ListItemProps) {
  return (
    <button
      data-slot="list-item"
      type="button"
      data-status={selected ? 'active' : 'inactive'}
      className={cn(
        'flex w-full items-center justify-between gap-3 border px-3 py-2.5 text-left font-[family-name:var(--font-mono)] text-[12px] transition-colors cursor-pointer',
        selected
          ? 'border-[var(--sys-home-border)] bg-[var(--sys-home-accent-bg)] text-[var(--sys-home-accent-fg)]'
          : 'border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] text-[var(--sys-home-fg)] sys-hover',
        className,
      )}
      {...props}
    />
  )
}
