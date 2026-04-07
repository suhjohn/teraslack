import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, Outlet } from '@tanstack/react-router'
import {
  Building2,
  Check,
  ChevronsUpDown,
  CalendarClock,
  Plus,
  KeyRound,
  LayoutDashboard,
  LoaderCircle,
  LogOut,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from '../components/ui/button'
import { Eyebrow } from '../components/ui/eyebrow'
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card'
import { APIClientError } from '../lib/api'
import {
  AdminContext,
  getPreferredAdminWorkspaceID,
  setPreferredAdminWorkspaceID,
} from '../lib/admin'
import {
  getGetAuthMeQueryKey,
  getListWorkspacesQueryKey,
  useDeleteCurrentSession,
  useGetAuthMe,
  useListWorkspaces,
} from '../lib/openapi'
import type { AuthMeResponse, WorkspacesCollection } from '../lib/openapi'

export const Route = createFileRoute('/workspace')({
  component: AdminLayout,
})

const navItems = [
  { to: '/workspace', label: 'Overview', icon: LayoutDashboard, exact: true },
  { to: '/workspace/settings', label: 'Billing', icon: Building2, exact: false },
  { to: '/workspace/api-keys', label: 'API Keys', icon: KeyRound, exact: false },
  { to: '/workspace/events', label: 'Events', icon: CalendarClock, exact: false },
]

function AdminLayout() {
  const queryClient = useQueryClient()
  const [preferredWorkspaceID, setPreferredWorkspaceID] = useState(() =>
    getPreferredAdminWorkspaceID(),
  )
  const [signedOut, setSignedOut] = useState(false)

  const authQuery = useGetAuthMe<AuthMeResponse>({
    query: { retry: false, staleTime: 30_000 },
  })

  const isUnauthorized =
    signedOut ||
    (authQuery.error instanceof APIClientError &&
      authQuery.error.status === 401)

  const workspacesQuery = useListWorkspaces<WorkspacesCollection>({
    query: {
      enabled: authQuery.isSuccess && !signedOut,
      retry: false,
      staleTime: 30_000,
    },
  })

  const auth = authQuery.data ?? null
  const workspaces = workspacesQuery.data?.items ?? []

  const workspaceID = useMemo(() => {
    if (
      preferredWorkspaceID &&
      workspaces.some((workspace) => workspace.id === preferredWorkspaceID)
    ) {
      return preferredWorkspaceID
    }
    const firstMembershipWorkspaceID = auth?.workspaces[0]?.workspace_id
    if (
      firstMembershipWorkspaceID &&
      workspaces.some((workspace) => workspace.id === firstMembershipWorkspaceID)
    ) {
      return firstMembershipWorkspaceID
    }
    return workspaces[0]?.id ?? ''
  }, [preferredWorkspaceID, workspaces, auth])

  const activeWorkspace =
    workspaces.find((workspace) => workspace.id === workspaceID) ?? null

  async function selectWorkspace(nextWorkspaceID: string) {
    setPreferredWorkspaceID(nextWorkspaceID)
    setPreferredAdminWorkspaceID(nextWorkspaceID)
  }

  const deleteSession = useDeleteCurrentSession()

  async function logout() {
    try {
      await deleteSession.mutateAsync()
      setSignedOut(true)
      queryClient.removeQueries({ queryKey: getGetAuthMeQueryKey() })
      queryClient.removeQueries({ queryKey: getListWorkspacesQueryKey() })
    } catch {
      // Handled by query state
    }
  }

  if (
    authQuery.status === 'pending' ||
    (authQuery.isSuccess && workspacesQuery.status === 'pending')
  ) {
    return (
      <main className="admin-shell mx-auto w-[min(1800px,calc(100%-2rem))] py-12">
        <Card className="flex min-h-[40vh] items-center justify-center rounded-[2rem]">
          <span className="inline-flex items-center gap-3 text-[var(--sys-home-muted)]">
            <LoaderCircle className="h-5 w-5 animate-spin" />
            Loading workspace session…
          </span>
        </Card>
      </main>
    )
  }

  if (isUnauthorized) {
    return (
      <main className="admin-shell mx-auto w-[min(1800px,calc(100%-2rem))] py-12">
        <Card className="rounded-[2rem] p-8">
          <CardHeader>
            <Eyebrow>Workspace Access</Eyebrow>
            <CardTitle className="text-4xl">Authentication required</CardTitle>
            <CardDescription className="max-w-2xl text-base leading-7">
              {signedOut
                ? 'The current session has been revoked.'
                : 'Sign in to load your workspace.'}
            </CardDescription>
          </CardHeader>
          <div className="mt-6 flex gap-3">
            <Link
              to="/"
              className="sys-command-button no-underline"
            >
              Go to login
            </Link>
            <Link
              to="/"
              className="sys-outline-link no-underline"
            >
              Back home
            </Link>
          </div>
        </Card>
      </main>
    )
  }

  if (!auth) {
    return (
      <main className="admin-shell mx-auto w-[min(1800px,calc(100%-2rem))] py-12">
        <Card className="rounded-[2rem] p-8">
          <CardHeader>
            <CardTitle>Session unavailable</CardTitle>
            <CardDescription>
              The current session could not be loaded.
            </CardDescription>
          </CardHeader>
          <Button
            variant="outline"
            className="mt-4"
            onClick={() => void authQuery.refetch()}
          >
            Retry
          </Button>
        </Card>
      </main>
    )
  }

  return (
    <AdminContext.Provider
      value={{ workspaceID, workspaces, activeWorkspace, auth, selectWorkspace }}
    >
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto flex min-h-dvh w-full max-w-[1560px]">
          <aside className="admin-rail hidden w-[240px] shrink-0 border-r border-[var(--sys-home-border)] lg:block">
            <div className="flex min-h-dvh flex-col gap-6 px-3 py-4">
              {workspaces.length > 0 ? (
                <WorkspaceSwitcher
                  workspaces={workspaces}
                  activeWorkspaceID={workspaceID}
                  onSelect={selectWorkspace}
                />
              ) : null}

              <div className="space-y-2 px-1.5">
                <div className="px-0.5 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                  Workspace
                </div>
                <nav className="flex flex-col gap-1">
                  {navItems.map((item) => (
                    <Link
                      key={item.to}
                      to={item.to}
                      activeOptions={{ exact: item.exact }}
                      className="inline-flex items-center gap-2.5 border border-[var(--sys-home-border)] px-2 py-2 text-[12px] text-[var(--sys-home-muted)] sys-hover data-[status=active]:bg-[var(--sys-home-accent-bg)] data-[status=active]:font-bold data-[status=active]:text-[var(--sys-home-accent-fg)]"
                    >
                      <item.icon className="h-4 w-4" />
                      {item.label}
                    </Link>
                  ))}
                </nav>
              </div>

              <div className="mt-auto px-1.5 pb-0">
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full justify-center"
                  onClick={() => void logout()}
                >
                  <LogOut className="h-3.5 w-3.5" />
                  Sign out
                </Button>
              </div>
            </div>
          </aside>

          <section className="admin-content min-w-0 flex-1 overflow-y-auto">
            <div className="mx-auto min-h-full w-full max-w-[1320px] px-4 py-5 md:px-6 md:py-6 xl:px-8">
              <Outlet />
            </div>
          </section>
        </div>
      </main>
    </AdminContext.Provider>
  )
}

function WorkspaceSwitcher({
  workspaces,
  activeWorkspaceID,
  onSelect,
}: {
  workspaces: { id: string; name: string }[]
  activeWorkspaceID: string
  onSelect: (id: string) => Promise<void>
}) {
  const [open, setOpen] = useState(false)
  const [pendingWorkspaceID, setPendingWorkspaceID] = useState('')
  const ref = useRef<HTMLDivElement>(null)
  const active = workspaces.find((workspace) => workspace.id === activeWorkspaceID)

  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  return (
    <div ref={ref} className="relative px-2">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 rounded-none border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-3 py-2.5 text-left sys-hover"
      >
        <div className="flex h-6 w-6 flex-none items-center justify-center border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] text-xs font-bold text-[var(--sys-home-fg)]">
          {active ? active.name.charAt(0).toUpperCase() : '?'}
        </div>
        <span className="min-w-0 flex-1 truncate text-[12px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]">
          {active?.name ?? 'Select workspace'}
        </span>
        <ChevronsUpDown className="h-3.5 w-3.5 flex-none text-[var(--sys-home-muted)]" />
      </button>

      {open ? (
        <div className="absolute left-2 right-2 z-50 mt-1 border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] py-1 shadow-lg">
          <div className="px-3 py-1.5 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
            Workspaces
          </div>
          {workspaces.map((workspace) => (
            <button
              key={workspace.id}
              type="button"
              onClick={() => {
                setPendingWorkspaceID(workspace.id)
                void onSelect(workspace.id).finally(() => {
                  setPendingWorkspaceID('')
                  setOpen(false)
                })
              }}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-[12px] text-[var(--sys-home-fg)] sys-hover"
            >
              <div className="flex h-5 w-5 flex-none items-center justify-center border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] text-[10px] font-bold text-[var(--sys-home-fg)]">
                {workspace.name.charAt(0).toUpperCase()}
              </div>
              <span className="min-w-0 flex-1 truncate">{workspace.name}</span>
              {pendingWorkspaceID === workspace.id ? (
                <LoaderCircle className="h-3.5 w-3.5 flex-none animate-spin text-[var(--sys-home-fg)]" />
              ) : workspace.id === activeWorkspaceID ? (
                <Check className="h-3.5 w-3.5 flex-none text-[var(--sys-home-fg)]" />
              ) : null}
            </button>
          ))}
          <div className="mt-1 border-t border-[var(--sys-home-border)] pt-1">
            <Link
              to="/workspace/settings"
              search={{ create: true }}
              onClick={() => setOpen(false)}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-[12px] text-[var(--sys-home-muted)] no-underline sys-hover"
            >
              <Plus className="h-3.5 w-3.5 flex-none" />
              <span>Create workspace</span>
            </Link>
          </div>
        </div>
      ) : null}
    </div>
  )
}
