import { Link } from '@tanstack/react-router'
import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../ui/table'
import {
  DashboardHeader,
  DashboardLoadingState,
  DashboardMetric,
  DashboardScopeBadge,
  DashboardSection,
} from './dashboard-kit'
import {
  formatDate,
  formatNumber,
  formatPercent,
  getDashboardWorkspaceParams,
  getErrorMessage,
  useAdmin,
} from '../../lib/admin'
import {
  useGetDashboardAudit,
  useGetDashboardOverview,
} from '../../lib/openapi'
import type {
  DashboardAuditResponse,
  DashboardOverview as DashboardOverviewData,
} from '../../lib/openapi'

export function AdminOverview() {
  const { workspaceID } = useAdmin()
  const params = getDashboardWorkspaceParams(workspaceID)

  const overviewQuery = useGetDashboardOverview<DashboardOverviewData>(params, {
    query: { retry: false, staleTime: 30_000 },
  })
  const auditQuery = useGetDashboardAudit<DashboardAuditResponse>(
    { ...params, limit: 6 },
    { query: { retry: false, staleTime: 30_000 } },
  )

  if (overviewQuery.isPending && !overviewQuery.data) {
    return <DashboardLoadingState />
  }

  if (!overviewQuery.data) {
    return (
      <div className="space-y-4">
        <DashboardHeader
          eyebrow="Usage"
          title="Overview"
          description="The top-level dashboard summary could not be loaded."
          tag="ERROR"
        />
        <Alert variant="destructive">
          {getErrorMessage(
            overviewQuery.error,
            'Failed to load dashboard overview.',
          )}
        </Alert>
      </div>
    )
  }

  const overview = overviewQuery.data
  const auditItems = auditQuery.data?.items ?? []
  const successRate =
    overview.traffic.requests_7d > 0
      ? overview.traffic.success_7d / overview.traffic.requests_7d
      : 0

  return (
    <div className="space-y-8">
      <DashboardHeader
        eyebrow="Usage"
        title="Overview"
        description="Account or workspace health across keys, traffic, delivery state, and stored messaging activity."
        tag="LIVE"
      />

      <div className="flex flex-wrap items-center gap-2">
        <DashboardScopeBadge
          workspaceName={overview.scope.workspace_name ?? null}
        />
        <Badge variant="muted">
          Success 7d {formatPercent(successRate)}
        </Badge>
        {overview.api_keys.last_used_at ? (
          <Badge variant="muted">
            Last key use {formatDate(overview.api_keys.last_used_at)}
          </Badge>
        ) : null}
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <DashboardMetric
          label="Active keys"
          value={formatNumber(overview.api_keys.active)}
          detail={`${formatNumber(overview.api_keys.expiring_soon)} expiring soon`}
          href="/workspace/api-keys"
        />
        <DashboardMetric
          label="Requests 24h"
          value={formatNumber(overview.traffic.requests_24h)}
          detail={`${formatNumber(overview.traffic.requests_7d)} over 7 days`}
          href="/workspace/traffic"
        />
        <DashboardMetric
          label="Failed deliveries"
          value={formatNumber(overview.webhooks.failed_deliveries)}
          detail={`${formatNumber(overview.webhooks.enabled_subscriptions)} enabled subscriptions`}
          href="/workspace/events"
        />
        <DashboardMetric
          label="Messages 7d"
          value={formatNumber(overview.data.messages_7d)}
          detail={`${formatNumber(overview.data.conversations)} visible conversations`}
          href="/workspace/data-activity"
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,.9fr)]">
        <DashboardSection
          title="Operational posture"
          description="The fastest way to spot stale credentials, traffic pressure, and delivery issues."
        >
          <div className="space-y-3 px-4 py-4 text-sm text-[var(--sys-home-muted)]">
            <OverviewRow
              label="Key hygiene"
              value={
                overview.api_keys.stale > 0
                  ? `${formatNumber(overview.api_keys.stale)} stale keys`
                  : 'No stale keys'
              }
            />
            <OverviewRow
              label="Rate limits"
              value={
                overview.traffic.rate_limited_7d > 0
                  ? `${formatNumber(overview.traffic.rate_limited_7d)} requests throttled`
                  : 'No throttling observed'
              }
            />
            <OverviewRow
              label="Latency"
              value={`avg ${formatNumber(overview.traffic.avg_duration_ms)} ms / p95 ${formatNumber(overview.traffic.p95_duration_ms)} ms`}
            />
            <OverviewRow
              label="Delivery queue"
              value={
                overview.webhooks.pending_deliveries > 0
                  ? `${formatNumber(overview.webhooks.pending_deliveries)} pending deliveries`
                  : 'No pending deliveries'
              }
            />
            <OverviewRow
              label="Room mix"
              value={`${formatNumber(overview.data.member_conversations)} member-only / ${formatNumber(overview.data.broadcast_conversations)} broadcast`}
            />
          </div>
        </DashboardSection>

        <DashboardSection
          title="Recent audit"
          description="Latest internal lifecycle events initiated by the current caller."
          action={
            <Link
              to="/workspace/audit"
              className="text-xs uppercase tracking-[0.06em] text-[var(--sys-home-muted)] no-underline hover:text-[var(--sys-home-fg)]"
            >
              Open audit
            </Link>
          }
        >
          {auditQuery.error ? (
            <Alert variant="destructive" className="m-4">
              {getErrorMessage(auditQuery.error, 'Failed to load audit activity.')}
            </Alert>
          ) : auditItems.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Event</TableHead>
                  <TableHead>Aggregate</TableHead>
                  <TableHead className="text-right">Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {auditItems.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="align-top">
                      <div className="font-bold">{item.event_type}</div>
                    </TableCell>
                    <TableCell className="align-top text-[var(--sys-home-muted)]">
                      <div>{item.aggregate_type}</div>
                      <div className="truncate text-[11px]">{item.aggregate_id}</div>
                    </TableCell>
                    <TableCell className="text-right text-[var(--sys-home-muted)]">
                      {formatDate(item.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
              No audit activity recorded for this scope.
            </div>
          )}
        </DashboardSection>
      </div>
    </div>
  )
}

function OverviewRow({
  label,
  value,
}: {
  label: string
  value: string
}) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-[var(--sys-home-border)] pb-3 last:border-b-0 last:pb-0">
      <span className="text-[11px] uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
        {label}
      </span>
      <span className="text-right text-sm text-[var(--sys-home-fg)]">{value}</span>
    </div>
  )
}
