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
  WorkspaceMembershipSummaryRole,
  WorkspaceMembershipSummaryStatus,
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
  AuthMeResponse,
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
  const { workspaceID, selectWorkspace, auth } = useAdmin()

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

  const workspace = workspaceQuery.data ?? null
  const members = membersQuery.data?.items ?? []
  const keys = keysQuery.data?.items ?? []
  const subscriptions = subscriptionsQuery.data?.items ?? []
  const allEvents = eventsQuery.data?.items ?? []
  const memberships = Array.isArray(auth.workspaces) ? auth.workspaces : []

  const userUsage = useMemo(() => {
    const activeMemberships = memberships.filter(
      (membership) => membership.status === WorkspaceMembershipSummaryStatus.active,
    )
    const invitedMemberships = memberships.filter(
      (membership) => membership.status === WorkspaceMembershipSummaryStatus.invited,
    )
    const ownerMemberships = activeMemberships.filter(
      (membership) => membership.role === WorkspaceMembershipSummaryRole.owner,
    )
    const adminMemberships = activeMemberships.filter(
      (membership) => membership.role === WorkspaceMembershipSummaryRole.admin,
    )
    const personalKeys = keys.filter(
      (key) =>
        key.scope_type === APIKeyScopeType.user &&
        !key.revoked_at,
    )
    const visibleWorkspaceKeys = keys.filter(
      (key) =>
        key.scope_type === APIKeyScopeType.workspace &&
        !key.revoked_at,
    )
    const enabledSubscriptions = subscriptions.filter(
      (subscription) => subscription.enabled,
    )
    const visibleEvents = [...allEvents].sort(
      (left, right) =>
        Date.parse(right.occurred_at) - Date.parse(left.occurred_at),
    )
    const eventsLast7Days = visibleEvents.filter(
      (event) => Date.parse(event.occurred_at) >= Date.now() - 7 * 86400_000,
    )

    return {
      activeMemberships,
      invitedMemberships,
      ownerMemberships,
      adminMemberships,
      personalKeys,
      visibleWorkspaceKeys,
      enabledSubscriptions,
      visibleEvents,
      eventsLast7Days,
    }
  }, [allEvents, keys, memberships, subscriptions])

  const workspaceUsage = useMemo(() => {
    if (!workspaceID) {
      return null
    }

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

  const userLabel =
    auth.user.profile.display_name ||
    auth.user.profile.handle ||
    auth.user.email ||
    auth.user.id

  return (
    <div className="space-y-10">
      <div className="sys-panel">
        <div className="sys-panel-header">
          <span>Billing_Monitor</span>
          <span className="sys-tag">DERIVED_SIGNALS</span>
        </div>
        <div className="sys-panel-body">
          <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Billing scopes
          </h1>
          <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
            Derived billing signals for the current operator and, when selected,
            the active workspace.
          </p>
        </div>
      </div>

      <Alert>
        The current OpenAPI surface does not expose invoices, plan state, or
        spend totals. This page tracks the user-level and workspace-level usage
        signals that exist today.
      </Alert>

      <section className="space-y-8">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
              User scope
            </h2>
            <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
              Billing signals tied to the current operator across every visible
              workspace.
            </p>
          </div>
          <Badge variant="muted">{userLabel}</Badge>
        </div>

        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <MetricCard
            label="Active workspaces"
            value={userUsage.activeMemberships.length}
            detail={`${userUsage.ownerMemberships.length} owned, ${userUsage.adminMemberships.length} admin`}
          />
          <MetricCard
            label="Personal keys"
            value={userUsage.personalKeys.length}
            detail={`${userUsage.visibleWorkspaceKeys.length} workspace keys visible`}
          />
          <MetricCard
            label="Enabled subscriptions"
            value={userUsage.enabledSubscriptions.length}
            detail={`${subscriptions.length} visible across accessible workspaces`}
          />
          <MetricCard
            label="Events last 7d"
            value={userUsage.eventsLast7Days.length}
            detail={
              userUsage.visibleEvents[0]
                ? `Latest ${formatDate(userUsage.visibleEvents[0].occurred_at)}`
                : 'No visible events yet'
            }
          />
        </div>

        <div className="grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,.95fr)]">
          <UserProfileCard
            auth={auth}
            activeWorkspaceID={workspaceID}
            activeWorkspaceName={workspace?.name ?? null}
          />
          <SnapshotCard
            title="User usage snapshot"
            description="Current billing inputs derived for the signed-in operator."
            payload={{
              user_id: auth.user.id,
              handle: auth.user.profile.handle || null,
              display_name: auth.user.profile.display_name || null,
              email: auth.user.email ?? null,
              active_workspaces: userUsage.activeMemberships.length,
              invited_workspaces: userUsage.invitedMemberships.length,
              owner_workspaces: userUsage.ownerMemberships.length,
              admin_workspaces: userUsage.adminMemberships.length,
              personal_keys: userUsage.personalKeys.length,
              workspace_keys_visible: userUsage.visibleWorkspaceKeys.length,
              subscriptions_enabled: userUsage.enabledSubscriptions.length,
              subscriptions_total: subscriptions.length,
              events_last_7_days: userUsage.eventsLast7Days.length,
              latest_event_at: userUsage.visibleEvents[0]?.occurred_at ?? null,
              selected_workspace_id: workspaceID || null,
            }}
          />
        </div>

        <div className="grid gap-4 lg:grid-cols-2">
          <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
            <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
              Workspace access footprint
            </h2>
            <div className="mt-4 space-y-3">
              <FootprintRow
                label="Owned workspaces"
                value={userUsage.ownerMemberships.map((membership) => membership.name).join(', ') || 'None'}
              />
              <FootprintRow
                label="Admin workspaces"
                value={userUsage.adminMemberships.map((membership) => membership.name).join(', ') || 'None'}
              />
              <FootprintRow
                label="Invites pending"
                value={String(userUsage.invitedMemberships.length)}
              />
              <FootprintRow
                label="Current handle"
                value={auth.user.profile.handle || 'Not set'}
              />
            </div>
          </section>

          <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
            <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
              Credential + delivery footprint
            </h2>
            <div className="mt-4 space-y-3">
              <FootprintRow
                label="Personal keys"
                value={String(userUsage.personalKeys.length)}
              />
              <FootprintRow
                label="Workspace keys visible"
                value={String(userUsage.visibleWorkspaceKeys.length)}
              />
              <FootprintRow
                label="Enabled subscriptions"
                value={String(userUsage.enabledSubscriptions.length)}
              />
              <FootprintRow
                label="Recent visible events"
                value={String(userUsage.visibleEvents.slice(0, 20).length)}
              />
            </div>
          </section>
        </div>
      </section>

      <section className="space-y-8">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
              Workspace scope
            </h2>
            <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
              Billing signals tied to the currently selected workspace.
            </p>
          </div>
          {workspace ? <Badge variant="muted">{workspace.name}</Badge> : null}
        </div>

        {!workspaceID ? (
          <EmptyWorkspaceBillingState />
        ) : workspaceQuery.isFetching && !workspace ? (
          <div className="flex min-h-[20vh] items-center justify-center border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
            <span className="inline-flex items-center gap-2 text-sm text-[var(--ink-soft)]">
              <LoaderCircle className="h-4 w-4 animate-spin" />
              Loading workspace billing…
            </span>
          </div>
        ) : !workspace || !workspaceUsage ? (
          <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-6">
            <p className="text-sm text-[var(--ink-soft)]">
              The selected workspace could not be loaded.
            </p>
          </section>
        ) : (
          <>
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <MetricCard
                label="Active members"
                value={workspaceUsage.activeMembers.length}
                detail={`${workspaceUsage.owners.length} owners, ${workspaceUsage.admins.length} admins`}
              />
              <MetricCard
                label="Workspace keys"
                value={workspaceUsage.activeKeys.length}
                detail={`${workspaceUsage.expiringSoon.length} expiring in 30d`}
              />
              <MetricCard
                label="Subscriptions"
                value={workspaceUsage.enabledSubscriptions.length}
                detail={`${workspaceUsage.workspaceSubscriptions.length} configured`}
              />
              <MetricCard
                label="Events last 7d"
                value={workspaceUsage.eventsLast7Days.length}
                detail={
                  workspaceUsage.workspaceEvents[0]
                    ? `Latest ${formatDate(workspaceUsage.workspaceEvents[0].occurred_at)}`
                    : 'No events yet'
                }
              />
            </div>

            <div className="grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,.95fr)]">
              <WorkspaceProfileCard key={workspace.id} workspace={workspace} />
              <SnapshotCard
                title="Workspace usage snapshot"
                description="Current billing inputs derived for the active workspace."
                payload={{
                  workspace_id: workspace.id,
                  slug: workspace.slug,
                  active_members: workspaceUsage.activeMembers.length,
                  invited_members: workspaceUsage.invitedMembers.length,
                  workspace_keys: workspaceUsage.activeKeys.length,
                  subscriptions_enabled: workspaceUsage.enabledSubscriptions.length,
                  subscriptions_total: workspaceUsage.workspaceSubscriptions.length,
                  events_last_7_days: workspaceUsage.eventsLast7Days.length,
                  latest_event_at: workspaceUsage.workspaceEvents[0]?.occurred_at ?? null,
                }}
              />
            </div>

            <div className="grid gap-4 lg:grid-cols-2">
              <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
                <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                  Membership footprint
                </h2>
                <div className="mt-4 space-y-3">
                  <FootprintRow
                    label="Owners"
                    value={workspaceUsage.owners.map(getMemberLabel).join(', ') || 'None'}
                  />
                  <FootprintRow
                    label="Admins"
                    value={workspaceUsage.admins.map(getMemberLabel).join(', ') || 'None'}
                  />
                  <FootprintRow
                    label="Invites pending"
                    value={String(workspaceUsage.invitedMembers.length)}
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
                    value={String(workspaceUsage.workspaceKeys.length)}
                  />
                  <FootprintRow
                    label="Enabled subscriptions"
                    value={String(workspaceUsage.enabledSubscriptions.length)}
                  />
                  <FootprintRow
                    label="Recent events"
                    value={String(workspaceUsage.workspaceEvents.slice(0, 20).length)}
                  />
                  <FootprintRow
                    label="Last update"
                    value={formatDate(workspace.updated_at)}
                  />
                </div>
              </section>
            </div>
          </>
        )}
      </section>
    </div>
  )
}

function UserProfileCard({
  auth,
  activeWorkspaceID,
  activeWorkspaceName,
}: {
  auth: AuthMeResponse
  activeWorkspaceID: string
  activeWorkspaceName: string | null
}) {
  return (
    <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            User profile
          </h2>
          <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
            Operator identity and current workspace selection used for billing views.
          </p>
        </div>
        <Badge variant="muted">{auth.user.principal_type}</Badge>
      </div>

      <div className="mt-4 space-y-3">
        <FootprintRow
          label="Display name"
          value={auth.user.profile.display_name || 'Not set'}
        />
        <FootprintRow
          label="Handle"
          value={auth.user.profile.handle || 'Not set'}
        />
        <FootprintRow
          label="Email"
          value={auth.user.email ?? 'Not set'}
        />
        <FootprintRow
          label="Selected workspace"
          value={activeWorkspaceName ?? (activeWorkspaceID || 'None selected')}
        />
      </div>
    </section>
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

function SnapshotCard({
  title,
  description,
  payload,
}: {
  title: string
  description: string
  payload: Record<string, unknown>
}) {
  return (
    <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            {title}
          </h2>
          <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
            {description}
          </p>
        </div>
        <Badge variant="muted">derived</Badge>
      </div>
      <CodeBlock className="mt-4 text-xs">
        {JSON.stringify(payload, null, 2)}
      </CodeBlock>
    </section>
  )
}

function EmptyWorkspaceBillingState() {
  return (
    <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-6">
      <h3 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
        No workspace selected
      </h3>
      <p className="mt-2 text-sm text-[var(--sys-home-muted)]">
        User-level billing signals are available above. Create a workspace to unlock
        tenant-level billing, membership, and delivery posture.
      </p>
      <div className="mt-5">
        <Link
          to="/workspace/settings"
          search={{ create: true }}
          className="sys-command-button no-underline"
        >
          Create workspace
        </Link>
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
      if (created.status !== 201) {
        throw new Error('Failed to create workspace.')
      }
      await queryClient.invalidateQueries({
        queryKey: getListWorkspacesQueryKey(),
      })
      await onCreated(created.data.id)
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
