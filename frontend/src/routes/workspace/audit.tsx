import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Select } from '../../components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../../components/ui/table'
import {
  DashboardHeader,
  DashboardLoadingState,
  DashboardMetric,
  DashboardScopeBadge,
  DashboardSection,
} from '../../components/admin/dashboard-kit'
import {
  formatDate,
  formatNumber,
  getDashboardWorkspaceParams,
  getErrorMessage,
  useAdmin,
} from '../../lib/admin'
import { useGetDashboardAudit } from '../../lib/openapi'
import type { DashboardAuditResponse } from '../../lib/openapi'

export const Route = createFileRoute('/workspace/audit')({
  component: AuditPage,
})

function AuditPage() {
  const { workspaceID } = useAdmin()
  const [limit, setLimit] = useState(50)
  const params = { ...getDashboardWorkspaceParams(workspaceID), limit }

  const auditQuery = useGetDashboardAudit<DashboardAuditResponse>(params, {
    query: { retry: false, staleTime: 30_000 },
  })

  if (auditQuery.isPending && !auditQuery.data) {
    return <DashboardLoadingState label="Loading audit trail…" />
  }

  if (!auditQuery.data) {
    return (
      <div className="space-y-4">
        <DashboardHeader
          eyebrow="Audit"
          title="Audit trail"
          description="Internal actor events could not be loaded."
          tag="ERROR"
        />
        <Alert variant="destructive">
          {getErrorMessage(auditQuery.error, 'Failed to load audit trail.')}
        </Alert>
      </div>
    )
  }

  const response = auditQuery.data

  return (
    <div className="space-y-8">
      <DashboardHeader
        eyebrow="Audit"
        title="Audit trail"
        description="Recent internal lifecycle events initiated by the current user, with optional workspace scoping."
        tag={`${limit}`}
      />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <DashboardScopeBadge
            workspaceName={response.scope.workspace_name ?? null}
          />
          <Badge variant="muted">
            Events {formatNumber(response.items.length)}
          </Badge>
          <Badge variant="muted">
            Types {formatNumber(response.top_types.length)}
          </Badge>
        </div>

        <label className="flex items-center gap-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          Limit
          <Select
            className="w-[9rem]"
            value={String(limit)}
            onChange={(event) => setLimit(Number(event.target.value))}
          >
            <option value="25">25 items</option>
            <option value="50">50 items</option>
            <option value="100">100 items</option>
            <option value="200">200 items</option>
          </Select>
        </label>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <DashboardMetric
          label="Loaded events"
          value={formatNumber(response.items.length)}
          detail="Returned in the current page"
        />
        <DashboardMetric
          label="Top type"
          value={response.top_types[0]?.label ?? 'None'}
          detail={
            response.top_types[0]
              ? `${formatNumber(response.top_types[0].count)} occurrences`
              : 'No event types recorded'
          }
        />
        <DashboardMetric
          label="Workspace scoped"
          value={response.scope.workspace_name ? 'Yes' : 'No'}
          detail={
            response.scope.workspace_name
              ? response.scope.workspace_name
              : 'Showing all visible workspaces'
          }
        />
        <DashboardMetric
          label="Actor"
          value="Current user"
          detail="Audit entries are filtered to the authenticated actor"
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,.7fr)_minmax(0,1.3fr)]">
        <DashboardSection
          title="Top event types"
          description="Most frequent internal event types in the current result set."
        >
          {response.top_types.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Type</TableHead>
                  <TableHead className="text-right">Count</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {response.top_types.map((bucket) => (
                  <TableRow key={bucket.label}>
                    <TableCell>{bucket.label}</TableCell>
                    <TableCell className="text-right">{formatNumber(bucket.count)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
              No audit types recorded.
            </div>
          )}
        </DashboardSection>

        <DashboardSection
          title="Event log"
          description="Most recent internal events for the authenticated actor."
        >
          {response.items.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Event</TableHead>
                  <TableHead>Aggregate</TableHead>
                  <TableHead>Payload</TableHead>
                  <TableHead className="text-right">Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {response.items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="align-top">
                      <div className="font-bold">{item.event_type}</div>
                      {item.workspace_id ? (
                        <div className="text-[11px] text-[var(--sys-home-muted)]">
                          {item.workspace_id}
                        </div>
                      ) : null}
                    </TableCell>
                    <TableCell className="align-top">
                      <div>{item.aggregate_type}</div>
                      <div className="break-all text-[11px] text-[var(--sys-home-muted)]">
                        {item.aggregate_id}
                      </div>
                    </TableCell>
                    <TableCell className="align-top text-[11px] text-[var(--sys-home-muted)]">
                      {summarizePayload(item.payload)}
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
              No audit events recorded for this scope.
            </div>
          )}
        </DashboardSection>
      </div>
    </div>
  )
}

function summarizePayload(payload: Record<string, unknown>) {
  const json = JSON.stringify(payload)
  if (json.length <= 140) {
    return json
  }
  return `${json.slice(0, 137)}...`
}
