import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { LoaderCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { CodeBlock } from '../../components/ui/code-block'
import { Input } from '../../components/ui/input'
import { formatDate, getErrorMessage, useAdmin } from '../../lib/admin'
import {
  APIKeyScopeType,
  WorkspaceMemberRole,
  WorkspaceMemberStatus,
  getGetWorkspaceQueryKey,
  getListWorkspacesQueryKey,
  useCreateWorkspace,
  useGetWorkspace,
  useListApiKeys,
  useListEventSubscriptions,
  useListEvents,
  useListWorkspaceMembers,
  useUpdateWorkspace,
} from '../../lib/openapi'
import type {
  APIKeysCollection,
  EventSubscriptionsCollection,
  ExternalEventsCollection,
  Workspace,
  WorkspaceMembersCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspace/settings')({
  validateSearch: (search: Record<string, unknown>) => ({
    create:
      search.create === true ||
      search.create === 'true' ||
      search.create === '1',
  }),
  component: BillingMonitorPage,
})

function BillingMonitorPage() {
  const navigate = useNavigate({ from: '/workspace/settings' })
  const search = Route.useSearch()
  const { workspaceID, selectWorkspace } = useAdmin()

  const workspaceQuery = useGetWorkspace<Workspace>(workspaceID, {
    query: { enabled: !!workspaceID, retry: false, staleTime: 30_000 },
  })
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
    { limit: 100 },
    { query: { retry: false, staleTime: 30_000 } },
  )

  if (search.create) {
    return (
      <CreateWorkspacePanel
        onCancel={() =>
          void navigate({
            to: '/workspace/settings',
            search: { create: false },
            replace: true,
          })
        }
        onCreated={async (nextWorkspaceID) => {
          await selectWorkspace(nextWorkspaceID)
          void navigate({
            to: '/workspace/settings',
            search: { create: false },
            replace: true,
          })
        }}
      />
    )
  }

  if (!workspaceID) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          No workspace is selected.{' '}
          <Link
            to="/workspace/settings"
            search={{ create: true }}
            className="text-[var(--ink)] underline underline-offset-4"
          >
            Create a workspace
          </Link>{' '}
          to start tracking API usage.
        </p>
      </div>
    )
  }

  if (workspaceQuery.isFetching && !workspaceQuery.data) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <span className="inline-flex items-center gap-2 text-sm text-[var(--ink-soft)]">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          Loading billing monitor…
        </span>
      </div>
    )
  }

  const workspace = workspaceQuery.data

  if (!workspace) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          The selected workspace could not be loaded.
        </p>
      </div>
    )
  }

  const members = membersQuery.data?.items ?? []
  const keys = keysQuery.data?.items ?? []
  const subscriptions = subscriptionsQuery.data?.items ?? []
  const allEvents = eventsQuery.data?.items ?? []

  const usage = useMemo(() => {
    const activeMembers = members.filter(
      (member) => member.status === WorkspaceMemberStatus.active,
    )
    const invitedMembers = members.filter(
      (member) => member.status === WorkspaceMemberStatus.invited,
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
    const activeKeys = workspaceKeys.filter((key) => !key.revoked_at)
    const expiringSoon = activeKeys.filter((key) => {
      if (!key.expires_at) return false
      const expiration = Date.parse(key.expires_at)
      return expiration >= Date.now() && expiration <= Date.now() + 30 * 86400_000
    })
    const workspaceSubscriptions = subscriptions.filter(
      (subscription) => subscription.workspace_id === workspaceID,
    )
    const enabledSubscriptions = workspaceSubscriptions.filter(
      (subscription) => subscription.enabled,
    )
    const workspaceEvents = allEvents
      .filter((event) => event.workspace_id === workspaceID)
      .sort(
        (left, right) =>
          Date.parse(right.occurred_at) - Date.parse(left.occurred_at),
      )
    const eventsLast7Days = workspaceEvents.filter(
      (event) => Date.parse(event.occurred_at) >= Date.now() - 7 * 86400_000,
    )

    return {
      activeMembers,
      invitedMembers,
      owners,
      admins,
      workspaceKeys,
      activeKeys,
      expiringSoon,
      workspaceSubscriptions,
      enabledSubscriptions,
      workspaceEvents,
      eventsLast7Days,
    }
  }, [allEvents, keys, members, subscriptions, workspaceID])

  return (
    <div className="space-y-8">
      <div className="sys-panel">
        <div className="sys-panel-header">
          <span>Billing_Monitor</span>
          <span className="sys-tag">DERIVED_SIGNALS</span>
        </div>
        <div className="sys-panel-body">
          <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            {workspace.name}
          </h1>
          <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
            Workspace usage, delivery footprint, and the inputs that matter for
            API billing posture.
          </p>
        </div>
      </div>

      <Alert>
        The current OpenAPI surface does not expose invoices, plan state, or
        spend totals. This page tracks the usage signals that exist today.
      </Alert>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          label="Active members"
          value={usage.activeMembers.length}
          detail={`${usage.owners.length} owners, ${usage.admins.length} admins`}
        />
        <MetricCard
          label="Workspace keys"
          value={usage.activeKeys.length}
          detail={`${usage.expiringSoon.length} expiring in 30d`}
        />
        <MetricCard
          label="Subscriptions"
          value={usage.enabledSubscriptions.length}
          detail={`${usage.workspaceSubscriptions.length} configured`}
        />
        <MetricCard
          label="Events last 7d"
          value={usage.eventsLast7Days.length}
          detail={
            usage.workspaceEvents[0]
              ? `Latest ${formatDate(usage.workspaceEvents[0].occurred_at)}`
              : 'No events yet'
          }
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,.95fr)]">
        <WorkspaceProfileCard key={workspace.id} workspace={workspace} />
        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                Usage snapshot
              </h2>
              <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
                Current billing inputs derived from the supported API.
              </p>
            </div>
            <Badge variant="muted">derived</Badge>
          </div>
          <CodeBlock className="mt-4 text-xs">
            {JSON.stringify(
              {
                workspace_id: workspace.id,
                slug: workspace.slug,
                active_members: usage.activeMembers.length,
                invited_members: usage.invitedMembers.length,
                workspace_keys: usage.activeKeys.length,
                subscriptions_enabled: usage.enabledSubscriptions.length,
                subscriptions_total: usage.workspaceSubscriptions.length,
                events_last_7_days: usage.eventsLast7Days.length,
                latest_event_at: usage.workspaceEvents[0]?.occurred_at ?? null,
              },
              null,
              2,
            )}
          </CodeBlock>
        </section>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Membership footprint
          </h2>
          <div className="mt-4 space-y-3">
            <FootprintRow
              label="Owners"
              value={usage.owners.map(getMemberLabel).join(', ') || 'None'}
            />
            <FootprintRow
              label="Admins"
              value={usage.admins.map(getMemberLabel).join(', ') || 'None'}
            />
            <FootprintRow
              label="Invites pending"
              value={String(usage.invitedMembers.length)}
            />
            <FootprintRow
              label="Workspace created"
              value={formatDate(workspace.created_at)}
            />
          </div>
        </section>

        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Delivery footprint
          </h2>
          <div className="mt-4 space-y-3">
            <FootprintRow
              label="Workspace-scoped keys"
              value={String(usage.workspaceKeys.length)}
            />
            <FootprintRow
              label="Enabled subscriptions"
              value={String(usage.enabledSubscriptions.length)}
            />
            <FootprintRow
              label="Recent events"
              value={String(usage.workspaceEvents.slice(0, 20).length)}
            />
            <FootprintRow
              label="Last update"
              value={formatDate(workspace.updated_at)}
            />
          </div>
        </section>
      </div>
    </div>
  )
}

function WorkspaceProfileCard({ workspace }: { workspace: Workspace }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState(workspace.name)
  const [slug, setSlug] = useState(workspace.slug)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const updateWorkspace = useUpdateWorkspace()

  async function handleSave() {
    const trimmedName = name.trim()
    const trimmedSlug = slug.trim()

    if (!trimmedName || !trimmedSlug) {
      setError('Workspace name and slug are required.')
      setSaved(false)
      return
    }

    setError('')
    setSaved(false)

    try {
      await updateWorkspace.mutateAsync({
        workspaceId: workspace.id,
        data: {
          name: trimmedName,
          slug: trimmedSlug,
        },
      })
      await queryClient.invalidateQueries({
        queryKey: getGetWorkspaceQueryKey(workspace.id),
      })
      await queryClient.invalidateQueries({
        queryKey: getListWorkspacesQueryKey(),
      })
      setSaved(true)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to update workspace.'))
    }
  }

  return (
    <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Workspace profile
          </h2>
          <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
            Keep the workspace identity used by clients and operators aligned.
          </p>
        </div>
        {saved ? <Badge variant="success">saved</Badge> : null}
      </div>

      {error ? (
        <Alert variant="destructive" className="mt-4">
          {error}
        </Alert>
      ) : null}

      <div className="mt-4 grid gap-4 md:grid-cols-2">
        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Name</span>
          <Input value={name} onChange={(event) => setName(event.target.value)} />
        </label>
        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Slug</span>
          <Input value={slug} onChange={(event) => setSlug(event.target.value)} />
        </label>
      </div>

      <div className="mt-4 flex flex-wrap items-center gap-3 text-xs text-[var(--sys-home-muted)]">
        <span>Created {formatDate(workspace.created_at)}</span>
        <span>Updated {formatDate(workspace.updated_at)}</span>
      </div>

      <div className="mt-5">
        <Button onClick={() => void handleSave()} disabled={updateWorkspace.isPending}>
          {updateWorkspace.isPending ? 'Saving…' : 'Save workspace'}
        </Button>
      </div>
    </section>
  )
}

function CreateWorkspacePanel({
  onCancel,
  onCreated,
}: {
  onCancel: () => void
  onCreated: (workspaceID: string) => Promise<void>
}) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [error, setError] = useState('')
  const createWorkspace = useCreateWorkspace()

  async function handleCreate() {
    const trimmedName = name.trim()
    const trimmedSlug = slug.trim()

    if (!trimmedName || !trimmedSlug) {
      setError('Workspace name and slug are required.')
      return
    }

    setError('')

    try {
      const created = await createWorkspace.mutateAsync({
        data: {
          name: trimmedName,
          slug: trimmedSlug,
        },
      })
      await queryClient.invalidateQueries({
        queryKey: getListWorkspacesQueryKey(),
      })
      await onCreated(created.id)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create workspace.'))
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6 py-6">
      <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-6">
        <h1 className="text-lg font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
          Create workspace
        </h1>
        <p className="mt-2 text-sm text-[var(--sys-home-muted)]">
          Start with a minimal tenant record and use the dashboard to monitor
          API activity as it grows.
        </p>

        {error ? (
          <Alert variant="destructive" className="mt-4">
            {error}
          </Alert>
        ) : null}

        <div className="mt-5 grid gap-4">
          <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
            <span>Name</span>
            <Input value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
            <span>Slug</span>
            <Input value={slug} onChange={(event) => setSlug(event.target.value)} />
          </label>
        </div>

        <div className="mt-6 flex gap-3">
          <Button onClick={() => void handleCreate()} disabled={createWorkspace.isPending}>
            {createWorkspace.isPending ? 'Creating…' : 'Create workspace'}
          </Button>
          <Button variant="outline" onClick={onCancel} disabled={createWorkspace.isPending}>
            Cancel
          </Button>
        </div>
      </section>
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

function FootprintRow({
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
      <span className="max-w-[70%] text-right text-sm text-[var(--sys-home-fg)]">
        {value}
      </span>
    </div>
  )
}

function getMemberLabel(member: WorkspaceMembersCollection['items'][number]) {
  return member.user.profile.display_name || member.user.profile.handle || member.user_id
}
