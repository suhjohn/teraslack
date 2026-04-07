import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { DashboardLoadingState } from '../../components/admin/dashboard-kit'
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
  formatDate,
  getDashboardWorkspaceParams,
  getErrorMessage,
  useAdmin,
} from '../../lib/admin'
import { useGetDashboardAudit } from '../../lib/openapi'
import type { DashboardAuditResponse } from '../../lib/openapi'

export const Route = createFileRoute('/settings/audit')({
  component: AuditPage,
})

function AuditPage() {
  const { activeWorkspace, workspaceID } = useAdmin()
  const [limit, setLimit] = useState(50)
  const params = { ...getDashboardWorkspaceParams(workspaceID), limit }

  const auditQuery = useGetDashboardAudit<DashboardAuditResponse>(params, {
    query: { retry: false, staleTime: 30_000 },
  })

  if (auditQuery.isPending) {
    return <DashboardLoadingState label="Loading events…" />
  }

  if (auditQuery.isError) {
    return (
      <div className="space-y-4">
        <PageHeader title="Events" description="Recent internal actor events." />
        <Alert variant="destructive">
          {getErrorMessage(auditQuery.error, 'Failed to load events.')}
        </Alert>
      </div>
    )
  }

  const items = auditQuery.data.items
  const showWorkspace = !workspaceID

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <PageHeader title="Events" description="Recent internal actor events." />
        <label className="flex items-center gap-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          Show
          <Select
            className="w-[8rem]"
            value={String(limit)}
            onChange={(event) => setLimit(Number(event.target.value))}
          >
            <option value="25">25</option>
            <option value="50">50</option>
            <option value="100">100</option>
            <option value="200">200</option>
          </Select>
        </label>
      </div>

      <Badge variant="muted">
        Scope {activeWorkspace ? activeWorkspace.name : 'All workspaces'}
      </Badge>

      <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
        {items.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Event</TableHead>
                <TableHead>Target</TableHead>
                <TableHead className="text-right">Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="align-top">
                    <div className="font-medium text-[var(--sys-home-fg)]">
                      {item.event_type}
                    </div>
                    {showWorkspace && item.workspace_id ? (
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
                  <TableCell className="text-right text-[var(--sys-home-muted)]">
                    {formatDate(item.created_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <div className="px-4 py-6 text-xs text-[var(--sys-home-muted)]">
            No events.
          </div>
        )}
      </section>
    </div>
  )
}

function PageHeader({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <div>
      <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
        {title}
      </h1>
      <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
        {description}
      </p>
    </div>
  )
}
