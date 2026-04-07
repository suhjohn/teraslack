import { createFileRoute } from '@tanstack/react-router'
import {
  LoaderCircle,
  Plus,
  RadioTower,
  ToggleLeft,
  ToggleRight,
  Trash2,
  X,
} from 'lucide-react'
import { useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Input } from '../../components/ui/input'
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
import {
  EventResourceType,
  useCreateEventSubscription,
  useDeleteEventSubscription,
  useGetDashboardWebhooks,
  useUpdateEventSubscription,
} from '../../lib/openapi'
import type {
  DashboardWebhookDelivery,
  DashboardWebhookSubscription,
  DashboardWebhooksResponse,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspace/events')({
  component: WebhooksPage,
})

function WebhooksPage() {
  const { workspaceID } = useAdmin()
  const params = { ...getDashboardWorkspaceParams(workspaceID), limit: 25 }
  const [showCreate, setShowCreate] = useState(false)
  const [mutationError, setMutationError] = useState('')

  const webhooksQuery = useGetDashboardWebhooks<DashboardWebhooksResponse>(
    params,
    { query: { retry: false, staleTime: 30_000 } },
  )

  const deleteMutation = useDeleteEventSubscription()
  const updateMutation = useUpdateEventSubscription()

  async function refresh() {
    await webhooksQuery.refetch()
  }

  async function handleDelete(subscriptionId: string) {
    setMutationError('')

    try {
      await deleteMutation.mutateAsync({ subscriptionId })
      await refresh()
    } catch (error) {
      setMutationError(getErrorMessage(error, 'Failed to delete subscription.'))
    }
  }

  async function handleToggle(subscription: DashboardWebhookSubscription) {
    setMutationError('')

    try {
      await updateMutation.mutateAsync({
        subscriptionId: subscription.subscription_id,
        data: { enabled: !subscription.enabled },
      })
      await refresh()
    } catch (error) {
      setMutationError(getErrorMessage(error, 'Failed to update subscription.'))
    }
  }

  if (webhooksQuery.isPending && !webhooksQuery.data) {
    return <DashboardLoadingState label="Loading webhook state…" />
  }

  if (!webhooksQuery.data) {
    return (
      <div className="space-y-4">
        <DashboardHeader
          eyebrow="Webhooks"
          title="Delivery health"
          description="Webhook subscription inventory and delivery outcomes could not be loaded."
          tag="ERROR"
        />
        <Alert variant="destructive">
          {getErrorMessage(webhooksQuery.error, 'Failed to load webhook state.')}
        </Alert>
      </div>
    )
  }

  const response = webhooksQuery.data
  const summary = response.summary

  return (
    <div className="space-y-8">
      <DashboardHeader
        eyebrow="Webhooks"
        title="Delivery health"
        description="Subscription inventory, retry pressure, and recent delivery outcomes for outbound webhooks."
        tag="LIVE"
      />

      <div className="flex flex-wrap items-center gap-2">
        <DashboardScopeBadge
          workspaceName={response.scope.workspace_name ?? null}
        />
        <Badge variant="muted">
          Delivered 24h {formatNumber(summary.delivered_24h)}
        </Badge>
        <Badge variant="muted">
          Failed 24h {formatNumber(summary.failed_24h)}
        </Badge>
      </div>

      {mutationError ? <Alert variant="destructive">{mutationError}</Alert> : null}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <DashboardMetric
          label="Subscriptions"
          value={formatNumber(summary.subscriptions)}
          detail={`${formatNumber(summary.enabled_subscriptions)} enabled`}
        />
        <DashboardMetric
          label="Pending deliveries"
          value={formatNumber(summary.pending_deliveries)}
          detail="Backlog waiting for a worker attempt"
        />
        <DashboardMetric
          label="Failed deliveries"
          value={formatNumber(summary.failed_deliveries)}
          detail="Current failed delivery records"
        />
        <DashboardMetric
          label="Delivered 24h"
          value={formatNumber(summary.delivered_24h)}
          detail={`${formatNumber(summary.failed_24h)} failed in the same window`}
        />
      </div>

      <DashboardSection
        title="Subscriptions"
        description="Create, disable, or retire signed webhook endpoints."
        action={
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => void refresh()}
              disabled={webhooksQuery.isFetching}
            >
              <LoaderCircle
                className={`h-3.5 w-3.5 ${
                  webhooksQuery.isFetching ? 'animate-spin' : 'hidden'
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
        }
      >
        {showCreate ? (
          <CreateSubscriptionForm
            workspaceID={workspaceID}
            onCancel={() => setShowCreate(false)}
            onCreated={async () => {
              setShowCreate(false)
              await refresh()
            }}
          />
        ) : null}

        {response.subscriptions.length ? (
          <div>
            {response.subscriptions.map((subscription, index) => (
              <SubscriptionRow
                key={subscription.subscription_id}
                subscription={subscription}
                index={index}
                onDelete={() => void handleDelete(subscription.subscription_id)}
                onToggle={() => void handleToggle(subscription)}
                mutating={deleteMutation.isPending || updateMutation.isPending}
              />
            ))}
          </div>
        ) : (
          <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
            No webhook subscriptions configured for this scope.
          </div>
        )}
      </DashboardSection>

      <DashboardSection
        title="Recent deliveries"
        description="Most recent delivery attempts ordered by delivery time."
      >
        {response.recent_deliveries.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Status</TableHead>
                <TableHead>Subscription</TableHead>
                <TableHead>Event</TableHead>
                <TableHead className="text-right">Attempt</TableHead>
                <TableHead className="text-right">Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {response.recent_deliveries.map((delivery) => (
                <DeliveryRow key={delivery.delivery_id} delivery={delivery} />
              ))}
            </TableBody>
          </Table>
        ) : (
          <div className="px-4 py-8 text-center text-xs text-[var(--sys-home-muted)]">
            No delivery records available yet.
          </div>
        )}
      </DashboardSection>
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
    const trimmedURL = url.trim()
    const trimmedSecret = secret.trim()
    const trimmedEventType = eventType.trim()
    const trimmedResourceType = resourceType.trim()
    const trimmedResourceID = resourceID.trim()

    if (!trimmedURL || !trimmedSecret) {
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
          workspace_id: workspaceID || null,
          url: trimmedURL,
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
    } catch (createError) {
      setError(getErrorMessage(createError, 'Failed to create subscription.'))
    }
  }

  return (
    <section className="border-b border-[var(--sys-home-border)] px-4 py-4">
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
            placeholder="conversation.message.created"
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
  subscription: DashboardWebhookSubscription
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
          {subscription.last_status ? (
            <Badge
              variant={
                subscription.last_status === 'failed'
                  ? 'destructive'
                  : subscription.last_status === 'delivered'
                    ? 'success'
                    : 'warning'
              }
            >
              {subscription.last_status}
            </Badge>
          ) : null}
          {subscription.event_type ? (
            <Badge variant="muted">{subscription.event_type}</Badge>
          ) : null}
          {subscription.resource_type ? (
            <Badge variant="muted">
              {subscription.resource_type}
              {subscription.resource_id ? `/${subscription.resource_id}` : ''}
            </Badge>
          ) : null}
        </div>

        <div className="mt-2 flex flex-wrap gap-3 text-[11px] text-[var(--sys-home-muted)]">
          <span>{formatNumber(subscription.total_deliveries)} deliveries</span>
          <span>{formatNumber(subscription.failed_count)} failed</span>
          <span>{formatNumber(subscription.pending_count)} pending</span>
          {subscription.last_delivery_at ? (
            <span>last {formatDate(subscription.last_delivery_at)}</span>
          ) : null}
        </div>

        {subscription.last_error ? (
          <div className="mt-2 text-xs text-[#dc2626]">{subscription.last_error}</div>
        ) : null}
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

function DeliveryRow({
  delivery,
}: {
  delivery: DashboardWebhookDelivery
}) {
  return (
    <TableRow>
      <TableCell>
        <Badge
          variant={
            delivery.status === 'failed'
              ? 'destructive'
              : delivery.status === 'delivered'
                ? 'success'
                : 'warning'
          }
        >
          {delivery.status}
        </Badge>
      </TableCell>
      <TableCell className="align-top">
        <div className="max-w-[22rem] truncate text-[var(--sys-home-fg)]">
          {delivery.url}
        </div>
        <div className="text-[11px] text-[var(--sys-home-muted)]">
          {delivery.subscription_id}
        </div>
      </TableCell>
      <TableCell className="align-top">
        <div>{delivery.event_type}</div>
        <div className="text-[11px] text-[var(--sys-home-muted)]">
          {delivery.resource_type}/{delivery.resource_id}
        </div>
        {delivery.last_error ? (
          <div className="mt-1 text-[11px] text-[#dc2626]">{delivery.last_error}</div>
        ) : null}
      </TableCell>
      <TableCell className="text-right">{formatNumber(delivery.attempt_count)}</TableCell>
      <TableCell className="text-right text-[var(--sys-home-muted)]">
        {formatDate(delivery.delivered_at ?? delivery.created_at)}
      </TableCell>
    </TableRow>
  )
}
