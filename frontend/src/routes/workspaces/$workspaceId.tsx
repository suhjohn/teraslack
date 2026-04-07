import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Outlet, useRouterState } from '@tanstack/react-router'
import { useEffect, useMemo } from 'react'
import { Hash } from 'lucide-react'
import { Card, CardDescription, CardHeader, CardTitle } from '../../components/ui/card'
import {
  CreateChannelDialogButton,
  CreateWorkspaceDialogButton,
  ManageConversationDialogButton,
  ManageWorkspaceDialogButton,
} from '../../components/workspace/management'
import {
  WorkspaceConversationRail,
  WorkspaceInfoRail,
  WorkspaceRail,
  sortConversations,
} from '../../components/workspace/shell'
import { useWorkspaceApp, WorkspaceRouteContext } from '../../lib/workspace-context'
import {
  getListConversationsQueryKey,
  getListWorkspaceMembersQueryKey,
  listConversations,
  listWorkspaceMembers,
} from '../../lib/openapi'
import type {
  ConversationsCollection,
  User,
  WorkspaceMembersCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspaces/$workspaceId')({
  component: WorkspaceLayout,
})

function WorkspaceLayout() {
  const { workspaceId } = Route.useParams()
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const {
    auth,
    workspaces,
    preferredWorkspaceID,
    selectWorkspace,
    logout,
    isSigningOut,
  } = useWorkspaceApp()

  const workspace =
    workspaces.find((candidate) => candidate.id === workspaceId) ?? null

  useEffect(() => {
    if (!workspace || preferredWorkspaceID === workspace.id) {
      return
    }

    void selectWorkspace(workspace.id)
  }, [preferredWorkspaceID, selectWorkspace, workspace])

  const conversationsQuery = useQuery<ConversationsCollection>({
    queryKey: getListConversationsQueryKey({ workspace_id: workspaceId, limit: 200 }),
    queryFn: async () =>
      (await listConversations({
        workspace_id: workspaceId,
        limit: 200,
      })) as unknown as ConversationsCollection,
    enabled: workspace != null,
    retry: false,
    staleTime: 15_000,
  })

  const membersQuery = useQuery<WorkspaceMembersCollection>({
    queryKey: getListWorkspaceMembersQueryKey(workspaceId),
    queryFn: async () =>
      (await listWorkspaceMembers(workspaceId)) as unknown as WorkspaceMembersCollection,
    enabled: workspace != null,
    retry: false,
    staleTime: 30_000,
  })

  const conversations = useMemo(
    () => sortConversations(conversationsQuery.data?.items ?? []),
    [conversationsQuery.data?.items],
  )

  const members = membersQuery.data?.items ?? []
  const currentMembership =
    members.find((member) => member.user_id === auth.user.id) ?? null
  const canManageWorkspace =
    currentMembership?.role === 'owner' || currentMembership?.role === 'admin'

  const selectedConversationId = useMemo(() => {
    const prefix = `/workspaces/${workspaceId}/channels/`
    if (!pathname.startsWith(prefix)) {
      return ''
    }

    return decodeURIComponent(pathname.slice(prefix.length))
  }, [pathname, workspaceId])

  const selectedConversation =
    conversations.find((conversation) => conversation.id === selectedConversationId) ??
    null
  const canManageSelectedConversation =
    selectedConversation != null &&
    (canManageWorkspace ||
      selectedConversation.created_by_user_id === auth.user.id)

  const memberUsersById = useMemo(
    () =>
      new Map<string, User>(
        members.map((member) => [member.user_id, member.user] as const),
      ),
    [members],
  )

  if (!workspace) {
    return (
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto w-full max-w-[1560px] px-4 py-12">
          <Card className="rounded-[2rem] p-8">
            <CardHeader>
              <CardTitle>Workspace unavailable</CardTitle>
              <CardDescription>
                The requested workspace is not visible to this session.
              </CardDescription>
            </CardHeader>
          </Card>
        </div>
      </main>
    )
  }

  return (
    <WorkspaceRouteContext.Provider
      value={{
        workspace,
        conversations,
        conversationsPending: conversationsQuery.isPending,
        conversationsError: conversationsQuery.isError
          ? conversationsQuery.error.message
          : '',
        members,
        membersPending: membersQuery.isPending,
        membersError: membersQuery.isError ? membersQuery.error.message : '',
        memberUsersById,
        selectedConversationId,
        selectedConversation,
      }}
    >
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto flex min-h-dvh w-full max-w-[1760px] flex-col border-x border-[var(--sys-home-border)] xl:flex-row">
          <WorkspaceRail
            workspaces={workspaces}
            activeScope="workspace"
            activeWorkspaceID={workspace.id}
            workspaceCreateAction={
              <CreateWorkspaceDialogButton
                variant="ghost"
                size="icon"
                className="h-11 w-11 border border-[var(--sys-home-border)] border-b-0 bg-[var(--sys-home-bg)] p-0 text-[11px] text-[var(--sys-home-muted)] hover:bg-[var(--sys-home-hover-bg)] hover:text-[var(--sys-home-fg)]"
                title="New workspace"
              >
                +
              </CreateWorkspaceDialogButton>
            }
            onSelectWorkspace={selectWorkspace}
            onLogout={logout}
            isSigningOut={isSigningOut}
          />

          <div className="min-h-0 flex flex-1 flex-col md:flex-row">
            <WorkspaceConversationRail
              scope={{ kind: 'workspace', workspaceId: workspace.id }}
              eyebrow="Workspace"
              title={workspace.name}
              subtitle={`/${workspace.slug}`}
              badgeLabel={`${conversations.length} rooms`}
              headerAction={
                selectedConversation && canManageSelectedConversation ? (
                  <ManageConversationDialogButton
                    workspaceID={workspace.id}
                    conversation={selectedConversation}
                    className="xl:hidden"
                  >
                    Manage room
                  </ManageConversationDialogButton>
                ) : canManageWorkspace ? (
                  <ManageWorkspaceDialogButton
                    workspace={workspace}
                    className="xl:hidden"
                  >
                    Manage workspace
                  </ManageWorkspaceDialogButton>
                ) : null
              }
              sectionLabel="Channels"
              sectionAction={
                <CreateChannelDialogButton workspace={workspace} members={members}>
                  New room
                </CreateChannelDialogButton>
              }
              emptyStateIcon={<Hash className="h-5 w-5" />}
              emptyStateHeading="No rooms yet"
              emptyStateDescription="Create or seed a workspace conversation to turn this shell into a live workspace."
              conversations={conversations}
              selectedConversationID={selectedConversationId}
              conversationsPending={conversationsQuery.isPending}
              conversationsError={
                conversationsQuery.isError ? conversationsQuery.error.message : ''
              }
              eventsLink={`/workspaces/${workspace.id}/events`}
            />

            <section className="min-h-[56vh] min-w-0 flex-1 border-b border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] md:border-b-0">
              <Outlet />
            </section>

            <WorkspaceInfoRail
              workspace={workspace}
              selectedConversation={selectedConversation}
              managementActions={
                selectedConversation && canManageSelectedConversation ? (
                  <ManageConversationDialogButton
                    workspaceID={workspace.id}
                    conversation={selectedConversation}
                    className="w-full justify-center"
                  >
                    Manage room
                  </ManageConversationDialogButton>
                ) : canManageWorkspace ? (
                  <ManageWorkspaceDialogButton
                    workspace={workspace}
                    className="w-full justify-center"
                  >
                    Manage workspace
                  </ManageWorkspaceDialogButton>
                ) : null
              }
              members={members}
              membersPending={membersQuery.isPending}
              membersError={membersQuery.isError ? membersQuery.error.message : ''}
            />
          </div>
        </div>
      </main>
    </WorkspaceRouteContext.Provider>
  )
}
