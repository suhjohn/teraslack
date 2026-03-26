import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '../../lib/utils'

/* ------------------------------------------------------------------ */
/*  DetailTile — stat block for metadata grids                        */
/* ------------------------------------------------------------------ */

type DetailTileProps = HTMLAttributes<HTMLDivElement> & {
  label: string
  value: ReactNode
}

export function DetailTile({
  label,
  value,
  className,
  ...props
}: DetailTileProps) {
  return (
    <div
      data-slot="detail-tile"
      className={cn(
        'border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-3.5',
        className,
      )}
      {...props}
    >
      <p className="font-[family-name:var(--font-mono)] text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
        {label}
      </p>
      <p className="mt-1 text-[13px] text-[var(--sys-home-fg)]">{value}</p>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  InfoRow — icon + label + value horizontal row                     */
/* ------------------------------------------------------------------ */

type InfoRowProps = HTMLAttributes<HTMLDivElement> & {
  icon?: ReactNode
  label: string
  value: ReactNode
}

export function InfoRow({
  icon,
  label,
  value,
  className,
  ...props
}: InfoRowProps) {
  return (
    <div
      className={cn('flex items-center gap-2.5 text-[13px]', className)}
      {...props}
    >
      {icon && (
        <span className="flex-none text-[var(--sys-home-muted)]">{icon}</span>
      )}
      <span className="text-[var(--sys-home-muted)]">{label}</span>
      <span className="ml-auto text-[var(--sys-home-fg)]">{value}</span>
    </div>
  )
}
