import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  KeyRound,
  Users,
} from 'lucide-react'
import { useMemo } from 'react'
import { Badge } from '../ui/badge'
import { Eyebrow } from '../ui/eyebrow'
import { formatDate, useAdmin } from '../../lib/admin'
import {
  useListApiKeys,
  useListWorkspaceAuthorizationAuditLogs,
  useListWorkspaceIntegrationLogs,
  useListUsers,
} from '../../lib/openapi'
import type {
  APIKeysCollection,
  AuthorizationAuditLogsCollection,
  UsersCollection,
  WorkspaceIntegrationLogsCollection,
} from '../../lib/openapi'

type ActivityItem = {
  id: string
  kind: 'audit' | 'integration'
  title: string
  detail: string
  actor: string
  timestamp: string
}

export function AdminOverview() {
  const { workspaceID, activeWorkspace } = useAdmin()

  const usersQuery = useListUsers<UsersCollection>(
    { workspace_id: workspaceID, limit: 200 },
    { query: { enabled: !!workspaceID, retry: false } },
  )
  const apiKeysQuery = useListApiKeys<APIKeysCollection>(
    { workspace_id: workspaceID },
    { query: { enabled: !!workspaceID, retry: false } },
  )
  const auditQuery =
    useListWorkspaceAuthorizationAuditLogs<AuthorizationAuditLogsCollection>(
      workspaceID,
      { limit: 10 },
      { query: { enabled: !!workspaceID, retry: false } },
    )
  const integrationQuery =
    useListWorkspaceIntegrationLogs<WorkspaceIntegrationLogsCollection>(
      workspaceID,
      { limit: 10 },
      { query: { enabled: !!workspaceID, retry: false } },
    )

  const users = usersQuery.data?.items ?? []
  const apiKeys = apiKeysQuery.data?.items ?? []
  const auditItems = auditQuery.data?.items ?? []
  const integrationItems = integrationQuery.data?.items ?? []

  const delegatedUsers = users.filter(
    (user) => (user.delegated_roles?.length ?? 0) > 0,
  )
  const activeKeys = apiKeys.filter((key) => !key.revoked)
  const staleKeys = activeKeys.filter((key) => !key.last_used_at)

  const activity = useMemo(() => {
    const items: ActivityItem[] = [
      ...auditItems.map((item) => ({
        id: `audit-${item.id}`,
        kind: 'audit' as const,
        title: item.action,
        detail: `${item.resource}/${item.resource_id}`,
        actor: item.actor_id || 'system',
        timestamp: item.created_at,
      })),
      ...integrationItems.map((item, index) => ({
        id: `integration-${item.app_id}-${item.date}-${index}`,
        kind: 'integration' as const,
        title: item.action,
        detail: `${item.app_name} (${item.app_type})`,
        actor: item.user_name || item.user_id,
        timestamp: item.date,
      })),
    ]

    return items
      .sort(
        (left, right) =>
          Date.parse(right.timestamp) - Date.parse(left.timestamp),
      )
      .slice(0, 10)
  }, [auditItems, integrationItems])

  if (!workspaceID) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          Select a workspace from the sidebar to get started.
        </p>
      </div>
    )
  }

  const summaryItems = [
    {
      icon: Users,
      value: users.length,
      label: 'users',
      to: '/workspace/users',
      warn: false,
    },
    {
      icon: KeyRound,
      value: activeKeys.length,
      label: 'API keys',
      to: '/workspace/api-keys',
      warn: staleKeys.length > 0,
    },
  ] as const

  return (
    <div className="space-y-8">
      <div className="sys-panel">
        <div className="sys-panel-header">
          <span>Workspace_Overview</span>
          <span className="sys-tag">LIVE_STATE</span>
        </div>
        <div className="sys-panel-body">
          <Eyebrow>Overview</Eyebrow>
          <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
          {activeWorkspace?.name ?? 'Workspace'}
          </h1>
          <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
            Key metrics, access posture, and recent activity for the active
            workspace.
          </p>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-[var(--sys-home-grid-size)]">
        {summaryItems.map((item) => {
          const Icon = item.icon
          return (
            <Link
              key={item.label}
              to={item.to}
              className="group flex items-center gap-3 border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-4 py-3.5 no-underline transition-colors hover:bg-[var(--sys-home-accent-bg)] hover:text-[var(--sys-home-accent-fg)]"
            >
              <Icon className="h-4 w-4 flex-none text-[var(--sys-home-muted)] group-hover:text-[var(--sys-home-accent-fg)]" />
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="text-xl font-bold tabular-nums text-[var(--sys-home-fg)] group-hover:text-[var(--sys-home-accent-fg)]">
                    {item.value}
                  </span>
                  {item.warn ? (
                    <span className="h-1.5 w-1.5 rounded-full bg-[#ca8a04]" />
                  ) : null}
                </div>
                <span className="text-[11px] uppercase tracking-[0.06em] text-[var(--sys-home-muted)] group-hover:text-[var(--sys-home-accent-fg)]">
                  {item.label}
                </span>
              </div>
            </Link>
          )
        })}
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <InsightCard
          label="Access"
          to="/workspace/users"
          items={[
            `${delegatedUsers.length} users with delegated roles`,
            `${auditItems.length} recent auth events`,
          ]}
          status={delegatedUsers.length > 0 ? 'review' : 'ok'}
        />
        <InsightCard
          label="Automation"
          to="/workspace/api-keys"
          items={[
            `${staleKeys.length} unused active keys`,
            `${apiKeys.filter((k) => k.revoked).length} revoked keys`,
          ]}
          status={staleKeys.length > 0 ? 'review' : 'ok'}
        />
      </div>

      <div>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Recent activity
          </h2>
          <span className="text-[11px] uppercase tracking-[0.06em] text-[var(--sys-home-muted)]">
            {activeWorkspace?.name}
          </span>
        </div>

        {activity.length ? (
          <div className="border border-[var(--sys-home-border)]">
            {activity.map((item, index) => (
              <div
                key={item.id}
                className={`flex items-start gap-3 px-4 py-2.5 ${
                  index > 0 ? 'border-t border-[var(--sys-home-border)]' : ''
                }`}
              >
                <Badge
                  variant={item.kind === 'audit' ? 'default' : 'muted'}
                  className="mt-0.5 flex-none"
                >
                  {item.kind}
                </Badge>
                <div className="min-w-0 flex-1">
                  <span className="text-sm font-medium text-[var(--sys-home-fg)]">
                    {item.title}
                  </span>
                  <span className="ml-2 text-sm text-[var(--sys-home-muted)]">
                    {item.detail}
                  </span>
                </div>
                <div className="flex-none text-right">
                  <div className="text-xs text-[var(--sys-home-muted)]">
                    {item.actor}
                  </div>
                  <div className="text-[11px] text-[var(--sys-home-muted)]">
                    {formatDate(item.timestamp)}
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="border border-dashed border-[var(--sys-home-border)] px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
            No recent activity for this workspace.
          </div>
        )}
      </div>
    </div>
  )
}

function InsightCard({
  label,
  to,
  items,
  status,
}: {
  label: string
  to: string
  items: string[]
  status: 'ok' | 'review'
}) {
  return (
    <div className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
      <div className="flex items-center justify-between border-b border-[var(--sys-home-border)] px-4 py-2.5">
        <span className="text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          {label}
        </span>
        {status === 'review' ? (
          <span className="h-1.5 w-1.5 rounded-full bg-[#ca8a04]" />
        ) : (
          <span className="h-1.5 w-1.5 rounded-full bg-[#16a34a]" />
        )}
      </div>
      <div className="space-y-1.5 px-4 py-3">
        {items.map((item) => (
          <div key={item} className="text-[13px] text-[var(--sys-home-fg)]">
            {item}
          </div>
        ))}
      </div>
      <Link
        to={to}
        className="flex items-center gap-1.5 border-t border-[var(--sys-home-border)] px-4 py-2.5 text-[11px] font-bold uppercase tracking-[0.06em] text-[var(--sys-home-muted)] no-underline transition-colors hover:bg-[var(--sys-home-accent-bg)] hover:text-[var(--sys-home-accent-fg)]"
      >
        View details
        <ArrowRight className="h-3 w-3" />
      </Link>
    </div>
  )
}
