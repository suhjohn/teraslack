import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { LoaderCircle, Plus, RotateCcw, Trash2, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { CodeBlock } from '../../components/ui/code-block'
import { Input } from '../../components/ui/input'
import { formatDate, getErrorMessage, useAdmin } from '../../lib/admin'
import { permissionOptions } from '../../lib/generated/permissions'
import { Select } from '../../components/ui/select'
import {
  getListApiKeysQueryKey,
  useCreateApiKey,
  useDeleteApiKey,
  useListApiKeys,
  useListUsers,
  useRotateApiKey,
} from '../../lib/openapi'
import type { APIKey, APIKeysCollection, User, UsersCollection } from '../../lib/openapi'

export const Route = createFileRoute('/workspace/api-keys')({
  component: ApiKeysPage,
})

function ApiKeysPage() {
  const { workspaceID, workspaces, auth } = useAdmin()
  const queryClient = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [error, setError] = useState('')

  const keysQuery = useListApiKeys<APIKeysCollection>(
    { workspace_id: workspaceID },
    { query: { enabled: !!workspaceID, retry: false } },
  )
  const usersQuery = useListUsers<UsersCollection>(
    workspaceID,
    { limit: 200 },
    { query: { enabled: !!workspaceID, retry: false, staleTime: 30_000 } },
  )

  const keys: APIKey[] = keysQuery.data?.items ?? []
  const users: User[] = usersQuery.data?.items ?? []
  const userByID = useMemo(
    () =>
      new Map(
        users.map((u) => [u.id, u.display_name || u.real_name || u.name]),
      ),
    [users],
  )
  const workspaceByID = useMemo(
    () => new Map(workspaces.map((workspace) => [workspace.id, workspace.name])),
    [workspaces],
  )
  const currentAccountID = auth.account?.id ?? auth.account_id ?? ''

  const activeKeys = keys.filter((k) => !k.revoked)
  const revokedKeys = keys.filter((k) => k.revoked)

  const deleteMutation = useDeleteApiKey()
  const rotateMutation = useRotateApiKey()

  async function handleDelete(id: string) {
    setError('')
    try {
      await deleteMutation.mutateAsync({ id })
      await queryClient.invalidateQueries({
        queryKey: getListApiKeysQueryKey({ workspace_id: workspaceID }),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to revoke API key.'))
    }
  }

  async function handleRotate(id: string) {
    setError('')
    try {
      await rotateMutation.mutateAsync({ id, data: {} })
      await queryClient.invalidateQueries({
        queryKey: getListApiKeysQueryKey({ workspace_id: workspaceID }),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to rotate API key.'))
    }
  }

  return (
    <div className="space-y-8">
      {error ? <Alert>{error}</Alert> : null}

      {/* Active keys */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <div>
            <h2 className="text-sm font-bold text-[var(--ink)]">API keys</h2>
            <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
              {activeKeys.length} active
              {revokedKeys.length > 0 ? `, ${revokedKeys.length} revoked` : ''}
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
            {showCreate ? 'Cancel' : 'New key'}
          </Button>
        </div>

        {showCreate ? (
          <CreateKeyForm
            workspaceID={workspaceID}
            workspaces={workspaces}
            onDone={() => {
              setShowCreate(false)
              void queryClient.invalidateQueries({
                queryKey: getListApiKeysQueryKey({ workspace_id: workspaceID }),
              })
            }}
            onCancel={() => setShowCreate(false)}
          />
        ) : null}

        {keysQuery.isFetching && !keys.length ? (
          <div className="flex items-center justify-center py-8">
            <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
          </div>
        ) : activeKeys.length ? (
          <div className="border border-[var(--line)]">
            {activeKeys.map((key, index) => (
              <KeyRow
                key={key.id}
                apiKey={key}
                index={index}
                userByID={userByID}
                workspaceByID={workspaceByID}
                canManage={
                  key.scope === 'workspace_system' ||
                  key.account_id === currentAccountID
                }
                onRotate={() => void handleRotate(key.id)}
                onRevoke={() => void handleDelete(key.id)}
                rotating={rotateMutation.isPending}
                revoking={deleteMutation.isPending}
              />
            ))}
          </div>
        ) : !keysQuery.isFetching ? (
          <p className="py-4 text-xs text-[var(--ink-soft)]">No active keys.</p>
        ) : null}
      </div>

      {/* Revoked keys */}
      {revokedKeys.length > 0 ? (
        <div>
          <h2 className="mb-3 text-sm font-bold text-[var(--ink)]">
            Revoked
          </h2>
          <div className="border border-[var(--line)] opacity-60">
            {revokedKeys.map((key, index) => (
              <KeyRow
                key={key.id}
                apiKey={key}
                index={index}
                userByID={userByID}
                workspaceByID={workspaceByID}
                canManage={false}
              />
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function KeyRow({
  apiKey,
  index,
  userByID,
  workspaceByID,
  canManage,
  onRotate,
  onRevoke,
  rotating,
  revoking,
}: {
  apiKey: APIKey
  index: number
  userByID: Map<string, string>
  workspaceByID: Map<string, string>
  canManage: boolean
  onRotate?: () => void
  onRevoke?: () => void
  rotating?: boolean
  revoking?: boolean
}) {
  const scopeLabel =
    apiKey.scope === 'workspace_system' ? 'Workspace system' : 'Account'
  const scopeDetail =
    apiKey.scope === 'workspace_system'
      ? workspaceByID.get(apiKey.workspace_id ?? '') ?? apiKey.workspace_id
      : apiKey.workspace_ids?.length
        ? apiKey.workspace_ids
            .map((workspaceID) => workspaceByID.get(workspaceID) ?? workspaceID)
            .join(', ')
        : 'All eligible workspaces'

  return (
    <div
      className={`flex items-start gap-4 px-4 py-3 ${
        index > 0 ? 'border-t border-[var(--line)]' : ''
      }`}
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[13px] font-semibold text-[var(--ink)]">
            {apiKey.name}
          </span>
          <span className="font-mono text-[11px] text-[var(--ink-soft)]">
            {apiKey.key_prefix}…
          </span>
          <Badge variant="muted">{scopeLabel}</Badge>
          {apiKey.permissions.length > 0
            ? apiKey.permissions.map((p) => (
                <Badge key={p} variant="muted">
                  {p}
                </Badge>
              ))
            : null}
        </div>

        <div className="mt-1 flex flex-wrap gap-3 text-[11px] text-[var(--ink-soft)]">
          <span>
            {apiKey.created_by
              ? (userByID.get(apiKey.created_by) ?? apiKey.created_by)
              : 'System'}
          </span>
          <span>{scopeDetail}</span>
          <span>
            last used:{' '}
            {apiKey.last_used_at ? formatDate(apiKey.last_used_at) : 'never'}
          </span>
          <span>created {formatDate(apiKey.created_at)}</span>
        </div>

        {apiKey.description ? (
          <div className="mt-0.5 text-[11px] text-[var(--ink-soft)]">
            {apiKey.description}
          </div>
        ) : null}
      </div>

      {onRotate && onRevoke && canManage ? (
        <div className="flex flex-none gap-1">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-[var(--ink-soft)]"
            onClick={onRotate}
            disabled={revoking || rotating}
            title="Rotate"
          >
            <RotateCcw className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-[var(--ink-soft)] hover:text-[#dc2626]"
            onClick={onRevoke}
            disabled={revoking || rotating}
            title="Revoke"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      ) : null}
    </div>
  )
}

function CreateKeyForm({
  workspaceID,
  workspaces,
  onDone,
  onCancel,
}: {
  workspaceID: string
  workspaces: { id: string; name: string }[]
  onDone: () => void
  onCancel: () => void
}) {
  const [name, setName] = useState('')
  const [scope, setScope] = useState<'account' | 'workspace_system'>('account')
  const [permissions, setPermissions] = useState<string[]>([])
  const [workspaceIDs, setWorkspaceIDs] = useState<string[]>([])
  const [nextPermission, setNextPermission] = useState('')
  const [error, setError] = useState('')
  const [createdSecret, setCreatedSecret] = useState('')
  const createMutation = useCreateApiKey()

  async function handleCreate() {
    if (!name.trim()) return
    setError('')
    try {
      const result = await createMutation.mutateAsync({
        data: {
          name: name.trim(),
          scope,
          workspace_id: workspaceID,
          workspace_ids: scope === 'account' ? workspaceIDs : undefined,
          permissions: permissions.length ? permissions : ['*'],
        },
      })
      if (result.status !== 201) {
        throw new Error('Failed to create API key.')
      }
      setCreatedSecret(result.data.secret)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create API key.'))
    }
  }

  if (createdSecret) {
    return (
      <div className="mb-3 border border-[var(--line)] bg-[var(--surface)] px-4 py-4">
        <p className="mb-2 text-sm font-semibold text-[var(--ink)]">
          Copy this secret — it won't be shown again.
        </p>
        <CodeBlock className="mb-3 break-all text-xs">{createdSecret}</CodeBlock>
        <Button size="sm" onClick={onDone}>
          Done
        </Button>
      </div>
    )
  }

  return (
    <div className="mb-3 border border-[var(--line)] bg-[var(--surface)] px-4 py-4">
      <div className="mb-3 text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
        New key
      </div>
      {error ? <Alert className="mb-3">{error}</Alert> : null}
      <div className="space-y-2">
        <div className="grid gap-2 sm:grid-cols-2">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Key name"
            autoFocus
          />
          <Select
            value={scope}
            onChange={(e) =>
              setScope(e.target.value as 'account' | 'workspace_system')
            }
          >
            <option value="account">Account key</option>
            <option value="workspace_system">Workspace system key</option>
          </Select>
        </div>
        {scope === 'account' ? (
          <div className="space-y-2">
            <div className="text-[11px] text-[var(--ink-soft)]">
              Leave all workspaces unselected to allow every workspace where your
              account has a user.
            </div>
            <div className="flex flex-wrap gap-2">
              {workspaces.map((workspace) => {
                const selected = workspaceIDs.includes(workspace.id)
                return (
                  <button
                    key={workspace.id}
                    type="button"
                    className={`rounded-full border px-2 py-1 text-[11px] ${
                      selected
                        ? 'border-[var(--accent)] bg-[var(--accent)]/10 text-[var(--accent)]'
                        : 'border-[var(--line)] bg-[var(--surface)] text-[var(--ink)]'
                    }`}
                    onClick={() =>
                      setWorkspaceIDs((current) =>
                        selected
                          ? current.filter((item) => item !== workspace.id)
                          : [...current, workspace.id],
                      )
                    }
                  >
                    {workspace.name}
                  </button>
                )
              })}
            </div>
          </div>
        ) : (
          <div className="text-[11px] text-[var(--ink-soft)]">
            Workspace system keys act as a workspace-scoped system principal in{' '}
            {workspaces.find((workspace) => workspace.id === workspaceID)?.name ??
              workspaceID}
            .
          </div>
        )}
        <div className="grid gap-2 sm:grid-cols-2">
          <Select
            value={nextPermission}
            onChange={(e) => {
              const value = e.target.value
              if (!value) return
              setPermissions((current) =>
                current.includes(value) ? current : [...current, value],
              )
              setNextPermission('')
            }}
          >
            <option value="">Add permission</option>
            {permissionOptions
              .filter((option) => !permissions.includes(option.value))
              .map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
          </Select>
        </div>
        {permissions.length ? (
          <div className="flex flex-wrap gap-2">
            {permissions.map((permission) => (
              <button
                key={permission}
                type="button"
                className="inline-flex items-center gap-1 rounded-full border border-[var(--line)] bg-[var(--surface)] px-2 py-1 text-[11px] text-[var(--ink)]"
                onClick={() =>
                  setPermissions((current) =>
                    current.filter((item) => item !== permission),
                  )
                }
              >
                <span className="font-mono">{permission}</span>
                <X className="h-3 w-3" />
              </button>
            ))}
          </div>
        ) : (
          <div className="flex flex-wrap gap-2">
            <span className="inline-flex items-center rounded-full border border-[var(--line)] bg-[var(--surface)] px-2 py-1 text-[11px] text-[var(--ink)]">
              All permissions (<span className="font-mono">*</span>)
            </span>
          </div>
        )}
        <div className="flex gap-2 pt-1">
          <Button
            size="sm"
            onClick={() => void handleCreate()}
            disabled={createMutation.isPending || !name.trim()}
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
