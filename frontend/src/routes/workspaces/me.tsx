import { useQueries, useQuery } from '@tanstack/react-query'
import { createFileRoute, Outlet, useRouterState } from '@tanstack/react-router'
import { useMemo } from 'react'
import { Users } from 'lucide-react'
import {
  CreateConversationInviteDialogButton,
  CreateMeConversationWithInviteDialogButton,
  CreateWorkspaceDialogButton,
} from '../../components/workspace/management'
import {
  MeInfoRail,
  WorkspaceConversationRail,
  WorkspaceRail,
  sortConversations,
} from '../../components/workspace/shell'
import { MeRouteContext, useWorkspaceApp } from '../../lib/workspace-context'
import {
  ConversationAccessPolicy,
  getListConversationsQueryKey,
  listConversations,
} from '../../lib/openapi'
import type {
  Conversation,
  ConversationsCollection,
  User,
  Workspace,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspaces/me')({
  component: WorkspacesMeRoute,
})

function WorkspacesMeRoute() {
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const {
    auth,
    workspaces,
    selectWorkspace,
    logout,
    isSigningOut,
  } = useWorkspaceApp()

  const globalConversationsQuery = useQuery<ConversationsCollection>({
    queryKey: getListConversationsQueryKey({
      access_policy: ConversationAccessPolicy.members,
      limit: 200,
    }),
    queryFn: async () =>
      (await listConversations({
        access_policy: ConversationAccessPolicy.members,
        limit: 200,
      })) as unknown as ConversationsCollection,
    retry: false,
    staleTime: 15_000,
  })

  const workspaceConversationQueries = useQueries({
    queries: workspaces.map((workspace) => ({
      queryKey: getListConversationsQueryKey({
        workspace_id: workspace.id,
        access_policy: ConversationAccessPolicy.members,
        limit: 200,
      }),
      queryFn: async () =>
        (await listConversations({
          workspace_id: workspace.id,
          access_policy: ConversationAccessPolicy.members,
          limit: 200,
        })) as unknown as ConversationsCollection,
      retry: false,
      staleTime: 15_000,
    })),
  })

  const conversations = useMemo(() => {
    const itemsByID = new Map<string, Conversation>()

    for (const conversation of globalConversationsQuery.data?.items ?? []) {
      if (conversation.access_policy === ConversationAccessPolicy.members) {
        itemsByID.set(conversation.id, conversation)
      }
    }

    for (const query of workspaceConversationQueries) {
      for (const conversation of query.data?.items ?? []) {
        if (conversation.access_policy === ConversationAccessPolicy.members) {
          itemsByID.set(conversation.id, conversation)
        }
      }
    }

    return sortConversations([...itemsByID.values()])
  }, [globalConversationsQuery.data?.items, workspaceConversationQueries])

  const conversationsPending =
    globalConversationsQuery.isPending ||
    workspaceConversationQueries.some((query) => query.isPending)

  const conversationsError = getFirstErrorMessage([
    globalConversationsQuery.error,
    ...workspaceConversationQueries.map((query) => query.error),
  ])

  const selectedConversationId = useMemo(() => {
    const prefix = '/workspaces/me/channels/'
    if (!pathname.startsWith(prefix)) {
      return ''
    }

    return decodeURIComponent(pathname.slice(prefix.length))
  }, [pathname])

  const selectedConversation =
    conversations.find((conversation) => conversation.id === selectedConversationId) ??
    null

  const memberUsersById = useMemo(() => new Map<string, User>(), [])
  const workspaceMembershipsById = useMemo(
    () =>
      new Map(
        auth.workspaces
          .filter((workspace) => workspace.status === 'active')
          .map((workspace) => [workspace.workspace_id, workspace] as const),
      ),
    [auth.workspaces],
  )

  const workspacesById = useMemo(
    () =>
      new Map<string, Workspace>(
        workspaces.map((workspace) => [workspace.id, workspace] as const),
      ),
    [workspaces],
  )
  const selectedWorkspaceMembership =
    selectedConversation?.workspace_id != null
      ? workspaceMembershipsById.get(selectedConversation.workspace_id) ?? null
      : null
  const inviteableSelectedConversation =
    selectedConversation?.access_policy === ConversationAccessPolicy.members &&
    (selectedConversation.created_by_user_id === auth.user.id ||
      selectedWorkspaceMembership?.role === 'owner' ||
      selectedWorkspaceMembership?.role === 'admin')
      ? selectedConversation
      : null

  return (
    <MeRouteContext.Provider
      value={{
        conversations,
        conversationsPending,
        conversationsError,
        memberUsersById,
        selectedConversationId,
        selectedConversation,
      }}
    >
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto flex min-h-dvh w-full max-w-[1760px] flex-col border-x border-[var(--sys-home-border)] xl:flex-row">
          <WorkspaceRail
            workspaces={workspaces}
            activeScope="me"
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
              scope={{ kind: 'me' }}
              eyebrow="Inbox"
              title="@me"
              subtitle="Direct messages and group chats"
              badgeLabel={`${conversations.length} chats`}
              headerAction={
                inviteableSelectedConversation ? (
                  <CreateConversationInviteDialogButton
                    conversation={inviteableSelectedConversation}
                    className="xl:hidden"
                  >
                    Invite link
                  </CreateConversationInviteDialogButton>
                ) : null
              }
              sectionLabel="Private chats"
              sectionAction={
                <CreateMeConversationWithInviteDialogButton>
                  New chat
                </CreateMeConversationWithInviteDialogButton>
              }
              emptyStateIcon={<Users className="h-5 w-5" />}
              emptyStateHeading="No chats yet"
              emptyStateDescription="Join or seed a direct message or group chat to populate your personal workspace."
              conversations={conversations}
              selectedConversationID={selectedConversationId}
              conversationsPending={conversationsPending}
              conversationsError={conversationsError}
              eventsLink="/workspaces/me/events"
              getConversationFallbackDescription={(conversation) => {
                if (!conversation.workspace_id) {
                  return 'Global direct message'
                }

                const workspace = workspacesById.get(conversation.workspace_id)
                return workspace
                  ? `Workspace ${workspace.slug}`
                  : 'Workspace direct message'
              }}
            />

            <section className="min-h-[56vh] min-w-0 flex-1 border-b border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] md:border-b-0">
              <Outlet />
            </section>

            <MeInfoRail
              conversations={conversations}
              selectedConversation={selectedConversation}
              managementActions={
                inviteableSelectedConversation ? (
                  <CreateConversationInviteDialogButton
                    conversation={inviteableSelectedConversation}
                    className="w-full justify-center"
                  >
                    Invite link
                  </CreateConversationInviteDialogButton>
                ) : null
              }
              workspacesById={workspacesById}
            />
          </div>
        </div>
      </main>
    </MeRouteContext.Provider>
  )
}

function getFirstErrorMessage(errors: Array<unknown>) {
  for (const error of errors) {
    if (error instanceof Error && error.message) {
      return error.message
    }
  }

  return ''
}
