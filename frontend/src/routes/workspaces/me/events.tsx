import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { Alert } from '../../../components/ui/alert'
import { Badge } from '../../../components/ui/badge'
import { Select } from '../../../components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../../../components/ui/table'
import { formatDate, getErrorMessage } from '../../../lib/admin'
import { useGetDashboardAudit } from '../../../lib/openapi'
import type { DashboardAuditResponse } from '../../../lib/openapi'

export const Route = createFileRoute('/workspaces/me/events')({
  component: MeEventsRoute,
})

function MeEventsRoute() {
  const [limit, setLimit] = useState(50)

  const auditQuery = useGetDashboardAudit<DashboardAuditResponse>(
    { limit },
    { query: { retry: false, staleTime: 30_000 } },
  )

  return (
    <div className="space-y-6 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Events
          </h1>
          <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
            Recent events across all workspaces.
          </p>
        </div>
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

      <Badge variant="muted">All workspaces</Badge>

      {auditQuery.isPending ? (
        <div className="px-4 py-12 text-center text-xs uppercase tracking-[0.06em] text-[var(--sys-home-muted)]">
          Loading events…
        </div>
      ) : auditQuery.isError ? (
        <Alert variant="destructive">
          {getErrorMessage(auditQuery.error, 'Failed to load events.')}
        </Alert>
      ) : (
        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
          {auditQuery.data.items.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Event</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead className="text-right">Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {auditQuery.data.items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="align-top">
                      <div className="font-medium text-[var(--sys-home-fg)]">
                        {item.event_type}
                      </div>
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
      )}
    </div>
  )
}
