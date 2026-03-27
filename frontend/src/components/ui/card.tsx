import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type CardProps = HTMLAttributes<HTMLDivElement> & {
  variant?: 'default' | 'interactive'
}

export function Card({
  className,
  variant = 'default',
  ...props
}: CardProps) {
  return (
    <div
      data-slot="card"
      className={cn(
        'border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] text-[var(--sys-home-fg)]',
        variant === 'interactive' &&
          'cursor-pointer sys-hover',
        className,
      )}
      {...props}
    />
  )
}

export function CardHeader({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('space-y-2', className)} {...props} />
}

export function CardTitle({
  className,
  ...props
}: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h2
      className={cn(
        'font-[family-name:var(--font-mono)] text-lg font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    />
  )
}

export function CardDescription({
  className,
  ...props
}: HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={cn('text-[13px] leading-6 text-[var(--sys-home-muted)]', className)}
      {...props}
    />
  )
}

export function CardContent({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('text-[13px] leading-[1.5] text-[var(--sys-home-muted)]', className)}
      {...props}
    />
  )
}
