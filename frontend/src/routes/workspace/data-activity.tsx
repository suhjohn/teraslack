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
import { useGetDashboardDataActivity } from '../../lib/openapi'
import type { DashboardDataActivityResponse } from '../../lib/openapi'

export const Route = createFileRoute('/workspace/data-activity')({
  component: DataActivityPage,
})

function DataActivityPage() {
  const { workspaceID } = useAdmin()
  const [days, setDays] = useState(14)
  const params = { ...getDashboardWorkspaceParams(workspaceID), days }

  const activityQuery = useGetDashboardDataActivity<DashboardDataActivityResponse>(
    params,
    { query: { retry: false, staleTime: 30_000 } },
  )

  if (activityQuery.isPending && !activityQuery.data) {
    return <DashboardLoadingState label="Loading data activity…" />
  }

  if (!activityQuery.data) {
    return (
      <div className="space-y-4">
        <DashboardHeader
          eyebrow="Data"
          title="Data activity"
          description="Conversation and message activity could not be loaded."
          tag="ERROR"
        />
        <Alert variant="destructive">
          {getErrorMessage(activityQuery.error, 'Failed to load data activity.')}
        </Alert>
      </div>
    )
  }

  const response = activityQuery.data

  return (
    <div className="space-y-8">
      <DashboardHeader
        eyebrow="Data"
        title="Data activity"
        description="Conversation creation, message volume, event publishing, room mix, and top active rooms for the current scope."
        tag={`${days}D`}
      />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <DashboardScopeBadge
            workspaceName={response.scope.workspace_name ?? null}
          />
          <Badge variant="muted">
            Conversations {formatNumber(response.summary.conversations)}
          </Badge>
          <Badge variant="muted">
            Events 24h {formatNumber(response.summary.recent_events_24h)}
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
          label="Conversations"
          value={formatNumber(response.summary.conversations)}
          detail={`${formatNumber(response.summary.member_conversations)} member-only`}
        />
        <DashboardMetric
          label="Messages 7d"
          value={formatNumber(response.summary.messages_7d)}
          detail={`${formatNumber(response.summary.broadcast_conversations)} broadcast conversations`}
        />
        <DashboardMetric
          label="Events 24h"
          value={formatNumber(response.summary.recent_events_24h)}
          detail="Visible external event volume"
        />
        <DashboardMetric
          label="Top rooms"
          value={formatNumber(response.top_conversations.length)}
          detail="Highest message count in the selected window"
        />
      </div>

      <DashboardSection
        title="Daily series"
        description="UTC day buckets for conversation creation, message creation, and external event publishing."
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead className="text-right">Conversations</TableHead>
              <TableHead className="text-right">Messages</TableHead>
              <TableHead className="text-right">Events</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[...response.series].reverse().map((point) => (
              <TableRow key={point.date}>
                <TableCell>{point.date}</TableCell>
                <TableCell className="text-right">
                  {formatNumber(point.conversations_created)}
                </TableCell>
                <TableCell className="text-right">
                  {formatNumber(point.messages_created)}
                </TableCell>
                <TableCell className="text-right">
                  {formatNumber(point.events_published)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </DashboardSection>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,.75fr)_minmax(0,1.25fr)]">
        <DashboardSection
          title="Room mix"
          description="Visible conversation categories derived from workspace scope and access policy."
        >
          {response.room_mix.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Type</TableHead>
                  <TableHead className="text-right">Count</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {response.room_mix.map((bucket) => (
                  <TableRow key={bucket.label}>
                    <TableCell>{bucket.label}</TableCell>
                    <TableCell className="text-right">{formatNumber(bucket.count)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
              No conversations visible in this scope.
            </div>
          )}
        </DashboardSection>

        <DashboardSection
          title="Top conversations"
          description="Rooms with the highest message count during the selected window."
        >
          {response.top_conversations.length ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Conversation</TableHead>
                  <TableHead className="text-right">Participants</TableHead>
                  <TableHead className="text-right">Messages</TableHead>
                  <TableHead className="text-right">Last message</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {response.top_conversations.map((conversation) => (
                  <TableRow key={conversation.conversation_id}>
                    <TableCell className="align-top">
                      <div className="font-bold">
                        {conversation.title || 'Untitled conversation'}
                      </div>
                      <div className="text-[11px] text-[var(--sys-home-muted)]">
                        {conversation.access_policy}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      {formatNumber(conversation.participant_count)}
                    </TableCell>
                    <TableCell className="text-right">
                      {formatNumber(conversation.message_count)}
                    </TableCell>
                    <TableCell className="text-right text-[var(--sys-home-muted)]">
                      {conversation.last_message_at
                        ? formatDate(conversation.last_message_at)
                        : 'No messages'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
              No conversation activity recorded in this window.
            </div>
          )}
        </DashboardSection>
      </div>
    </div>
  )
}
