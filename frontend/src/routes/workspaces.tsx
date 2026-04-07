import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, Outlet } from '@tanstack/react-router'
import { LoaderCircle } from 'lucide-react'
import { useState } from 'react'
import { Button } from '../components/ui/button'
import { Card, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Eyebrow } from '../components/ui/eyebrow'
import { APIClientError } from '../lib/api'
import {
  WorkspaceAppContext,
  getPreferredWorkspaceID,
  setPreferredWorkspaceID,
} from '../lib/workspace-context'
import {
  getProfile,
  getGetAuthMeQueryKey,
  getListWorkspacesQueryKey,
  listWorkspaces,
  useDeleteCurrentSession,
} from '../lib/openapi'
import type {
  AuthMeResponse,
  Workspace,
  WorkspacesCollection,
} from '../lib/openapi'

export const Route = createFileRoute('/workspaces')({
  component: WorkspaceAppLayout,
})

function WorkspaceAppLayout() {
  const queryClient = useQueryClient()
  const [preferredWorkspaceID, setLocalPreferredWorkspaceID] = useState(() =>
    getPreferredWorkspaceID(),
  )
  const [signedOut, setSignedOut] = useState(false)

  const authQuery = useQuery<AuthMeResponse>({
    queryKey: getGetAuthMeQueryKey(),
    queryFn: async () => (await getProfile()) as unknown as AuthMeResponse,
    retry: false,
    staleTime: 30_000,
  })

  const workspacesQuery = useQuery<WorkspacesCollection>({
    queryKey: getListWorkspacesQueryKey(),
    queryFn: async () =>
      (await listWorkspaces()) as unknown as WorkspacesCollection,
    enabled: authQuery.isSuccess && !signedOut,
    retry: false,
    staleTime: 30_000,
  })

  const revokeSessionMutation = useDeleteCurrentSession()

  const isUnauthorized =
    signedOut ||
    (authQuery.error instanceof APIClientError &&
      authQuery.error.status === 401)

  const auth = authQuery.data
  const workspaces = workspacesQuery.data?.items ?? []

  async function selectWorkspace(nextWorkspaceID: string) {
    setLocalPreferredWorkspaceID(nextWorkspaceID)
    setPreferredWorkspaceID(nextWorkspaceID)
  }

  async function logout() {
    try {
      await revokeSessionMutation.mutateAsync()
      setSignedOut(true)
      queryClient.removeQueries({ queryKey: getGetAuthMeQueryKey() })
      queryClient.removeQueries({ queryKey: getListWorkspacesQueryKey() })
    } catch {
      // The button state already reflects the pending mutation.
    }
  }

  if (
    authQuery.status === 'pending' ||
    (authQuery.isSuccess && workspacesQuery.status === 'pending')
  ) {
    return (
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto flex min-h-dvh w-full max-w-[1560px] items-center justify-center px-4 py-12">
          <Card className="flex min-h-[40vh] w-full items-center justify-center rounded-[2rem]">
            <span className="inline-flex items-center gap-3 text-[var(--sys-home-muted)]">
              <LoaderCircle className="h-5 w-5 animate-spin" />
              Loading workspace session…
            </span>
          </Card>
        </div>
      </main>
    )
  }

  if (isUnauthorized) {
    return (
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto w-full max-w-[1560px] px-4 py-12">
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
              <Link to="/" className="sys-command-button no-underline">
                Go to login
              </Link>
              <Link to="/" className="sys-outline-link no-underline">
                Back home
              </Link>
            </div>
          </Card>
        </div>
      </main>
    )
  }

  if (auth == null) {
    return (
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto w-full max-w-[1560px] px-4 py-12">
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
        </div>
      </main>
    )
  }

  return (
    <WorkspaceAppContext.Provider
      value={{
        auth,
        workspaces,
        preferredWorkspaceID,
        selectWorkspace,
        logout,
        isSigningOut: revokeSessionMutation.isPending,
      }}
    >
      <Outlet />
    </WorkspaceAppContext.Provider>
  )
}
