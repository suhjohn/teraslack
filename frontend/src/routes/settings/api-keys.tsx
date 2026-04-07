import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { CalendarClock, KeyRound, LoaderCircle, Plus, Trash2, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { CodeBlock } from '../../components/ui/code-block'
import { Input } from '../../components/ui/input'
import { Select } from '../../components/ui/select'
import { formatDate, getErrorMessage, useAdmin } from '../../lib/admin'
import {
  APIKeyScopeType,
  CreateAPIKeyRequestScopeType,
  getListApiKeysQueryKey,
  useCreateApiKey,
  useDeleteApiKey,
  useListApiKeys,
} from '../../lib/openapi'
import type {
  APIKey,
  APIKeysCollection,
  WorkspacesCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/settings/api-keys')({
  component: ApiKeysPage,
})

function ApiKeysPage() {
  const { workspaceID, workspaces } = useAdmin()
  const queryClient = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [error, setError] = useState('')
  const [createdSecret, setCreatedSecret] = useState('')

  const keysQuery = useListApiKeys<APIKeysCollection>({
    query: { retry: false, staleTime: 30_000 },
  })
  const deleteMutation = useDeleteApiKey()

  const keys = keysQuery.data?.items ?? []
  const workspaceByID = useMemo(
    () => new Map(workspaces.map((workspace) => [workspace.id, workspace.name])),
    [workspaces],
  )

  const activeKeys = keys.filter((key) => !key.revoked_at)
  const workspaceKeys = activeKeys.filter(
    (key) =>
      key.scope_type === APIKeyScopeType.workspace &&
      key.scope_workspace_id === workspaceID,
  )
  const personalKeys = activeKeys.filter(
    (key) => key.scope_type === APIKeyScopeType.user,
  )
  const revokedKeys = keys.filter((key) => !!key.revoked_at)

  async function handleDelete(keyId: string) {
    setError('')

    try {
      await deleteMutation.mutateAsync({ keyId })
      await queryClient.invalidateQueries({
        queryKey: getListApiKeysQueryKey(),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to revoke API key.'))
    }
  }

  return (
    <div className="space-y-8">
      <div className="sys-panel">
        <div className="sys-panel-header">
          <span>Api_Access</span>
          <span className="sys-tag">LIVE_KEYS</span>
        </div>
        <div className="sys-panel-body">
          <h1 className="text-xl font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            API keys
          </h1>
          <p className="mt-1 text-[13px] text-[var(--sys-home-muted)]">
            Issue workspace-scoped credentials, keep personal operator keys in
            view, and revoke anything stale.
          </p>
        </div>
      </div>

      {error ? <Alert variant="destructive">{error}</Alert> : null}

      {createdSecret ? (
        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                New secret
              </h2>
              <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
                This secret is shown once. Rotate or recreate the key if it is
                lost.
              </p>
            </div>
            <Button variant="ghost" size="sm" onClick={() => setCreatedSecret('')}>
              Dismiss
            </Button>
          </div>
          <CodeBlock className="mt-4">{createdSecret}</CodeBlock>
        </section>
      ) : null}

      <div className="grid gap-4 md:grid-cols-3">
        <MetricCard
          label="Workspace keys"
          value={workspaceKeys.length}
          detail={
            workspaceID
              ? 'Scoped to the active workspace'
              : 'Select a workspace to create scoped keys'
          }
        />
        <MetricCard
          label="Personal keys"
          value={personalKeys.length}
          detail="Visible to the current operator"
        />
        <MetricCard
          label="Revoked"
          value={revokedKeys.length}
          detail="Retained here for auditability"
        />
      </div>

      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
            Manage credentials
          </h2>
          <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
            The dashboard is intentionally narrow: create, inspect, revoke.
          </p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="gap-1.5"
          onClick={() => setShowCreate((value) => !value)}
        >
          {showCreate ? <X className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
          {showCreate ? 'Cancel' : 'New key'}
        </Button>
      </div>

      {showCreate ? (
        <CreateKeyForm
          defaultWorkspaceID={workspaceID}
          workspaces={workspaces}
          onCancel={() => setShowCreate(false)}
          onCreated={async (secret) => {
            setCreatedSecret(secret)
            setShowCreate(false)
            await queryClient.invalidateQueries({
              queryKey: getListApiKeysQueryKey(),
            })
          }}
        />
      ) : null}

      <KeySection
        title="Workspace-scoped"
        description="Keys limited to the active workspace."
        keys={workspaceKeys}
        workspaceByID={workspaceByID}
        onRevoke={(keyId) => void handleDelete(keyId)}
        revoking={deleteMutation.isPending}
        empty="No active workspace keys."
      />

      <KeySection
        title="Personal"
        description="Global operator keys visible in this session."
        keys={personalKeys}
        workspaceByID={workspaceByID}
        onRevoke={(keyId) => void handleDelete(keyId)}
        revoking={deleteMutation.isPending}
        empty="No active personal keys."
      />

      {revokedKeys.length ? (
        <KeySection
          title="Revoked"
          description="Credentials that have already been retired."
          keys={revokedKeys}
          workspaceByID={workspaceByID}
          empty="No revoked keys."
        />
      ) : null}

      {keysQuery.isFetching && !keys.length ? (
        <div className="flex items-center justify-center py-8">
          <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
        </div>
      ) : null}
    </div>
  )
}

function CreateKeyForm({
  defaultWorkspaceID,
  workspaces,
  onCancel,
  onCreated,
}: {
  defaultWorkspaceID: string
  workspaces: WorkspacesCollection['items']
  onCancel: () => void
  onCreated: (secret: string) => Promise<void>
}) {
  const [label, setLabel] = useState('')
  const [scopeType, setScopeType] = useState<CreateAPIKeyRequestScopeType>(
    defaultWorkspaceID
      ? CreateAPIKeyRequestScopeType.workspace
      : CreateAPIKeyRequestScopeType.user,
  )
  const [scopeWorkspaceID, setScopeWorkspaceID] = useState(defaultWorkspaceID)
  const [expiresAt, setExpiresAt] = useState('')
  const [error, setError] = useState('')
  const createMutation = useCreateApiKey()

  async function handleCreate() {
    const trimmedLabel = label.trim()

    if (!trimmedLabel) {
      setError('Key label is required.')
      return
    }

    if (
      scopeType === CreateAPIKeyRequestScopeType.workspace &&
      !scopeWorkspaceID
    ) {
      setError('Select a workspace for workspace-scoped keys.')
      return
    }

    setError('')

    try {
      const created = await createMutation.mutateAsync({
        data: {
          label: trimmedLabel,
          scope_type: scopeType,
          scope_workspace_id:
            scopeType === CreateAPIKeyRequestScopeType.workspace
              ? scopeWorkspaceID
              : null,
          expires_at: expiresAt ? new Date(expiresAt).toISOString() : null,
        },
      })
      if (created.status !== 201) {
        throw new Error('Failed to create API key.')
      }
      setLabel('')
      setExpiresAt('')
      await onCreated(created.data.secret)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create API key.'))
    }
  }

  return (
    <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
      <div className="grid gap-4 md:grid-cols-2">
        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Label</span>
          <Input value={label} onChange={(event) => setLabel(event.target.value)} />
        </label>

        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Scope</span>
          <Select
            value={scopeType}
            onChange={(event) =>
              setScopeType(event.target.value as CreateAPIKeyRequestScopeType)
            }
          >
            <option value={CreateAPIKeyRequestScopeType.user}>Personal</option>
            <option value={CreateAPIKeyRequestScopeType.workspace}>Workspace</option>
          </Select>
        </label>

        {scopeType === CreateAPIKeyRequestScopeType.workspace ? (
          <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
            <span>Workspace</span>
            <Select
              value={scopeWorkspaceID}
              onChange={(event) => setScopeWorkspaceID(event.target.value)}
            >
              <option value="">Select workspace</option>
              {workspaces.map((workspace) => (
                <option key={workspace.id} value={workspace.id}>
                  {workspace.name}
                </option>
              ))}
            </Select>
          </label>
        ) : null}

        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Expires at</span>
          <Input
            type="datetime-local"
            value={expiresAt}
            onChange={(event) => setExpiresAt(event.target.value)}
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
          {createMutation.isPending ? 'Creating…' : 'Create key'}
        </Button>
        <Button variant="outline" onClick={onCancel} disabled={createMutation.isPending}>
          Cancel
        </Button>
      </div>
    </section>
  )
}

function KeySection({
  title,
  description,
  keys,
  workspaceByID,
  onRevoke,
  revoking,
  empty,
}: {
  title: string
  description: string
  keys: APIKey[]
  workspaceByID: Map<string, string>
  onRevoke?: (keyId: string) => void
  revoking?: boolean
  empty: string
}) {
  return (
    <section>
      <div className="mb-3">
        <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
          {title}
        </h2>
        <p className="mt-1 text-xs text-[var(--sys-home-muted)]">{description}</p>
      </div>

      {keys.length ? (
        <div className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
          {keys.map((key, index) => (
            <KeyRow
              key={key.id}
              apiKey={key}
              workspaceByID={workspaceByID}
              index={index}
              onRevoke={onRevoke}
              revoking={revoking}
            />
          ))}
        </div>
      ) : (
        <div className="border border-dashed border-[var(--sys-home-border)] px-4 py-6 text-xs text-[var(--sys-home-muted)]">
          {empty}
        </div>
      )}
    </section>
  )
}

function KeyRow({
  apiKey,
  workspaceByID,
  index,
  onRevoke,
  revoking,
}: {
  apiKey: APIKey
  workspaceByID: Map<string, string>
  index: number
  onRevoke?: (keyId: string) => void
  revoking?: boolean
}) {
  const isWorkspaceScoped = apiKey.scope_type === APIKeyScopeType.workspace
  const scopeLabel = isWorkspaceScoped
    ? workspaceByID.get(apiKey.scope_workspace_id ?? '') ??
      apiKey.scope_workspace_id ??
      'Workspace'
    : 'Personal'

  return (
    <div
      className={`flex items-start gap-4 px-4 py-4 ${
        index > 0 ? 'border-t border-[var(--sys-home-border)]' : ''
      }`}
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-semibold text-[var(--sys-home-fg)]">
            {apiKey.label}
          </span>
          <Badge variant="muted">{scopeLabel}</Badge>
          {apiKey.revoked_at ? <Badge variant="warning">revoked</Badge> : null}
        </div>

        <div className="mt-3 flex flex-wrap gap-3 text-[11px] text-[var(--sys-home-muted)]">
          <span className="inline-flex items-center gap-1">
            <KeyRound className="h-3.5 w-3.5" />
            {isWorkspaceScoped ? 'workspace' : 'personal'}
          </span>
          <span className="inline-flex items-center gap-1">
            <CalendarClock className="h-3.5 w-3.5" />
            created {formatDate(apiKey.created_at)}
          </span>
          <span>
            last used{' '}
            {apiKey.last_used_at ? formatDate(apiKey.last_used_at) : 'never'}
          </span>
          <span>
            expires {apiKey.expires_at ? formatDate(apiKey.expires_at) : 'never'}
          </span>
        </div>
      </div>

      {onRevoke && !apiKey.revoked_at ? (
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 text-[var(--sys-home-muted)] hover:text-[#dc2626]"
          onClick={() => onRevoke(apiKey.id)}
          disabled={revoking}
          title="Revoke key"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
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
