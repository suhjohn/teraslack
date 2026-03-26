import type { HTMLAttributes, LabelHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

/* ------------------------------------------------------------------ */
/*  Label                                                             */
/* ------------------------------------------------------------------ */

export function Label({
  className,
  ...props
}: LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label
      className={cn(
        'font-[family-name:var(--font-mono)] text-[11px] font-bold uppercase tracking-[0.06em] text-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    />
  )
}

/* ------------------------------------------------------------------ */
/*  FormField — wraps label + input + error/description               */
/* ------------------------------------------------------------------ */

type FormFieldProps = HTMLAttributes<HTMLDivElement> & {
  label?: string
  htmlFor?: string
  description?: string
  error?: string
}

export function FormField({
  label,
  htmlFor,
  description,
  error,
  className,
  children,
  ...props
}: FormFieldProps) {
  return (
    <div className={cn('space-y-1.5', className)} {...props}>
      {label && (
        <Label htmlFor={htmlFor}>{label}</Label>
      )}
      {children}
      {description && !error && (
        <p className="text-[11px] text-[var(--sys-home-muted)]">{description}</p>
      )}
      {error && (
        <p className="text-[11px] text-[#dc2626]">{error}</p>
      )}
    </div>
  )
}
