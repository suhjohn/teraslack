import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type CodeBlockProps = HTMLAttributes<HTMLPreElement> & {
  copyable?: boolean
}

export function CodeBlock({
  className,
  children,
  ...props
}: CodeBlockProps) {
  return (
    <pre
      className={cn(
        'overflow-x-auto border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-4 font-[family-name:var(--font-mono)] text-[12px] leading-relaxed text-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    >
      <code>{children}</code>
    </pre>
  )
}

export function InlineCode({
  className,
  ...props
}: HTMLAttributes<HTMLElement>) {
  return (
    <code
      className={cn(
        'border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-1.5 py-0.5 text-[0.875em] text-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    />
  )
}
