import type { ReactNode } from 'react'
import { AlertTriangle, LoaderCircle } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { Badge } from '../ui/badge'
import { EmptyState } from '../ui/empty-state'
import { cn } from '../../lib/utils'

export function DashboardHeader({
  title,
  description,
  eyebrow = 'Dashboard',
  tag,
}: {
  title: string
  description: string
  eyebrow?: string
  tag?: string
}) {
  return (
    <div className="sys-panel">
      <div className="sys-panel-header">
        <span>{eyebrow}</span>
        {tag ? <span className="sys-tag">{tag}</span> : null}
      </div>
      <div className="sys-panel-body">
        <p className="text-[11px] uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          {eyebrow}
        </p>
        <h1 className="mt-2 text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
          {title}
        </h1>
        <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
          {description}
        </p>
      </div>
    </div>
  )
}

export function DashboardMetric({
  label,
  value,
  detail,
  href,
}: {
  label: string
  value: string
  detail?: string
  href?: string
}) {
  const content = (
    <div className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-4 py-4">
      <div className="text-[11px] uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
        {label}
      </div>
      <div className="mt-2 text-3xl font-bold tabular-nums text-[var(--sys-home-fg)]">
        {value}
      </div>
      {detail ? (
        <div className="mt-2 text-xs text-[var(--sys-home-muted)]">{detail}</div>
      ) : null}
    </div>
  )

  if (!href) {
    return content
  }

  return (
    <Link to={href} className="no-underline sys-hover">
      {content}
    </Link>
  )
}

export function DashboardSection({
  title,
  description,
  action,
  children,
  className,
}: {
  title: string
  description?: string
  action?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cn('border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]', className)}>
      <div className="flex items-start justify-between gap-3 border-b border-[var(--sys-home-border)] px-4 py-3">
        <div>
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            {title}
          </h2>
          {description ? (
            <p className="mt-1 text-xs text-[var(--sys-home-muted)]">{description}</p>
          ) : null}
        </div>
        {action}
      </div>
      <div>{children}</div>
    </section>
  )
}

export function DashboardScopeBadge({
  workspaceName,
}: {
  workspaceName?: string | null
}) {
  return (
    <Badge variant="muted">
      {workspaceName ? `Scope ${workspaceName}` : 'Scope All workspaces'}
    </Badge>
  )
}

export function DashboardLoadingState({
  label = 'Loading dashboard state…',
}: {
  label?: string
}) {
  return (
    <div className="flex min-h-[32vh] items-center justify-center border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
      <span className="inline-flex items-center gap-3 text-[12px] uppercase tracking-[0.06em] text-[var(--sys-home-muted)]">
        <LoaderCircle className="h-4 w-4 animate-spin" />
        {label}
      </span>
    </div>
  )
}

export function DashboardEmptyState({
  heading,
  description,
}: {
  heading: string
  description: string
}) {
  return (
    <EmptyState
      icon={<AlertTriangle className="h-5 w-5" />}
      heading={heading}
      description={description}
    />
  )
}
