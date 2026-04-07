import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Plus, Trash2, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { DashboardLoadingState } from '../../components/admin/dashboard-kit'
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
  const { activeWorkspace, workspaceID, workspaces } = useAdmin()
  const queryClient = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [mutationError, setMutationError] = useState('')
  const [createdSecret, setCreatedSecret] = useState('')

  const keysQuery = useListApiKeys<APIKeysCollection>({
    query: { retry: false, staleTime: 30_000 },
  })
  const deleteMutation = useDeleteApiKey()

  const keys = keysQuery.data?.items ?? []
  const workspaceByID = useMemo(
    () =>
      new Map(workspaces.map((workspace) => [workspace.id, workspace.name])),
    [workspaces],
  )

  const visibleKeys = keys.filter((key) => isVisibleKey(key, workspaceID))
  const activeKeys = visibleKeys.filter((key) => !key.revoked_at)
  const revokedKeys = visibleKeys.filter((key) => !!key.revoked_at)

  async function refreshKeys() {
    await queryClient.invalidateQueries({
      queryKey: getListApiKeysQueryKey(),
    })
  }

  async function handleDelete(keyId: string) {
    setMutationError('')

    try {
      await deleteMutation.mutateAsync({ keyId })
      await refreshKeys()
    } catch (error) {
      setMutationError(getErrorMessage(error, 'Failed to revoke API key.'))
    }
  }

  if (keysQuery.isPending) {
    return <DashboardLoadingState label="Loading API keys…" />
  }

  if (keysQuery.isError) {
    return (
      <div className="space-y-4">
        <PageHeader
          title="API keys"
          description="Create and revoke credentials."
        />
        <Alert variant="destructive">
          {getErrorMessage(keysQuery.error, 'Failed to load API keys.')}
        </Alert>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <PageHeader
          title="API keys"
          description="Create and revoke credentials."
        />
        <Button
          variant="ghost"
          size="sm"
          className="gap-1.5"
          onClick={() => setShowCreate((value) => !value)}
        >
          {showCreate ? (
            <X className="h-3.5 w-3.5" />
          ) : (
            <Plus className="h-3.5 w-3.5" />
          )}
          {showCreate ? 'Cancel' : 'New key'}
        </Button>
      </div>

      <Badge variant="muted">
        Scope {activeWorkspace ? activeWorkspace.name : 'All workspaces'}
      </Badge>

      {mutationError ? (
        <Alert variant="destructive">{mutationError}</Alert>
      ) : null}

      {createdSecret ? (
        <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                New key
              </h2>
              <p className="mt-1 text-xs text-[var(--sys-home-muted)]">
                Copy this secret now. It will not be shown again.
              </p>
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setCreatedSecret('')}
            >
              Dismiss
            </Button>
          </div>
          <CodeBlock className="mt-3">{createdSecret}</CodeBlock>
        </section>
      ) : null}

      {showCreate ? (
        <CreateKeyForm
          defaultWorkspaceID={workspaceID}
          workspaces={workspaces}
          onCancel={() => setShowCreate(false)}
          onCreated={async (secret) => {
            setCreatedSecret(secret)
            setShowCreate(false)
            await refreshKeys()
          }}
        />
      ) : null}

      <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)]">
        <SectionHeader
          title="Active"
          count={activeKeys.length}
          status={keysQuery.isFetching ? 'Updating…' : undefined}
        />

        {activeKeys.length ? (
          activeKeys.map((key, index) => (
            <KeyRow
              key={key.id}
              apiKey={key}
              workspaceByID={workspaceByID}
              index={index}
              onRevoke={(keyId) => void handleDelete(keyId)}
              revoking={deleteMutation.isPending}
            />
          ))
        ) : (
          <div className="px-4 py-6 text-xs text-[var(--sys-home-muted)]">
            No active keys.
          </div>
        )}

        {revokedKeys.length ? (
          <>
            <SectionHeader
              title="Revoked"
              count={revokedKeys.length}
              bordered
            />
            {revokedKeys.map((key, index) => (
              <KeyRow
                key={key.id}
                apiKey={key}
                workspaceByID={workspaceByID}
                index={index}
              />
            ))}
          </>
        ) : null}
      </section>
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
    } catch (createError) {
      setError(getErrorMessage(createError, 'Failed to create API key.'))
    }
  }

  return (
    <section className="border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-5">
      <div className="grid gap-4 md:grid-cols-2">
        <label className="space-y-2 text-xs uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
          <span>Label</span>
          <Input
            value={label}
            onChange={(event) => setLabel(event.target.value)}
          />
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
            <option value={CreateAPIKeyRequestScopeType.workspace}>
              Workspace
            </option>
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
        <Button
          onClick={() => void handleCreate()}
          disabled={createMutation.isPending}
        >
          {createMutation.isPending ? 'Creating…' : 'Create key'}
        </Button>
        <Button
          variant="outline"
          onClick={onCancel}
          disabled={createMutation.isPending}
        >
          Cancel
        </Button>
      </div>
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
  const scopeLabel =
    apiKey.scope_type === APIKeyScopeType.workspace
      ? (workspaceByID.get(apiKey.scope_workspace_id ?? '') ??
        apiKey.scope_workspace_id ??
        'Workspace')
      : 'Personal'

  const details = apiKey.revoked_at
    ? [`Revoked ${formatDate(apiKey.revoked_at)}`]
    : [
        apiKey.last_used_at
          ? `Last used ${formatDate(apiKey.last_used_at)}`
          : null,
        apiKey.expires_at ? `Expires ${formatDate(apiKey.expires_at)}` : null,
      ].filter((value): value is string => value !== null)

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
        </div>

        {details.length ? (
          <div className="mt-1 flex flex-wrap gap-3 text-[11px] text-[var(--sys-home-muted)]">
            {details.map((detail) => (
              <span key={detail}>{detail}</span>
            ))}
          </div>
        ) : null}
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

function SectionHeader({
  title,
  count,
  status,
  bordered = false,
}: {
  title: string
  count: number
  status?: string
  bordered?: boolean
}) {
  return (
    <div
      className={`flex items-center justify-between gap-3 px-4 py-3 ${
        bordered
          ? 'border-y border-[var(--sys-home-border)]'
          : 'border-b border-[var(--sys-home-border)]'
      }`}
    >
      <div>
        <h2 className="text-sm font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
          {title}
        </h2>
        {status ? (
          <p className="mt-1 text-[11px] text-[var(--sys-home-muted)]">
            {status}
          </p>
        ) : null}
      </div>
      <span className="text-[11px] uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
        {count}
      </span>
    </div>
  )
}

function isVisibleKey(apiKey: APIKey, workspaceID: string) {
  if (apiKey.scope_type === APIKeyScopeType.user) {
    return true
  }
  if (!workspaceID) {
    return true
  }
  return apiKey.scope_workspace_id === workspaceID
}
