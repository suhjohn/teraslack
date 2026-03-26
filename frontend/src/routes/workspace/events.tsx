import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { ChevronDown, ChevronRight, LoaderCircle, Plus, Trash2, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { CodeBlock } from '../../components/ui/code-block'
import { Input } from '../../components/ui/input'
import { useAdmin, formatDate, getErrorMessage } from '../../lib/admin'
import {
  getListEventSubscriptionsQueryKey,
  useCreateEventSubscription,
  useDeleteEventSubscription,
  useListEventSubscriptions,
  useListEvents,
} from '../../lib/openapi'
import type {
  EventSubscription,
  EventSubscriptionsCollection,
  ExternalEvent,
  ExternalEventsCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspace/events')({
  component: EventsPage,
})

function EventsPage() {
  const { workspaceID } = useAdmin()
  const queryClient = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const deleteMutation = useDeleteEventSubscription()

  const subscriptionsQuery =
    useListEventSubscriptions<EventSubscriptionsCollection>(
      { workspace_id: workspaceID },
      { query: { enabled: !!workspaceID, retry: false } },
    )

  const eventsQuery = useListEvents<ExternalEventsCollection>(
    { limit: 50 },
    { query: { retry: false } },
  )

  const subscriptions: EventSubscription[] =
    subscriptionsQuery.data?.items ?? []
  const events = useMemo(
    () =>
      (eventsQuery.data?.items ?? [])
        .filter((e) => e.workspace_id === workspaceID)
        .sort(
          (a, b) =>
            Date.parse(b.occurred_at) - Date.parse(a.occurred_at),
        ),
    [eventsQuery.data?.items, workspaceID],
  )

  async function handleDelete(id: string) {
    setDeleteError('')
    try {
      await deleteMutation.mutateAsync({ id })
      await queryClient.invalidateQueries({
        queryKey: getListEventSubscriptionsQueryKey({ workspace_id: workspaceID }),
      })
    } catch (err) {
      setDeleteError(getErrorMessage(err, 'Failed to delete subscription.'))
    }
  }

  return (
    <div className="space-y-8">
      {/* Subscriptions */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <div>
            <h2 className="text-sm font-bold text-[var(--ink)]">
              Subscriptions
            </h2>
            <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
              Webhook endpoints that receive events for this workspace.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="gap-1.5"
            onClick={() => setShowCreate((p) => !p)}
          >
            {showCreate ? (
              <X className="h-3.5 w-3.5" />
            ) : (
              <Plus className="h-3.5 w-3.5" />
            )}
            {showCreate ? 'Cancel' : 'New'}
          </Button>
        </div>

        {showCreate ? (
          <CreateSubscriptionForm
            workspaceID={workspaceID}
            onDone={() => {
              setShowCreate(false)
              void queryClient.invalidateQueries({
                queryKey: getListEventSubscriptionsQueryKey({
                  workspace_id: workspaceID,
                }),
              })
            }}
            onCancel={() => setShowCreate(false)}
          />
        ) : null}

        {deleteError ? <Alert className="mb-3">{deleteError}</Alert> : null}

        {subscriptionsQuery.isFetching && !subscriptions.length ? (
          <div className="flex items-center justify-center py-8">
            <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
          </div>
        ) : subscriptions.length ? (
          <div className="border border-[var(--line)]">
            {subscriptions.map((sub, index) => (
              <SubscriptionRow
                key={sub.id}
                sub={sub}
                index={index}
                onDelete={() => void handleDelete(sub.id)}
                deleting={deleteMutation.isPending}
              />
            ))}
          </div>
        ) : (
          <p className="py-4 text-xs text-[var(--ink-soft)]">
            No subscriptions configured. Create one to start receiving webhook
            events.
          </p>
        )}
      </div>

      {/* Event feed */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <div>
            <h2 className="text-sm font-bold text-[var(--ink)]">Event feed</h2>
            <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
              Recent external events for this workspace — the source that drives
              webhook delivery.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => void eventsQuery.refetch()}
            disabled={eventsQuery.isFetching}
          >
            {eventsQuery.isFetching ? (
              <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
            ) : (
              'Refresh'
            )}
          </Button>
        </div>

        {eventsQuery.isFetching && !events.length ? (
          <div className="flex items-center justify-center py-8">
            <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
          </div>
        ) : events.length ? (
          <div className="border border-[var(--line)]">
            {events.map((event, index) => (
              <EventRow key={event.id} event={event} index={index} />
            ))}
          </div>
        ) : (
          <p className="py-4 text-xs text-[var(--ink-soft)]">
            No events found for this workspace.
          </p>
        )}
      </div>
    </div>
  )
}

function SubscriptionRow({
  sub,
  index,
  onDelete,
  deleting,
}: {
  sub: EventSubscription
  index: number
  onDelete: () => void
  deleting: boolean
}) {
  return (
    <div
      className={`flex items-start gap-3 px-4 py-3 ${
        index > 0 ? 'border-t border-[var(--line)]' : ''
      }`}
    >
      <div className="mt-0.5 flex-none">
        {sub.enabled ? (
          <span className="inline-block h-2 w-2 rounded-full bg-[#16a34a]" />
        ) : (
          <span className="inline-block h-2 w-2 rounded-full bg-[var(--line)]" />
        )}
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="truncate font-mono text-xs text-[var(--ink)]">
          {sub.url}
        </div>
        <div className="flex flex-wrap gap-2">
          {sub.type ? (
            <span className="text-[11px] text-[var(--ink-soft)]">
              type: <span className="font-mono text-[var(--ink)]">{sub.type}</span>
            </span>
          ) : (
            <span className="text-[11px] text-[var(--ink-soft)]">all events</span>
          )}
          {sub.resource_type ? (
            <span className="text-[11px] text-[var(--ink-soft)]">
              resource:{' '}
              <span className="font-mono text-[var(--ink)]">
                {sub.resource_type}
                {sub.resource_id ? `/${sub.resource_id}` : '/*'}
              </span>
            </span>
          ) : null}
          <span className="text-[11px] text-[var(--ink-soft)]">
            created {formatDate(sub.created_at)}
          </span>
        </div>
      </div>
      <Button
        variant="ghost"
        size="icon"
        className="h-7 w-7 flex-none text-[var(--ink-soft)] hover:text-[#dc2626]"
        onClick={onDelete}
        disabled={deleting}
        title="Delete subscription"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}

function EventRow({
  event,
  index,
}: {
  event: ExternalEvent
  index: number
}) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div className={index > 0 ? 'border-t border-[var(--line)]' : ''}>
      <button
        type="button"
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left hover:bg-[var(--accent-faint)]"
        onClick={() => setExpanded((p) => !p)}
      >
        <span className="flex-none text-[var(--ink-soft)]">
          {expanded ? (
            <ChevronDown className="h-3.5 w-3.5" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5" />
          )}
        </span>
        <span className="min-w-0 flex-1 truncate font-mono text-xs font-medium text-[var(--ink)]">
          {event.type}
        </span>
        <Badge variant="muted" className="flex-none">
          {event.resource_type}
        </Badge>
        <span className="flex-none font-mono text-[11px] text-[var(--ink-soft)]">
          {event.resource_id}
        </span>
        <span className="flex-none text-[11px] text-[var(--ink-soft)]">
          {formatDate(event.occurred_at)}
        </span>
      </button>
      {expanded ? (
        <div className="border-t border-[var(--line)] bg-[var(--surface)] px-4 py-3">
          <CodeBlock className="max-h-[320px] overflow-auto text-xs">
            {JSON.stringify(event.payload, null, 2)}
          </CodeBlock>
        </div>
      ) : null}
    </div>
  )
}

function CreateSubscriptionForm({
  workspaceID,
  onDone,
  onCancel,
}: {
  workspaceID: string
  onDone: () => void
  onCancel: () => void
}) {
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [type, setType] = useState('')
  const [error, setError] = useState('')
  const createMutation = useCreateEventSubscription()

  async function handleCreate() {
    if (!url.trim() || !secret.trim()) return
    setError('')
    try {
      await createMutation.mutateAsync({
        data: {
          workspace_id: workspaceID,
          url: url.trim(),
          secret: secret.trim(),
          type: type.trim() || undefined,
        },
      })
      onDone()
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create subscription.'))
    }
  }

  return (
    <div className="mb-3 border border-[var(--line)] bg-[var(--surface)] px-4 py-4">
      <div className="mb-3 text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
        New subscription
      </div>
      {error ? <Alert className="mb-3">{error}</Alert> : null}
      <div className="space-y-2">
        <div className="grid gap-2 sm:grid-cols-2">
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="Webhook URL"
            autoFocus
          />
          <Input
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder="Signing secret"
            type="password"
          />
        </div>
        <Input
          value={type}
          onChange={(e) => setType(e.target.value)}
          placeholder="Event type filter — e.g. conversation.message.created (leave blank for all)"
        />
        <div className="flex gap-2 pt-1">
          <Button
            size="sm"
            onClick={() => void handleCreate()}
            disabled={createMutation.isPending || !url.trim() || !secret.trim()}
          >
            {createMutation.isPending ? 'Creating…' : 'Create'}
          </Button>
          <Button variant="ghost" size="sm" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </div>
    </div>
  )
}
