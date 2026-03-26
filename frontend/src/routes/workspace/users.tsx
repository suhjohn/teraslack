import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Bot, LoaderCircle, Trash2 } from 'lucide-react'
import { startTransition, useDeferredValue, useMemo, useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Input } from '../../components/ui/input'
import { Select } from '../../components/ui/select'
import { formatDate, getErrorMessage, useAdmin } from '../../lib/admin'
import {
  getListExternalPrincipalAccessQueryKey,
  getListUsergroupMembersQueryKey,
  getListUsergroupsQueryKey,
  getListUsersQueryKey,
  useCreateExternalPrincipalAccess,
  useDeleteExternalPrincipalAccess,
  useListExternalPrincipalAccess,
  useListUsergroupMembers,
  useListUsergroups,
  useListUsers,
  useReplaceUsergroupMembers,
  useUpdateUser,
} from '../../lib/openapi'
import type {
  ExternalPrincipalAccessCollection,
  User,
  Usergroup,
  UsergroupsCollection,
  UsersCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspace/users')({
  component: UsersPage,
})

const accountTypeOptions = ['member', 'admin', 'primary_admin'] as const
const externalAccessModeOptions = [
  'external_shared',
  'external_shared_readonly',
] as const

type AccountTypeOption = (typeof accountTypeOptions)[number]
type ExternalAccessModeOption = (typeof externalAccessModeOptions)[number]

function getUserDisplayName(user: User) {
  return user.display_name || user.real_name || user.name
}

function initialsForUser(label: string) {
  return (
    label
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0].toUpperCase())
      .join('') || 'U'
  )
}

function UsersPage() {
  const { workspaceID } = useAdmin()
  const [selectedUserID, setSelectedUserID] = useState('')
  const [userSearch, setUserSearch] = useState('')
  const deferredSearch = useDeferredValue(userSearch.trim().toLowerCase())

  const usersQuery = useListUsers<UsersCollection>(
    workspaceID ? { workspace_id: workspaceID, limit: 200 } : undefined,
    { query: { enabled: !!workspaceID, retry: false } },
  )
  const usergroupsQuery = useListUsergroups<UsergroupsCollection>(
    workspaceID ? { workspace_id: workspaceID } : undefined,
    { query: { enabled: !!workspaceID, retry: false } },
  )
  const users = usersQuery.data?.items ?? []
  const selectedUserIsBot =
    users.find((u) => u.id === selectedUserID)?.is_bot ?? false
  const principalAccessQuery =
    useListExternalPrincipalAccess<ExternalPrincipalAccessCollection>(
      workspaceID ? { host_workspace_id: workspaceID } : undefined,
      {
        query: {
          enabled: !!workspaceID && selectedUserIsBot,
          retry: false,
        },
      },
    )
  const usergroups = usergroupsQuery.data?.items ?? []
  const principalAccessRules = principalAccessQuery.data?.items ?? []

  const filteredUsers = useMemo(() => {
    if (!deferredSearch) return users
    return users.filter((user) =>
      [user.name, user.real_name, user.display_name, user.email, user.account_type]
        .join(' ')
        .toLowerCase()
        .includes(deferredSearch),
    )
  }, [deferredSearch, users])

  const effectiveUserID = filteredUsers.some((u) => u.id === selectedUserID)
    ? selectedUserID
    : (filteredUsers[0]?.id ?? '')

  const selectedUser =
    filteredUsers.find((u) => u.id === effectiveUserID) ?? null
  const selectedUserRules = selectedUser
    ? principalAccessRules.filter((r) => r.principal_id === selectedUser.id)
    : []

  return (
    <div className="flex h-full min-h-[600px] overflow-hidden border border-[var(--line)]">
      {/* Sidebar */}
      <div className="flex w-[260px] flex-none flex-col border-r border-[var(--line)] bg-[var(--surface-strong)]">
        <div className="flex-none border-b border-[var(--line)] px-4 py-3">
          <h2 className="text-[15px] font-bold text-[var(--ink)]">Users</h2>
          <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
            {users.length} in workspace
          </p>
        </div>

        <div className="flex-none border-b border-[var(--line)] px-3 py-2">
          <Input
            className="h-7 text-xs"
            type="search"
            value={userSearch}
            onChange={(e) => setUserSearch(e.target.value)}
            placeholder="Filter by name, email…"
          />
        </div>

        <div className="flex-1 overflow-y-auto">
          {usersQuery.isFetching && !users.length ? (
            <div className="flex items-center justify-center py-10">
              <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
            </div>
          ) : filteredUsers.length ? (
            <div className="py-1.5">
              {filteredUsers.map((user) => {
                const label = getUserDisplayName(user)
                const isSelected = user.id === effectiveUserID
                const isNonMember = user.account_type !== 'member'
                return (
                  <button
                    key={user.id}
                    type="button"
                    className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-left transition-colors ${
                      isSelected
                        ? 'bg-[var(--accent-faint)] text-[var(--ink)]'
                        : 'text-[var(--ink-soft)] hover:bg-[var(--accent-faint)]'
                    } ${user.deleted ? 'opacity-40' : ''}`}
                    onClick={() =>
                      startTransition(() => setSelectedUserID(user.id))
                    }
                  >
                    <div className="flex h-7 w-7 flex-none items-center justify-center rounded bg-[var(--accent-faint)] text-[10px] font-bold text-[var(--ink-soft)]">
                      {user.is_bot ? (
                        <Bot className="h-3.5 w-3.5" />
                      ) : (
                        initialsForUser(label)
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[13px] font-medium text-[var(--ink)]">
                        {label}
                      </div>
                      <div className="truncate text-[11px] text-[var(--ink-soft)]">
                        {user.email || user.name}
                      </div>
                    </div>
                    {isNonMember ? (
                      <span className="flex-none text-[10px] font-bold uppercase tracking-wide text-[var(--ink-soft)]">
                        {user.account_type === 'primary_admin' ? 'owner' : 'admin'}
                      </span>
                    ) : null}
                  </button>
                )
              })}
            </div>
          ) : (
            <div className="px-3 py-6 text-center text-xs text-[var(--ink-soft)]">
              {users.length ? 'No users matched.' : 'No users found.'}
            </div>
          )}
        </div>
      </div>

      {/* Detail pane */}
      <div className="flex min-w-0 flex-1 flex-col overflow-y-auto">
        {selectedUser ? (
          <UserDetail
            key={selectedUser.id}
            workspaceID={workspaceID}
            user={selectedUser}
            usergroups={usergroups}
            usergroupsLoading={usergroupsQuery.isFetching}
            principalAccessRules={selectedUserRules}
            principalAccessLoading={principalAccessQuery.isFetching}
          />
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-sm text-[var(--ink-soft)]">
              {userSearch.trim() && users.length
                ? 'No users matched the current filter.'
                : 'Select a user to inspect and edit.'}
            </p>
          </div>
        )}
      </div>
    </div>
  )
}

function UserDetail({
  workspaceID,
  user,
  usergroups,
  usergroupsLoading,
  principalAccessRules,
  principalAccessLoading,
}: {
  workspaceID: string
  user: User
  usergroups: Usergroup[]
  usergroupsLoading: boolean
  principalAccessRules: ExternalPrincipalAccessCollection['items']
  principalAccessLoading: boolean
}) {
  const label = getUserDisplayName(user)

  return (
    <div className="px-6 py-5">
      {/* Header */}
      <div className="flex items-start gap-4">
        <div className="flex h-12 w-12 flex-none items-center justify-center rounded-lg bg-[var(--accent-faint)] text-sm font-bold text-[var(--ink-soft)]">
          {user.is_bot ? (
            <Bot className="h-5 w-5" />
          ) : (
            initialsForUser(label)
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-xl font-bold text-[var(--ink)]">{label}</h2>
            {user.deleted ? (
              <Badge variant="muted">deleted</Badge>
            ) : null}
            {user.is_bot ? (
              <Badge variant="muted">bot</Badge>
            ) : null}
            {user.account_type !== 'member' ? (
              <Badge>{user.account_type}</Badge>
            ) : null}
          </div>
          <p className="mt-0.5 text-sm text-[var(--ink-soft)]">
            {user.email || 'No email on file'}
          </p>
        </div>
      </div>

      {/* Metadata strip */}
      <div className="mt-5 grid grid-cols-2 gap-px border border-[var(--line)] bg-[var(--line)] sm:grid-cols-4">
        <MetaCell label="User ID" value={user.id} mono />
        <MetaCell label="Principal" value={user.principal_type} />
        <MetaCell label="Created" value={formatDate(user.created_at)} />
        <MetaCell label="Updated" value={formatDate(user.updated_at)} />
      </div>

      {/* Account type */}
      <Section label="Account type" description="Workspace-level administrative access.">
        <AccountTypeForm user={user} workspaceID={workspaceID} />
      </Section>

      {/* Usergroups */}
      <Section
        label="Usergroups"
        description="Group membership for this user."
      >
        {usergroupsLoading && !usergroups.length ? (
          <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
        ) : !usergroups.length ? (
          <p className="text-xs text-[var(--ink-soft)]">
            No usergroups configured for this workspace.
          </p>
        ) : (
          <div className="space-y-0.5">
            {usergroups.map((group) => (
              <UsergroupRow
                key={group.id}
                group={group}
                workspaceID={workspaceID}
                userID={user.id}
              />
            ))}
          </div>
        )}
      </Section>

      {/* External access — agents only */}
      {user.is_bot ? (
        <Section
          label="External access"
          description="Principal-access rules that allow this agent to reach external workspaces."
        >
          <ExternalAccessSection
            principalAccessRules={principalAccessRules}
            workspaceID={workspaceID}
            user={user}
            loading={principalAccessLoading}
          />
        </Section>
      ) : null}
    </div>
  )
}

function AccountTypeForm({ user, workspaceID }: { user: User; workspaceID: string }) {
  const queryClient = useQueryClient()
  const [accountType, setAccountType] = useState<AccountTypeOption>(
    user.account_type,
  )
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const updateUser = useUpdateUser()

  async function save() {
    setError('')
    setSuccess(false)
    try {
      await updateUser.mutateAsync({
        id: user.id,
        data: { account_type: accountType },
      })
      await queryClient.invalidateQueries({
        queryKey: getListUsersQueryKey({ workspace_id: workspaceID, limit: 200 }),
      })
      setSuccess(true)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to save account type.'))
    }
  }

  return (
    <div className="space-y-3">
      {error ? <Alert>{error}</Alert> : null}
      {success ? <Alert>Account type saved.</Alert> : null}
      <div className="flex gap-2">
        <Select
          className="flex-1"
          value={accountType}
          onChange={(e) => setAccountType(e.target.value as AccountTypeOption)}
        >
          {accountTypeOptions.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </Select>
        <Button
          onClick={() => void save()}
          disabled={updateUser.isPending || accountType === user.account_type}
        >
          {updateUser.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  )
}

function UsergroupRow({
  group,
  workspaceID,
  userID,
}: {
  group: Usergroup
  workspaceID: string
  userID: string
}) {
  const queryClient = useQueryClient()
  const [error, setError] = useState('')
  const membersQuery = useListUsergroupMembers<UsersCollection>(group.id, {
    query: { retry: false },
  })
  const replaceMembers = useReplaceUsergroupMembers()

  const members = membersQuery.data?.items ?? []
  const isMember = members.some((m) => m.id === userID)

  async function toggle() {
    if (!membersQuery.data) return
    setError('')
    const nextUsers = isMember
      ? members.filter((m) => m.id !== userID).map((m) => m.id)
      : [...members.map((m) => m.id), userID]
    try {
      await replaceMembers.mutateAsync({ id: group.id, data: { users: nextUsers } })
      await queryClient.invalidateQueries({
        queryKey: getListUsergroupMembersQueryKey(group.id),
      })
      await queryClient.invalidateQueries({
        queryKey: getListUsergroupsQueryKey({ workspace_id: workspaceID }),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to update usergroup membership.'))
    }
  }

  return (
    <div>
      <div className="group flex items-center gap-3 rounded-sm px-2 py-1.5 hover:bg-[var(--accent-faint)]">
        <div className="min-w-0 flex-1">
          <span className="text-[13px] font-medium text-[var(--ink)]">
            {group.name}
          </span>
          <span className="ml-2 text-[11px] text-[var(--ink-soft)]">
            @{group.handle}
          </span>
        </div>
        {membersQuery.isFetching ? (
          <LoaderCircle className="h-3.5 w-3.5 animate-spin text-[var(--ink-soft)]" />
        ) : (
          <>
            <span
              className={`text-[11px] font-medium ${isMember ? 'text-[var(--ink)]' : 'text-[var(--ink-soft)]'}`}
            >
              {isMember ? 'member' : 'not in group'}
            </span>
            <Button
              variant={isMember ? 'outline' : 'ghost'}
              size="sm"
              className="h-6 px-2 text-xs opacity-0 group-hover:opacity-100"
              onClick={() => void toggle()}
              disabled={replaceMembers.isPending}
            >
              {replaceMembers.isPending ? '…' : isMember ? 'Remove' : 'Add'}
            </Button>
          </>
        )}
      </div>
      {error ? <Alert className="mt-1 text-xs">{error}</Alert> : null}
    </div>
  )
}

function ExternalAccessSection({
  principalAccessRules,
  workspaceID,
  user,
  loading,
}: {
  principalAccessRules: ExternalPrincipalAccessCollection['items']
  workspaceID: string
  user: User
  loading: boolean
}) {
  const queryClient = useQueryClient()
  const [homeWorkspaceID, setHomeWorkspaceID] = useState('')
  const [accessMode, setAccessMode] =
    useState<ExternalAccessModeOption>('external_shared')
  const [error, setError] = useState('')
  const createAccess = useCreateExternalPrincipalAccess()
  const deleteAccess = useDeleteExternalPrincipalAccess()

  async function createRule() {
    const id = homeWorkspaceID.trim()
    if (!id) return
    setError('')
    try {
      await createAccess.mutateAsync({
        data: {
          host_workspace_id: workspaceID,
          principal_id: user.id,
          principal_type: 'human',
          home_workspace_id: id,
          access_mode: accessMode,
        },
      })
      setHomeWorkspaceID('')
      await queryClient.invalidateQueries({
        queryKey: getListExternalPrincipalAccessQueryKey({ host_workspace_id: workspaceID }),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create external access rule.'))
    }
  }

  async function deleteRule(id: string) {
    setError('')
    try {
      await deleteAccess.mutateAsync({ id })
      await queryClient.invalidateQueries({
        queryKey: getListExternalPrincipalAccessQueryKey({ host_workspace_id: workspaceID }),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to delete external access rule.'))
    }
  }

  return (
    <div className="space-y-4">
      {error ? <Alert>{error}</Alert> : null}

      {/* Create form */}
      <div className="flex flex-wrap gap-2">
        <Input
          className="h-8 w-48 text-xs"
          value={homeWorkspaceID}
          onChange={(e) => setHomeWorkspaceID(e.target.value)}
          placeholder="Home workspace ID"
        />
        <Select
          className="h-8 text-xs"
          value={accessMode}
          onChange={(e) => setAccessMode(e.target.value as ExternalAccessModeOption)}
        >
          {externalAccessModeOptions.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </Select>
        <Button
          size="sm"
          onClick={() => void createRule()}
          disabled={createAccess.isPending || !homeWorkspaceID.trim()}
        >
          {createAccess.isPending ? 'Adding…' : 'Add rule'}
        </Button>
      </div>

      {/* Rules list */}
      {loading && !principalAccessRules.length ? (
        <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
      ) : principalAccessRules.length ? (
        <div className="border border-[var(--line)]">
          {principalAccessRules.map((rule, index) => (
            <div
              key={rule.id}
              className={`flex items-center gap-3 px-3 py-2 ${
                index > 0 ? 'border-t border-[var(--line)]' : ''
              }`}
            >
              <div className="min-w-0 flex-1">
                <span className="text-[13px] font-medium text-[var(--ink)]">
                  {rule.home_workspace_id}
                </span>
                <span className="ml-2 text-[11px] text-[var(--ink-soft)]">
                  {rule.access_mode}
                </span>
                {rule.revoked_at ? (
                  <Badge variant="muted" className="ml-2">revoked</Badge>
                ) : null}
              </div>
              <span className="text-[11px] text-[var(--ink-soft)]">
                {formatDate(rule.created_at)}
              </span>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6 text-[var(--ink-soft)] hover:text-[#dc2626]"
                onClick={() => void deleteRule(rule.id)}
                disabled={deleteAccess.isPending}
                title="Remove rule"
              >
                <Trash2 className="h-3 w-3" />
              </Button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs text-[var(--ink-soft)]">
          No external access rules for this user.
        </p>
      )}
    </div>
  )
}

function Section({
  label,
  description,
  children,
}: {
  label: string
  description: string
  children: React.ReactNode
}) {
  return (
    <div className="mt-6 border-t border-[var(--line)] pt-5">
      <div className="mb-3">
        <h3 className="text-sm font-bold text-[var(--ink)]">{label}</h3>
        <p className="mt-0.5 text-xs text-[var(--ink-soft)]">{description}</p>
      </div>
      {children}
    </div>
  )
}

function MetaCell({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="bg-[var(--surface-strong)] px-4 py-3">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
        {label}
      </div>
      <div
        className={`mt-0.5 truncate text-sm text-[var(--ink)] ${mono ? 'font-mono text-xs' : ''}`}
      >
        {value}
      </div>
    </div>
  )
}
