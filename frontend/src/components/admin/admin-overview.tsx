import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  Building2,
  CalendarClock,
  KeyRound,
  Users,
} from 'lucide-react'
import { useMemo } from 'react'
import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import { Eyebrow } from '../ui/eyebrow'
import { formatDate, useAdmin } from '../../lib/admin'
import {
  APIKeyScopeType,
  WorkspaceMemberRole,
  WorkspaceMemberStatus,
  useListApiKeys,
  useListEventSubscriptions,
  useListEvents,
  useListWorkspaceMembers,
} from '../../lib/openapi'
import type {
  APIKeysCollection,
  EventSubscriptionsCollection,
  ExternalEventsCollection,
  WorkspaceMembersCollection,
} from '../../lib/openapi'

export function AdminOverview() {
  const { workspaceID, activeWorkspace } = useAdmin()

  const membersQuery = useListWorkspaceMembers<WorkspaceMembersCollection>(
    workspaceID,
    { query: { enabled: !!workspaceID, retry: false, staleTime: 30_000 } },
  )
  const keysQuery = useListApiKeys<APIKeysCollection>({
    query: { retry: false, staleTime: 30_000 },
  })
  const subscriptionsQuery =
    useListEventSubscriptions<EventSubscriptionsCollection>({
      query: { retry: false, staleTime: 30_000 },
    })
  const eventsQuery = useListEvents<ExternalEventsCollection>(
    { limit: 50 },
    { query: { retry: false, staleTime: 30_000 } },
  )

  const members = membersQuery.data?.items ?? []
  const keys = keysQuery.data?.items ?? []
  const subscriptions = subscriptionsQuery.data?.items ?? []
  const events = eventsQuery.data?.items ?? []

  const activeMembers = members.filter(
    (member) => member.status === WorkspaceMemberStatus.active,
  )
  const owners = activeMembers.filter(
    (member) => member.role === WorkspaceMemberRole.owner,
  )
  const admins = activeMembers.filter(
    (member) => member.role === WorkspaceMemberRole.admin,
  )
  const workspaceKeys = keys.filter(
    (key) =>
      key.scope_type === APIKeyScopeType.workspace &&
      key.scope_workspace_id === workspaceID,
  )
  const activeWorkspaceKeys = workspaceKeys.filter((key) => !key.revoked_at)
  const staleWorkspaceKeys = activeWorkspaceKeys.filter((key) => !key.last_used_at)
  const workspaceSubscriptions = subscriptions.filter(
    (subscription) => subscription.workspace_id === workspaceID,
  )
  const enabledSubscriptions = workspaceSubscriptions.filter(
    (subscription) => subscription.enabled,
  )

  const recentEvents = useMemo(
    () =>
      events
        .filter((event) => event.workspace_id === workspaceID)
        .sort(
          (left, right) =>
            Date.parse(right.occurred_at) - Date.parse(left.occurred_at),
        ),
    [events, workspaceID],
  )
  const lastDayCutoff = Date.now() - 24 * 60 * 60 * 1000
  const eventsLastDay = recentEvents.filter(
    (event) => Date.parse(event.occurred_at) >= lastDayCutoff,
  )

  if (!workspaceID) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          Select a workspace to inspect API usage, or{' '}
          <Link
            to="/workspace/settings"
            search={{ create: true }}
            className="text-[var(--ink)] underline underline-offset-4"
          >
            create a workspace
          </Link>
          .
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <div className="sys-panel">
        <div className="sys-panel-header">
          <span>Ops_Dashboard</span>
          <span className="sys-tag">LIVE_STATE</span>
        </div>
        <div className="sys-panel-body">
          <Eyebrow>Overview</Eyebrow>
          <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            {activeWorkspace?.name ?? 'Workspace'}
          </h1>
          <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
            API access, delivery health, and workspace footprint for the active
            tenant.
          </p>
        </div>
      </div>

      <Alert>
        Billing endpoints are not published in the current API contract. This
        dashboard uses members, keys, subscriptions, and events as operational
        billing signals instead.
      </Alert>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <SummaryCard
          icon={Users}
          label="Active members"
          value={activeMembers.length}
          detail={`${owners.length} owners, ${admins.length} admins`}
          to="/workspace/settings"
        />
        <SummaryCard
          icon={KeyRound}
          label="Workspace keys"
          value={activeWorkspaceKeys.length}
          detail={
            staleWorkspaceKeys.length
              ? `${staleWorkspaceKeys.length} never used`
              : 'All keys have activity'
          }
          to="/workspace/api-keys"
        />
        <SummaryCard
          icon={Building2}
          label="Subscriptions"
          value={enabledSubscriptions.length}
          detail={`${workspaceSubscriptions.length} configured`}
          to="/workspace/events"
        />
        <SummaryCard
          icon={CalendarClock}
          label="Events last 24h"
          value={eventsLastDay.length}
          detail={
            recentEvents[0]
              ? `Latest ${formatDate(recentEvents[0].occurred_at)}`
              : 'No workspace events yet'
          }
          to="/workspace/events"
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,.8fr)]">
        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
          <div className="flex items-center justify-between border-b border-[var(--sys-home-border)] px-4 py-3">
            <div>
              <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                Recent events
              </h2>
              <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
                External activity visible for this workspace.
              </p>
            </div>
            <Link
              to="/workspace/events"
              className="inline-flex items-center gap-1 text-xs uppercase tracking-[0.06em] text-[var(--sys-home-muted)] no-underline hover:text-[var(--sys-home-fg)]"
            >
              View all
              <ArrowRight className="h-3.5 w-3.5" />
            </Link>
          </div>

          {recentEvents.length ? (
            <div>
              {recentEvents.slice(0, 6).map((event, index) => (
                <div
                  key={event.id}
                  className={`flex items-start gap-3 px-4 py-3 ${
                    index > 0 ? 'border-t border-[var(--sys-home-border)]' : ''
                  }`}
                >
                  <Badge variant="muted">{event.resource_type}</Badge>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium text-[var(--sys-home-fg)]">
                      {event.type}
                    </div>
                    <div className="truncate text-xs text-[var(--sys-home-muted)]">
                      {event.resource_id}
                    </div>
                  </div>
                  <div className="text-right text-[11px] text-[var(--sys-home-muted)]">
                    {formatDate(event.occurred_at)}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
              No events have been published for this workspace yet.
            </div>
          )}
        </section>

        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
          <div className="border-b border-[var(--sys-home-border)] px-4 py-3">
            <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
              Access posture
            </h2>
            <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
              Quick checks for API and delivery hygiene.
            </p>
          </div>
          <div className="space-y-3 px-4 py-4 text-sm text-[var(--sys-home-muted)]">
            <SignalRow
              label="Workspace keys"
              value={`${activeWorkspaceKeys.length} active`}
            />
            <SignalRow
              label="Unused keys"
              value={
                staleWorkspaceKeys.length
                  ? `${staleWorkspaceKeys.length} need review`
                  : 'No unused active keys'
              }
            />
            <SignalRow
              label="Webhook subscriptions"
              value={`${enabledSubscriptions.length} enabled / ${workspaceSubscriptions.length} total`}
            />
            <SignalRow
              label="Membership"
              value={`${activeMembers.length} active members`}
            />
            <div className="pt-2">
              <Link
                to="/workspace/settings"
                className="inline-flex items-center gap-1 text-xs uppercase tracking-[0.06em] text-[var(--sys-home-fg)] no-underline hover:text-[var(--sys-home-muted)]"
              >
                Open billing monitor
                <ArrowRight className="h-3.5 w-3.5" />
              </Link>
            </div>
          </div>
        </section>
      </div>
    </div>
  )
}

function SummaryCard({
  icon: Icon,
  label,
  value,
  detail,
  to,
}: {
  icon: typeof Users
  label: string
  value: number
  detail: string
  to: string
}) {
  return (
    <Link
      to={to}
      className="group border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-4 py-4 no-underline sys-hover"
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-[11px] uppercase tracking-[0.08em] text-[var(--sys-home-muted)] group-hover:text-[var(--sys-home-fg)]">
            {label}
          </div>
          <div className="mt-2 text-3xl font-bold tabular-nums text-[var(--sys-home-fg)]">
            {value}
          </div>
          <div className="mt-2 text-xs text-[var(--sys-home-muted)] group-hover:text-[var(--sys-home-fg)]">
            {detail}
          </div>
        </div>
        <Icon className="h-4 w-4 text-[var(--sys-home-muted)] group-hover:text-[var(--sys-home-fg)]" />
      </div>
    </Link>
  )
}

function SignalRow({
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
      <span className="text-right text-sm text-[var(--sys-home-fg)]">
        {value}
      </span>
    </div>
  )
}
