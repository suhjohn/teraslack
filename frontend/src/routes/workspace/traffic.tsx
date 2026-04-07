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
  formatPercent,
  getDashboardWorkspaceParams,
  getErrorMessage,
  useAdmin,
} from '../../lib/admin'
import { useGetDashboardTraffic } from '../../lib/openapi'
import type { DashboardTrafficResponse } from '../../lib/openapi'

export const Route = createFileRoute('/workspace/traffic')({
  component: TrafficPage,
})

function TrafficPage() {
  const { workspaceID } = useAdmin()
  const [days, setDays] = useState(14)
  const params = { ...getDashboardWorkspaceParams(workspaceID), days }

  const trafficQuery = useGetDashboardTraffic<DashboardTrafficResponse>(params, {
    query: { retry: false, staleTime: 30_000 },
  })

  if (trafficQuery.isPending && !trafficQuery.data) {
    return <DashboardLoadingState label="Loading traffic analytics…" />
  }

  if (!trafficQuery.data) {
    return (
      <div className="space-y-4">
        <DashboardHeader
          eyebrow="Traffic"
          title="API traffic"
          description="Request analytics could not be loaded."
          tag="ERROR"
        />
        <Alert variant="destructive">
          {getErrorMessage(trafficQuery.error, 'Failed to load traffic analytics.')}
        </Alert>
      </div>
    )
  }

  const response = trafficQuery.data
  const successRate =
    response.totals.requests > 0
      ? response.totals.success / response.totals.requests
      : 0

  return (
    <div className="space-y-8">
      <DashboardHeader
        eyebrow="Traffic"
        title="API traffic"
        description="Request volume, latency, endpoint mix, and key-level activity across the selected reporting window."
        tag={`${days}D`}
      />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <DashboardScopeBadge
            workspaceName={response.scope.workspace_name ?? null}
          />
          <Badge variant="muted">Success {formatPercent(successRate)}</Badge>
          <Badge variant="muted">
            Session {formatNumber(response.totals.session_requests)}
          </Badge>
          <Badge variant="muted">
            API key {formatNumber(response.totals.api_key_requests)}
          </Badge>
        </div>

        <label className="flex items-center gap-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          Window
          <Select
            className="w-[9rem]"
            value={String(days)}
            onChange={(event) => setDays(Number(event.target.value))}
          >
            <option value="7">7 days</option>
            <option value="14">14 days</option>
            <option value="30">30 days</option>
            <option value="90">90 days</option>
          </Select>
        </label>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <DashboardMetric
          label="Requests"
          value={formatNumber(response.totals.requests)}
          detail={`${formatNumber(response.totals.success)} successful`}
        />
        <DashboardMetric
          label="Client errors"
          value={formatNumber(response.totals.client_errors)}
          detail={`${formatNumber(response.totals.server_errors)} server errors`}
        />
        <DashboardMetric
          label="Rate limited"
          value={formatNumber(response.totals.rate_limited)}
          detail="HTTP 429 responses"
        />
        <DashboardMetric
          label="Latency"
          value={`${formatNumber(response.totals.p95_duration_ms)} ms`}
          detail={`avg ${formatNumber(response.totals.avg_duration_ms)} ms`}
        />
      </div>

      <DashboardSection
        title="Daily series"
        description="UTC day buckets across the selected reporting window."
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead className="text-right">Requests</TableHead>
              <TableHead className="text-right">Success</TableHead>
              <TableHead className="text-right">4xx</TableHead>
              <TableHead className="text-right">5xx</TableHead>
              <TableHead className="text-right">429</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[...response.series].reverse().map((point) => (
              <TableRow key={point.date}>
                <TableCell>{point.date}</TableCell>
                <TableCell className="text-right">{formatNumber(point.requests)}</TableCell>
                <TableCell className="text-right">{formatNumber(point.success)}</TableCell>
                <TableCell className="text-right">{formatNumber(point.client_errors)}</TableCell>
                <TableCell className="text-right">{formatNumber(point.server_errors)}</TableCell>
                <TableCell className="text-right">{formatNumber(point.rate_limited)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </DashboardSection>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,.95fr)]">
        <DashboardSection
          title="Top endpoints"
          description="Highest-volume normalized request paths in the current window."
        >
          {response.by_endpoint.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Endpoint</TableHead>
                  <TableHead className="text-right">Requests</TableHead>
                  <TableHead className="text-right">Success</TableHead>
                  <TableHead className="text-right">Latency</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {response.by_endpoint.map((endpoint) => (
                  <TableRow key={`${endpoint.method}-${endpoint.path}`}>
                    <TableCell className="align-top">
                      <div className="font-bold">{endpoint.method}</div>
                      <div className="break-all text-[11px] text-[var(--sys-home-muted)]">
                        {endpoint.path}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      {formatNumber(endpoint.requests)}
                    </TableCell>
                    <TableCell className="text-right">
                      {formatPercent(endpoint.success_rate)}
                    </TableCell>
                    <TableCell className="text-right text-[var(--sys-home-muted)]">
                      <div>p95 {formatNumber(endpoint.p95_duration_ms)} ms</div>
                      <div>avg {formatNumber(endpoint.avg_duration_ms)} ms</div>
                      {endpoint.last_seen_at ? (
                        <div>{formatDate(endpoint.last_seen_at)}</div>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
              No endpoint traffic recorded for this scope.
            </div>
          )}
        </DashboardSection>

        <DashboardSection
          title="API key activity"
          description="Most active credentials observed in request telemetry."
        >
          {response.by_key.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Key</TableHead>
                  <TableHead className="text-right">Requests</TableHead>
                  <TableHead className="text-right">Success</TableHead>
                  <TableHead className="text-right">Last seen</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {response.by_key.map((key) => (
                  <TableRow key={key.api_key_id}>
                    <TableCell className="align-top">
                      <div className="font-bold">{key.label}</div>
                      <div className="text-[11px] text-[var(--sys-home-muted)]">
                        {key.scope_type}
                        {key.scope_workspace_id ? `/${key.scope_workspace_id}` : ''}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">{formatNumber(key.requests)}</TableCell>
                    <TableCell className="text-right">{formatPercent(key.success_rate)}</TableCell>
                    <TableCell className="text-right text-[var(--sys-home-muted)]">
                      {key.last_request_at ? formatDate(key.last_request_at) : 'Never'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
              No API key traffic recorded in this window.
            </div>
          )}
        </DashboardSection>
      </div>

      <DashboardSection
        title="Status codes"
        description="Most common response codes captured in request telemetry."
      >
        {response.status_codes.length ? (
          <div className="flex flex-wrap gap-2 px-4 py-4">
            {response.status_codes.map((statusCode) => (
              <Badge key={statusCode.status_code} variant="muted">
                {statusCode.status_code} {formatNumber(statusCode.count)}
              </Badge>
            ))}
          </div>
        ) : (
          <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
            No status code data available yet.
          </div>
        )}
      </DashboardSection>
    </div>
  )
}
