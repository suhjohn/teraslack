import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Outlet, useRouterState } from '@tanstack/react-router'
import { cn } from '../../lib/utils'
import { useEffect, useMemo } from 'react'
import { Hash } from 'lucide-react'
import { Button } from '../../components/ui/button'
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle
} from '../../components/ui/card'
import { Skeleton } from '../../components/ui/skeleton'
import { WorkspaceChannelPlaceholder } from '../../components/workspace/channel-view'
import {
  ConversationArchiveButton,
  CreateChannelDialogButton,
  CreateWorkspaceDialogButton,
  ManageConversationDialogButton,
  ManageWorkspaceDialogButton
} from '../../components/workspace/management'
import {
  WorkspaceConversationRail,
  WorkspaceInfoRail,
  WorkspaceRail,
  sortConversations
} from '../../components/workspace/shell'
import {
  useWorkspaceApp,
  WorkspaceRouteContext
} from '../../lib/workspace-context'
import {
  getListConversationsQueryKey,
  getListWorkspaceMembersQueryKey,
  listConversations,
  listWorkspaceMembers
} from '../../lib/openapi'
import type {
  ConversationsCollection,
  User,
  Workspace,
  WorkspaceMembersCollection
} from '../../lib/openapi'

export const Route = createFileRoute('/workspaces/$workspaceId')({
  component: WorkspaceLayout
})

function WorkspaceLayout () {
  const { workspaceId } = Route.useParams()
  const pathname = useRouterState({
    select: state => state.location.pathname
  })
  const {
    auth,
    authPending,
    workspaces,
    workspacesPending,
    preferredWorkspaceID,
    selectWorkspace,
    logout,
    isSigningOut
  } = useWorkspaceApp()
  const appPending = authPending || workspacesPending

  const workspace =
    workspaces.find(candidate => candidate.id === workspaceId) ?? null
  const shellWorkspace = workspace ?? getLoadingWorkspace(workspaceId)

  useEffect(() => {
    if (!workspace || preferredWorkspaceID === workspace.id) {
      return
    }

    void selectWorkspace(workspace.id)
  }, [preferredWorkspaceID, selectWorkspace, workspace])

  const conversationsQuery = useQuery<ConversationsCollection>({
    queryKey: getListConversationsQueryKey({
      workspace_id: workspaceId,
      limit: 200
    }),
    queryFn: async () =>
      (await listConversations({
        workspace_id: workspaceId,
        limit: 200
      })) as unknown as ConversationsCollection,
    enabled: !appPending && workspace != null,
    retry: false,
    staleTime: 15_000
  })

  const membersQuery = useQuery<WorkspaceMembersCollection>({
    queryKey: getListWorkspaceMembersQueryKey(workspaceId),
    queryFn: async () =>
      (await listWorkspaceMembers(
        workspaceId
      )) as unknown as WorkspaceMembersCollection,
    enabled: !appPending && workspace != null,
    retry: false,
    staleTime: 30_000
  })

  const conversations = useMemo(
    () => sortConversations(conversationsQuery.data?.items ?? []),
    [conversationsQuery.data?.items]
  )
  const conversationsPending = appPending || conversationsQuery.isPending
  const conversationsError = conversationsQuery.isError
    ? conversationsQuery.error.message
    : ''

  const members = membersQuery.data?.items ?? []
  const membersPending = appPending || membersQuery.isPending
  const membersError = membersQuery.isError ? membersQuery.error.message : ''
  const currentMembership =
    auth !== null
      ? members.find(member => member.user_id === auth.user.id) ?? null
      : null
  const canManageWorkspace =
    currentMembership?.role === 'owner' || currentMembership?.role === 'admin'
  const firstConversationID = conversations[0]?.id ?? ''
  const isConversationOpen = pathname.startsWith(
    `/workspaces/${workspaceId}/channels/`
  )

  const selectedConversationId = useMemo(() => {
    const prefix = `/workspaces/${workspaceId}/channels/`
    if (!pathname.startsWith(prefix)) {
      return firstConversationID
    }

    return decodeURIComponent(pathname.slice(prefix.length))
  }, [firstConversationID, pathname, workspaceId])

  const selectedConversation =
    conversations.find(
      conversation => conversation.id === selectedConversationId
    ) ?? null
  const canManageSelectedConversation =
    selectedConversation != null &&
    (canManageWorkspace ||
      selectedConversation.created_by_user_id === auth?.user.id)

  const memberUsersById = useMemo(
    () =>
      new Map<string, User>(
        members.map(member => [member.user_id, member.user] as const)
      ),
    [members]
  )
  const badgeLabel = appPending ? (
    <Skeleton className='h-4 w-16 border border-[var(--sys-home-border)]' />
  ) : (
    `${conversations.length} rooms`
  )
  const loadingManagementActions = (
    <div className='flex flex-col items-center gap-2'>
      <Skeleton className='h-8 w-full border border-[var(--sys-home-border)]' />
      <Skeleton className='h-8 w-full border border-[var(--sys-home-border)]' />
    </div>
  )

  if (!appPending && !workspace) {
    return (
      <main className='admin-shell min-h-dvh bg-[var(--sys-home-bg)]'>
        <div className='mx-auto w-full max-w-[1560px] px-4 py-12'>
          <Card className='rounded-[2rem] p-8'>
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
        workspace: shellWorkspace,
        conversations,
        conversationsPending,
        conversationsError,
        members,
        membersPending,
        membersError,
        memberUsersById,
        selectedConversationId,
        selectedConversation
      }}
    >
      <main className='admin-shell flex h-dvh flex-col overflow-hidden bg-[var(--sys-home-bg)]'>
        <div className='mx-auto flex h-full w-full max-w-[1760px] flex-col border-x border-[var(--sys-home-border)] xl:flex-row'>
          <WorkspaceRail
            workspaces={workspaces}
            workspacesPending={appPending}
            activeScope='workspace'
            activeWorkspaceID={workspace?.id}
            workspaceCreateAction={
              <CreateWorkspaceDialogButton
                variant='ghost'
                size='icon'
                className='h-11 w-11 border border-[var(--sys-home-border)] border-b-0 bg-[var(--sys-home-bg)] p-0 text-[11px] text-[var(--sys-home-muted)] hover:bg-[var(--sys-home-hover-bg)] hover:text-[var(--sys-home-fg)]'
                title='New workspace'
              >
                +
              </CreateWorkspaceDialogButton>
            }
            onSelectWorkspace={selectWorkspace}
            onLogout={logout}
            isSigningOut={isSigningOut}
          />

          <div className='min-h-0 flex flex-1 flex-col md:flex-row'>
            <WorkspaceConversationRail
              className={isConversationOpen ? 'hidden md:block' : ''}
              scope={{ kind: 'workspace', workspaceId: shellWorkspace.id }}
              eyebrow='Workspace'
              title={
                appPending ? (
                  <Skeleton
                    as='span'
                    className='block h-4 w-32 border border-[var(--sys-home-border)]'
                  />
                ) : (
                  shellWorkspace.name
                )
              }
              subtitle={
                appPending ? (
                  <Skeleton
                    as='span'
                    className='mt-1 block h-3 w-24 border border-[var(--sys-home-border)]'
                  />
                ) : (
                  `/${shellWorkspace.slug}`
                )
              }
              badgeLabel={badgeLabel}
              headerAction={
                selectedConversation &&
                canManageSelectedConversation &&
                workspace ? (
                  <ManageConversationDialogButton
                    workspaceID={workspace.id}
                    conversation={selectedConversation}
                    className='xl:hidden'
                  >
                    Manage room
                  </ManageConversationDialogButton>
                ) : canManageWorkspace && workspace ? (
                  <ManageWorkspaceDialogButton
                    workspace={workspace}
                    className='xl:hidden'
                  >
                    Manage workspace
                  </ManageWorkspaceDialogButton>
                ) : null
              }
              sectionLabel='Channels'
              sectionAction={
                workspace && !appPending ? (
                  <CreateChannelDialogButton
                    workspace={workspace}
                    members={members}
                  >
                    New room
                  </CreateChannelDialogButton>
                ) : (
                  <Button variant='outline' size='sm' disabled>
                    New room
                  </Button>
                )
              }
              emptyStateIcon={<Hash className='h-5 w-5' />}
              emptyStateHeading='No rooms yet'
              emptyStateDescription='Create or seed a workspace conversation to turn this shell into a live workspace.'
              conversations={conversations}
              selectedConversationID={selectedConversationId}
              conversationsPending={conversationsPending}
              conversationsError={conversationsError}
              workspaceMemberCount={appPending ? undefined : members.length}
            />

            <section
              className={cn(
                'flex min-h-0 min-w-0 flex-1 flex-col border-b border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] md:border-b-0',
                !isConversationOpen && 'hidden md:flex'
              )}
            >
              {appPending ? <WorkspaceChannelPlaceholder /> : <Outlet />}
            </section>

            <WorkspaceInfoRail
              workspace={shellWorkspace}
              selectedConversation={selectedConversation}
              managementActions={
                selectedConversation &&
                canManageSelectedConversation &&
                workspace ? (
                  <>
                    <ManageConversationDialogButton
                      workspaceID={workspace.id}
                      conversation={selectedConversation}
                      className='w-full justify-center'
                    >
                      Manage room
                    </ManageConversationDialogButton>
                    <ConversationArchiveButton
                      conversation={selectedConversation}
                      className='w-full justify-center'
                    >
                      {selectedConversation.archived
                        ? 'Restore room'
                        : 'Archive room'}
                    </ConversationArchiveButton>
                  </>
                ) : canManageWorkspace && workspace ? (
                  <ManageWorkspaceDialogButton
                    workspace={workspace}
                    className='w-full justify-center'
                  >
                    Manage workspace
                  </ManageWorkspaceDialogButton>
                ) : appPending ? (
                  loadingManagementActions
                ) : null
              }
              members={members}
              workspacePending={appPending}
              membersPending={membersPending}
              membersError={membersError}
            />
          </div>
        </div>
      </main>
    </WorkspaceRouteContext.Provider>
  )
}

function getLoadingWorkspace (workspaceId: string): Workspace {
  return {
    id: workspaceId,
    slug: 'loading',
    name: 'Loading workspace',
    created_by_user_id: '',
    created_at: '',
    updated_at: ''
  }
}
