import type { ButtonHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'default' | 'outline' | 'ghost' | 'destructive' | 'link'
  size?: 'default' | 'sm' | 'icon'
}

const variantClasses: Record<NonNullable<ButtonProps['variant']>, string> = {
  default:
    'border-[var(--sys-home-border)] bg-[var(--sys-home-fg)] text-[var(--sys-home-bg)] transition-opacity hover:opacity-80',
  outline:
    'border-[var(--sys-home-border)] bg-transparent text-[var(--sys-home-fg)] sys-hover',
  ghost:
    'border-transparent bg-transparent text-[var(--sys-home-muted)] sys-hover hover:border-[var(--sys-home-border)]',
  destructive:
    'border-[#dc2626] bg-[#dc2626] text-white hover:border-[#b91c1c] hover:bg-[#b91c1c]',
  link:
    'border-transparent bg-transparent px-0 text-[var(--sys-home-fg)] underline underline-offset-4 hover:text-[var(--sys-home-muted)]',
}

const sizeClasses: Record<NonNullable<ButtonProps['size']>, string> = {
  default: 'min-h-9 px-3 py-2 text-[11px]',
  sm: 'min-h-8 px-2.5 py-1.5 text-[10px]',
  icon: 'h-9 w-9 p-0',
}

export function Button({
  className,
  variant = 'default',
  size = 'default',
  type = 'button',
  ...props
}: ButtonProps) {
  return (
    <button
      data-slot="button"
      type={type}
      className={cn(
        'inline-flex items-center justify-center gap-2 rounded-none border font-[family-name:var(--font-mono)] font-bold uppercase tracking-[0.08em] transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        variantClasses[variant],
        sizeClasses[size],
        className,
      )}
      {...props}
    />
  )
}
