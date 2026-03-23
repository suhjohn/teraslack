import { createFileRoute, Link } from '@tanstack/react-router'
import {
  AlertCircle,
  Building2,
  LoaderCircle,
  LogOut,
  RefreshCcw,
  Shield,
  Users,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { startTransition, useEffect, useMemo, useState } from 'react'
import {
  APIClientError,
  apiFetch,
  delegatedRoles,
} from '../lib/api'
import type { AuthContext, User, UserRolesResponse, Workspace } from '../lib/api'

type SessionState =
  | { status: 'loading' }
  | { status: 'unauthenticated'; error?: string }
  | { status: 'ready'; auth: AuthContext }

const accountTypeOptions = ['member', 'admin', 'primary_admin'] as const

const roleLabels: Record<(typeof delegatedRoles)[number], string> = {
  channels_admin: 'Channels admin',
  roles_admin: 'Roles admin',
  security_admin: 'Security admin',
  integrations_admin: 'Integrations admin',
  usergroups_admin: 'Usergroups admin',
  support_readonly: 'Support readonly',
}

export const Route = createFileRoute('/admin')({
  component: AdminRoute,
})

function AdminRoute() {
  const [session, setSession] = useState<SessionState>({ status: 'loading' })
  const [teams, setTeams] = useState<Workspace[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [selectedTeamID, setSelectedTeamID] = useState('')
  const [selectedUserID, setSelectedUserID] = useState('')
  const [roles, setRoles] = useState<Partial<Record<string, UserRolesResponse>>>({})
  const [loadingUsers, setLoadingUsers] = useState(false)
  const [savingAccountType, setSavingAccountType] = useState(false)
  const [savingRoles, setSavingRoles] = useState(false)
  const [pageError, setPageError] = useState('')
  const [userSearch, setUserSearch] = useState('')
  const [accountTypeDraft, setAccountTypeDraft] = useState('member')
  const [roleDraft, setRoleDraft] = useState<string[]>([])
  const deferredUserSearch = useMemo(() => userSearch.trim().toLowerCase(), [userSearch])

  useEffect(() => {
    void loadSessionAndTeams()
  }, [])

  useEffect(() => {
    if (!selectedTeamID) {
      return
    }
    void loadUsers(selectedTeamID)
  }, [selectedTeamID])

  useEffect(() => {
    const selectedUser = users.find((user) => user.id === selectedUserID)
    if (!selectedUser) {
      return
    }
    setAccountTypeDraft(selectedUser.account_type || 'member')
    void ensureRolesLoaded(selectedUser.id)
  }, [selectedUserID, users])

  useEffect(() => {
    if (!selectedUserID) {
      return
    }
    const userRoles = roles[selectedUserID]
    if (userRoles) {
      setRoleDraft(userRoles.delegated_roles)
    }
  }, [roles, selectedUserID])

  const filteredUsers = useMemo(() => {
    if (!deferredUserSearch) {
      return users
    }
    return users.filter((user) => {
      const haystack = [
        user.name,
        user.real_name,
        user.display_name,
        user.email,
        user.account_type,
      ]
        .join(' ')
        .toLowerCase()
      return haystack.includes(deferredUserSearch)
    })
  }, [deferredUserSearch, users])

  const selectedUser = users.find((user) => user.id === selectedUserID) ?? null
  const activeTeam = teams.find((team) => team.id === selectedTeamID) ?? null

  async function loadSessionAndTeams() {
    setPageError('')
    setSession({ status: 'loading' })

    try {
      const auth = await apiFetch<AuthContext>('/auth/me')
      const teamResponse = await apiFetch<{ items: Workspace[] }>('/teams')
      const nextTeamID = auth.team_id || teamResponse.items[0]?.id || ''

      startTransition(() => {
        setSession({ status: 'ready', auth })
        setTeams(teamResponse.items)
        setSelectedTeamID(nextTeamID)
      })
    } catch (error) {
      if (error instanceof APIClientError && error.status === 401) {
        setSession({
          status: 'unauthenticated',
          error: 'Sign in to load the admin workspace.',
        })
        return
      }
      setSession({
        status: 'unauthenticated',
        error: getErrorMessage(error, 'Failed to load the current session.'),
      })
    }
  }

  async function loadUsers(teamID: string) {
    setLoadingUsers(true)
    setPageError('')
    try {
      const response = await apiFetch<{ items: User[] }>(
        `/users?team_id=${encodeURIComponent(teamID)}&limit=200`,
      )
      startTransition(() => {
        setUsers(response.items)
        setSelectedUserID((current) => {
          if (current && response.items.some((user) => user.id === current)) {
            return current
          }
          return response.items[0]?.id ?? ''
        })
      })
    } catch (error) {
      setPageError(getErrorMessage(error, 'Failed to load users.'))
    } finally {
      setLoadingUsers(false)
    }
  }

  async function ensureRolesLoaded(userID: string) {
    if (roles[userID]) {
      return
    }
    try {
      const response = await apiFetch<UserRolesResponse>(`/users/${userID}/roles`)
      setRoles((current) => ({ ...current, [userID]: response }))
    } catch (error) {
      setPageError(getErrorMessage(error, 'Failed to load delegated roles.'))
    }
  }

  async function saveAccountType() {
    if (!selectedUser) {
      return
    }
    setSavingAccountType(true)
    setPageError('')
    try {
      const updated = await apiFetch<User>(`/users/${selectedUser.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ account_type: accountTypeDraft }),
      })
      setUsers((current) =>
        current.map((user) => (user.id === updated.id ? updated : user)),
      )
    } catch (error) {
      setPageError(getErrorMessage(error, 'Failed to save account type.'))
    } finally {
      setSavingAccountType(false)
    }
  }

  async function saveRoles() {
    if (!selectedUser) {
      return
    }
    setSavingRoles(true)
    setPageError('')
    try {
      const updated = await apiFetch<UserRolesResponse>(
        `/users/${selectedUser.id}/roles`,
        {
          method: 'PUT',
          body: JSON.stringify({ delegated_roles: roleDraft }),
        },
      )
      setRoles((current) => ({ ...current, [selectedUser.id]: updated }))
    } catch (error) {
      setPageError(getErrorMessage(error, 'Failed to save delegated roles.'))
    } finally {
      setSavingRoles(false)
    }
  }

  async function logout() {
    try {
      await apiFetch('/auth/sessions/current', { method: 'DELETE' })
      startTransition(() => {
        setSession({ status: 'unauthenticated', error: '' })
        setUsers([])
        setTeams([])
        setSelectedUserID('')
      })
    } catch (error) {
      setPageError(getErrorMessage(error, 'Failed to revoke the current session.'))
    }
  }

  if (session.status === 'loading') {
    return (
      <main className="page-wrap px-4 py-12">
        <section className="admin-card flex min-h-[40vh] items-center justify-center rounded-[2rem]">
          <div className="inline-flex items-center gap-3 text-[var(--ink-soft)]">
            <LoaderCircle className="h-5 w-5 animate-spin" />
            Loading admin session…
          </div>
        </section>
      </main>
    )
  }

  if (session.status === 'unauthenticated') {
    return (
      <main className="page-wrap px-4 py-12">
        <section className="admin-card rounded-[2rem] p-8">
          <p className="eyebrow">Admin Access</p>
          <h1 className="display-title text-4xl">Authentication required</h1>
          <p className="mt-4 max-w-2xl text-base leading-7 text-[var(--ink-soft)]">
            {session.error ||
              'The admin surface is only available after you complete an OAuth login.'}
          </p>
          <div className="mt-6 flex flex-wrap gap-3">
            <Link to="/login" className="action-button no-underline">
              Go to login
            </Link>
            <Link to="/" className="secondary-button no-underline">
              Back to landing page
            </Link>
          </div>
        </section>
      </main>
    )
  }

  return (
    <main className="page-wrap px-4 py-10">
      <section className="hero-panel rounded-[2rem] px-6 py-8 sm:px-8">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="eyebrow">Admin Console</p>
            <h1 className="display-title max-w-3xl text-4xl leading-[0.96] font-semibold sm:text-5xl">
              Teams, people, and delegated access in one operational view.
            </h1>
            <p className="mt-4 max-w-2xl text-base leading-7 text-[var(--ink-soft)]">
              This first cut sits directly on the Teraslack API. It is focused
              on session validation, team inspection, user management, and role
              delegation.
            </p>
          </div>
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              className="secondary-button"
              onClick={() => void loadSessionAndTeams()}
            >
              <RefreshCcw className="h-4 w-4" />
              Refresh
            </button>
            <button type="button" className="secondary-button" onClick={() => void logout()}>
              <LogOut className="h-4 w-4" />
              Sign out
            </button>
          </div>
        </div>
      </section>

      <section className="mt-6 grid gap-4 lg:grid-cols-3">
        <StatCard
          icon={<Shield className="h-5 w-5" />}
          label="Session"
          value={session.auth.account_type || 'member'}
          subvalue={`User ${session.auth.user_id}`}
        />
        <StatCard
          icon={<Building2 className="h-5 w-5" />}
          label="Teams"
          value={String(teams.length)}
          subvalue={activeTeam?.name || 'No team selected'}
        />
        <StatCard
          icon={<Users className="h-5 w-5" />}
          label="Users"
          value={String(users.length)}
          subvalue={loadingUsers ? 'Refreshing directory…' : 'Directory synced'}
        />
      </section>

      {pageError ? (
        <section className="warning-banner mt-5 rounded-2xl px-4 py-3 text-sm">
          <AlertCircle className="h-4 w-4" />
          {pageError}
        </section>
      ) : null}

      <section className="mt-6 grid gap-6 xl:grid-cols-[1.15fr_0.85fr]">
        <div className="space-y-6">
          <section className="admin-card rounded-[1.75rem] p-5">
            <div className="mb-4 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <div>
                <p className="eyebrow">Workspace Scope</p>
                <h2 className="text-xl font-semibold text-[var(--ink)]">
                  Teams you can administer
                </h2>
              </div>
              <select
                className="admin-select"
                value={selectedTeamID}
                onChange={(event) => {
                  const teamID = event.target.value
                  startTransition(() => setSelectedTeamID(teamID))
                }}
              >
                {teams.map((team) => (
                  <option key={team.id} value={team.id}>
                    {team.name}
                  </option>
                ))}
              </select>
            </div>

            {activeTeam ? (
              <div className="grid gap-4 md:grid-cols-2">
                <DetailTile label="Domain" value={activeTeam.domain || 'Unset'} />
                <DetailTile
                  label="Email Domain"
                  value={activeTeam.email_domain || 'Unset'}
                />
                <DetailTile
                  label="Discoverability"
                  value={activeTeam.discoverability || 'open'}
                />
                <DetailTile
                  label="Updated"
                  value={formatDate(activeTeam.updated_at)}
                />
              </div>
            ) : (
              <div className="empty-state">No team records available for this session.</div>
            )}
          </section>

          <section className="admin-card rounded-[1.75rem] p-5">
            <div className="mb-4 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <div>
                <p className="eyebrow">Directory</p>
                <h2 className="text-xl font-semibold text-[var(--ink)]">
                  Team users
                </h2>
              </div>
              <input
                className="admin-input"
                type="search"
                value={userSearch}
                onChange={(event) => setUserSearch(event.target.value)}
                placeholder="Filter by name, email, or role"
              />
            </div>

            <div className="space-y-2">
              {filteredUsers.map((user) => (
                <button
                  key={user.id}
                  type="button"
                  className={`user-row ${user.id === selectedUserID ? 'is-selected' : ''}`}
                  onClick={() => setSelectedUserID(user.id)}
                >
                  <div className="min-w-0">
                    <div className="truncate text-sm font-semibold text-[var(--ink)]">
                      {user.display_name || user.real_name || user.name}
                    </div>
                    <div className="truncate text-xs text-[var(--ink-soft)]">
                      {user.email || user.name}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="pill">{user.account_type || 'member'}</span>
                    {user.deleted ? <span className="pill pill-muted">deleted</span> : null}
                  </div>
                </button>
              ))}

              {!filteredUsers.length && !loadingUsers ? (
                <div className="empty-state">No users matched the current filter.</div>
              ) : null}
            </div>
          </section>
        </div>

        <aside className="admin-card rounded-[1.75rem] p-5">
          {selectedUser ? (
            <div className="space-y-6">
              <div>
                <p className="eyebrow">User Detail</p>
                <h2 className="text-2xl font-semibold text-[var(--ink)]">
                  {selectedUser.display_name ||
                    selectedUser.real_name ||
                    selectedUser.name}
                </h2>
                <p className="mt-2 text-sm text-[var(--ink-soft)]">
                  {selectedUser.email || 'No email on file'}
                </p>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <DetailTile label="Principal" value={selectedUser.principal_type} />
                <DetailTile label="Bot" value={selectedUser.is_bot ? 'Yes' : 'No'} />
                <DetailTile label="Created" value={formatDate(selectedUser.created_at)} />
                <DetailTile label="Updated" value={formatDate(selectedUser.updated_at)} />
              </div>

              <section className="space-y-3">
                <div>
                  <h3 className="text-sm font-semibold text-[var(--ink)]">
                    Account type
                  </h3>
                  <p className="text-sm text-[var(--ink-soft)]">
                    Promote or reduce workspace-level admin access.
                  </p>
                </div>
                <div className="flex flex-col gap-3 sm:flex-row">
                  <select
                    className="admin-select flex-1"
                    value={accountTypeDraft}
                    onChange={(event) => setAccountTypeDraft(event.target.value)}
                  >
                    {accountTypeOptions.map((option) => (
                      <option key={option} value={option}>
                        {option}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    className="action-button"
                    onClick={() => void saveAccountType()}
                    disabled={savingAccountType}
                  >
                    {savingAccountType ? 'Saving…' : 'Save account type'}
                  </button>
                </div>
              </section>

              <section className="space-y-3">
                <div>
                  <h3 className="text-sm font-semibold text-[var(--ink)]">
                    Delegated roles
                  </h3>
                  <p className="text-sm text-[var(--ink-soft)]">
                    Fine-grained admin capabilities layered on top of account
                    type.
                  </p>
                </div>
                <div className="grid gap-2">
                  {delegatedRoles.map((role) => {
                    const checked = roleDraft.includes(role)
                    return (
                      <label key={role} className="role-option">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() =>
                            setRoleDraft((current) =>
                              checked
                                ? current.filter((item) => item !== role)
                                : [...current, role],
                            )
                          }
                        />
                        <span>{roleLabels[role]}</span>
                      </label>
                    )
                  })}
                </div>
                <button
                  type="button"
                  className="action-button"
                  onClick={() => void saveRoles()}
                  disabled={savingRoles}
                >
                  {savingRoles ? 'Saving…' : 'Save delegated roles'}
                </button>
              </section>
            </div>
          ) : (
            <div className="empty-state">Choose a user to inspect and edit.</div>
          )}
        </aside>
      </section>
    </main>
  )
}

function StatCard({
  icon,
  label,
  value,
  subvalue,
}: {
  icon: ReactNode
  label: string
  value: string
  subvalue: string
}) {
  return (
    <article className="admin-card rounded-[1.5rem] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="eyebrow">{label}</p>
          <div className="mt-2 text-3xl font-semibold text-[var(--ink)]">
            {value}
          </div>
          <p className="mt-2 text-sm text-[var(--ink-soft)]">{subvalue}</p>
        </div>
        <div className="inline-flex h-11 w-11 items-center justify-center rounded-2xl bg-[rgba(209,78,45,0.12)] text-[var(--accent)]">
          {icon}
        </div>
      </div>
    </article>
  )
}

function DetailTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="detail-tile">
      <div className="text-[11px] font-semibold tracking-[0.16em] text-[var(--ink-soft)] uppercase">
        {label}
      </div>
      <div className="mt-2 text-sm font-medium text-[var(--ink)]">{value}</div>
    </div>
  )
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof APIClientError) {
    return error.message
  }
  if (error instanceof Error) {
    return error.message
  }
  return fallback
}
