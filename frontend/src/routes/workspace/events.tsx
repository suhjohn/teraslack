import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  ChevronDown,
  ChevronRight,
  LoaderCircle,
  Plus,
  RadioTower,
  RefreshCw,
  ToggleLeft,
  ToggleRight,
  Trash2,
  X,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { CodeBlock } from '../../components/ui/code-block'
import { Input } from '../../components/ui/input'
import { Select } from '../../components/ui/select'
import { formatDate, getErrorMessage, useAdmin } from '../../lib/admin'
import {
  EventResourceType,
  getListEventSubscriptionsQueryKey,
  useCreateEventSubscription,
  useDeleteEventSubscription,
  useListEventSubscriptions,
  useListEvents,
  useUpdateEventSubscription,
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
  const [error, setError] = useState('')

  const subscriptionsQuery =
    useListEventSubscriptions<EventSubscriptionsCollection>({
      query: { retry: false, staleTime: 30_000 },
    })
  const eventsQuery = useListEvents<ExternalEventsCollection>(
    { limit: 100 },
    { query: { retry: false, staleTime: 30_000 } },
  )

  const deleteMutation = useDeleteEventSubscription()
  const updateMutation = useUpdateEventSubscription()

  const subscriptions = useMemo(
    () =>
      (subscriptionsQuery.data?.items ?? [])
        .filter((subscription) => subscription.workspace_id === workspaceID)
        .sort(
          (left, right) =>
            Date.parse(right.updated_at) - Date.parse(left.updated_at),
        ),
    [subscriptionsQuery.data?.items, workspaceID],
  )
  const events = useMemo(
    () =>
      (eventsQuery.data?.items ?? [])
        .filter((event) => event.workspace_id === workspaceID)
        .sort(
          (left, right) =>
            Date.parse(right.occurred_at) - Date.parse(left.occurred_at),
        ),
    [eventsQuery.data?.items, workspaceID],
  )

  const enabledSubscriptions = subscriptions.filter(
    (subscription) => subscription.enabled,
  )
  const lastDayCutoff = Date.now() - 24 * 60 * 60 * 1000
  const eventsLastDay = events.filter(
    (event) => Date.parse(event.occurred_at) >= lastDayCutoff,
  )

  async function refreshSubscriptions() {
    await queryClient.invalidateQueries({
      queryKey: getListEventSubscriptionsQueryKey(),
    })
  }

  async function handleDelete(subscriptionId: string) {
    setError('')

    try {
      await deleteMutation.mutateAsync({ subscriptionId })
      await refreshSubscriptions()
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to delete subscription.'))
    }
  }

  async function handleToggle(subscription: EventSubscription) {
    setError('')

    try {
      await updateMutation.mutateAsync({
        subscriptionId: subscription.id,
        data: { enabled: !subscription.enabled },
      })
      await refreshSubscriptions()
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to update subscription.'))
    }
  }

  if (!workspaceID) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          Select a workspace to inspect deliveries and event flow.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <div className="sys-panel">
        <div className="sys-panel-header">
          <span>Event_Delivery</span>
          <span className="sys-tag">WEBHOOK_STATE</span>
        </div>
        <div className="sys-panel-body">
          <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Events
          </h1>
          <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
            Monitor webhook subscriptions and the external event feed that drives
            downstream delivery.
          </p>
        </div>
      </div>

      {error ? <Alert variant="destructive">{error}</Alert> : null}

      <div className="grid gap-4 md:grid-cols-3">
        <MetricCard
          label="Enabled subscriptions"
          value={enabledSubscriptions.length}
          detail={`${subscriptions.length} configured`}
        />
        <MetricCard
          label="Events last 24h"
          value={eventsLastDay.length}
          detail={
            events[0] ? `Latest ${formatDate(events[0].occurred_at)}` : 'No recent events'
          }
        />
        <MetricCard
          label="Tracked resources"
          value={new Set(events.map((event) => event.resource_id)).size}
          detail="Distinct resource IDs in the loaded feed"
        />
      </div>

      <div>
        <div className="mb-3 flex items-center justify-between">
          <div>
            <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
              Subscriptions
            </h2>
            <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
              Signed webhook endpoints scoped to the active workspace.
            </p>
          </div>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => void refreshSubscriptions()}
              disabled={subscriptionsQuery.isFetching}
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${
                  subscriptionsQuery.isFetching ? 'animate-spin' : ''
                }`}
              />
              Refresh
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5"
              onClick={() => setShowCreate((value) => !value)}
            >
              {showCreate ? <X className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
              {showCreate ? 'Cancel' : 'New'}
            </Button>
          </div>
        </div>

        {showCreate ? (
          <CreateSubscriptionForm
            workspaceID={workspaceID}
            onCancel={() => setShowCreate(false)}
            onCreated={async () => {
              setShowCreate(false)
              await refreshSubscriptions()
            }}
          />
        ) : null}

        {subscriptionsQuery.isFetching && !subscriptions.length ? (
          <div className="flex items-center justify-center py-8">
            <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
          </div>
        ) : subscriptions.length ? (
          <div className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
            {subscriptions.map((subscription, index) => (
              <SubscriptionRow
                key={subscription.id}
                subscription={subscription}
                index={index}
                onDelete={() => void handleDelete(subscription.id)}
                onToggle={() => void handleToggle(subscription)}
                mutating={deleteMutation.isPending || updateMutation.isPending}
              />
            ))}
          </div>
        ) : (
          <div className="border border-dashed border-[var(--sys-home-border)] px-4 py-6 text-xs text-[var(--sys-home-muted)]">
            No workspace subscriptions configured.
          </div>
        )}
      </div>

      <div>
        <div className="mb-3 flex items-center justify-between">
          <div>
            <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
              Event feed
            </h2>
            <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
              Recent externally visible events scoped to the active workspace.
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void eventsQuery.refetch()}
            disabled={eventsQuery.isFetching}
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${eventsQuery.isFetching ? 'animate-spin' : ''}`}
            />
            Refresh
          </Button>
        </div>

        {eventsQuery.isFetching && !events.length ? (
          <div className="flex items-center justify-center py-8">
            <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
          </div>
        ) : events.length ? (
          <div className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
            {events.map((event, index) => (
              <EventRow key={event.id} event={event} index={index} />
            ))}
          </div>
        ) : (
          <div className="border border-dashed border-[var(--sys-home-border)] px-4 py-6 text-xs text-[var(--sys-home-muted)]">
            No events found for this workspace.
          </div>
        )}
      </div>
    </div>
  )
}

function CreateSubscriptionForm({
  workspaceID,
  onCancel,
  onCreated,
}: {
  workspaceID: string
  onCancel: () => void
  onCreated: () => Promise<void>
}) {
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [eventType, setEventType] = useState('')
  const [resourceType, setResourceType] = useState('')
  const [resourceID, setResourceID] = useState('')
  const [error, setError] = useState('')
  const createMutation = useCreateEventSubscription()

  async function handleCreate() {
    const trimmedUrl = url.trim()
    const trimmedSecret = secret.trim()
    const trimmedEventType = eventType.trim()
    const trimmedResourceType = resourceType.trim()
    const trimmedResourceID = resourceID.trim()

    if (!trimmedUrl || !trimmedSecret) {
      setError('Webhook URL and signing secret are required.')
      return
    }

    if (trimmedResourceID && !trimmedResourceType) {
      setError('Choose a resource type before setting a resource ID.')
      return
    }

    setError('')

    try {
      await createMutation.mutateAsync({
        data: {
          workspace_id: workspaceID,
          url: trimmedUrl,
          secret: trimmedSecret,
          event_type: trimmedEventType || null,
          resource_type: trimmedResourceType
            ? (trimmedResourceType as EventResourceType)
            : null,
          resource_id: trimmedResourceID || null,
        },
      })
      setUrl('')
      setSecret('')
      setEventType('')
      setResourceType('')
      setResourceID('')
      await onCreated()
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create subscription.'))
    }
  }

  return (
    <section className="mb-4 border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
      <div className="grid gap-4 md:grid-cols-2">
        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)] md:col-span-2">
          <span>Webhook URL</span>
          <Input value={url} onChange={(event) => setUrl(event.target.value)} />
        </label>

        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Signing secret</span>
          <Input value={secret} onChange={(event) => setSecret(event.target.value)} />
        </label>

        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Event type</span>
          <Input
            value={eventType}
            onChange={(event) => setEventType(event.target.value)}
            placeholder="message.created"
          />
        </label>

        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Resource type</span>
          <Select
            value={resourceType}
            onChange={(event) => setResourceType(event.target.value)}
          >
            <option value="">All resources</option>
            {Object.values(EventResourceType).map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </Select>
        </label>

        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Resource ID</span>
          <Input
            value={resourceID}
            onChange={(event) => setResourceID(event.target.value)}
            placeholder="Optional"
          />
        </label>
      </div>

      {error ? (
        <Alert variant="destructive" className="mt-4">
          {error}
        </Alert>
      ) : null}

      <div className="mt-5 flex gap-3">
        <Button onClick={() => void handleCreate()} disabled={createMutation.isPending}>
          {createMutation.isPending ? 'Creating…' : 'Create subscription'}
        </Button>
        <Button variant="outline" onClick={onCancel} disabled={createMutation.isPending}>
          Cancel
        </Button>
      </div>
    </section>
  )
}

function SubscriptionRow({
  subscription,
  index,
  onDelete,
  onToggle,
  mutating,
}: {
  subscription: EventSubscription
  index: number
  onDelete: () => void
  onToggle: () => void
  mutating: boolean
}) {
  return (
    <div
      className={`flex items-start gap-4 px-4 py-4 ${
        index > 0 ? 'border-t border-[var(--sys-home-border)]' : ''
      }`}
    >
      <div className="mt-0.5">
        <RadioTower
          className={`h-4 w-4 ${
            subscription.enabled ? 'text-[#16a34a]' : 'text-[var(--sys-home-muted)]'
          }`}
        />
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium text-[var(--sys-home-fg)]">
            {subscription.url}
          </span>
          <Badge variant={subscription.enabled ? 'success' : 'muted'}>
            {subscription.enabled ? 'enabled' : 'disabled'}
          </Badge>
          {subscription.event_type ? <Badge variant="muted">{subscription.event_type}</Badge> : null}
          {subscription.resource_type ? (
            <Badge variant="muted">
              {subscription.resource_type}
              {subscription.resource_id ? `/${subscription.resource_id}` : ''}
            </Badge>
          ) : null}
        </div>

        <div className="mt-2 flex flex-wrap gap-3 text-[11px] text-[var(--sys-home-muted)]">
          <span>created {formatDate(subscription.created_at)}</span>
          <span>updated {formatDate(subscription.updated_at)}</span>
        </div>
      </div>

      <div className="flex gap-2">
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8"
          onClick={onToggle}
          disabled={mutating}
          title={subscription.enabled ? 'Disable subscription' : 'Enable subscription'}
        >
          {subscription.enabled ? (
            <ToggleRight className="h-4 w-4" />
          ) : (
            <ToggleLeft className="h-4 w-4" />
          )}
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 text-[var(--sys-home-muted)] hover:text-[#dc2626]"
          onClick={onDelete}
          disabled={mutating}
          title="Delete subscription"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
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
    <div className={index > 0 ? 'border-t border-[var(--sys-home-border)]' : ''}>
      <button
        type="button"
        className="flex w-full items-start gap-4 px-4 py-4 text-left hover:bg-[var(--accent-faint)]"
        onClick={() => setExpanded((value) => !value)}
      >
        <div className="mt-0.5">
          {expanded ? (
            <ChevronDown className="h-4 w-4 text-[var(--sys-home-muted)]" />
          ) : (
            <ChevronRight className="h-4 w-4 text-[var(--sys-home-muted)]" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium text-[var(--sys-home-fg)]">
              {event.type}
            </span>
            <Badge variant="muted">{event.resource_type}</Badge>
          </div>
          <div className="mt-1 text-xs text-[var(--sys-home-muted)]">
            {event.resource_id}
          </div>
        </div>
        <div className="text-right text-[11px] text-[var(--sys-home-muted)]">
          {formatDate(event.occurred_at)}
        </div>
      </button>

      {expanded ? (
        <div className="border-t border-[var(--sys-home-border)] px-4 py-4">
          <CodeBlock className="text-xs">
            {JSON.stringify(event.payload, null, 2)}
          </CodeBlock>
        </div>
      ) : null}
    </div>
  )
}

function MetricCard({
  label,
  value,
  detail,
}: {
  label: string
  value: number
  detail: string
}) {
  return (
    <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-4 py-4">
      <div className="text-[11px] uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
        {label}
      </div>
      <div className="mt-2 text-3xl font-bold tabular-nums text-[var(--sys-home-fg)]">
        {value}
      </div>
      <div className="mt-2 text-xs text-[var(--sys-home-muted)]">{detail}</div>
    </section>
  )
}
