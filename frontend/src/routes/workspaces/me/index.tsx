import { createFileRoute, Navigate } from '@tanstack/react-router'
import { Alert } from '../../../components/ui/alert'
import { EmptyState } from '../../../components/ui/empty-state'
import { WorkspaceChannelPlaceholder } from '../../../components/workspace/channel-view'
import { useMeRoute } from '../../../lib/workspace-context'

export const Route = createFileRoute('/workspaces/me/')({
  component: WorkspacesMeIndexRoute
})

function WorkspacesMeIndexRoute () {
  const { conversations, conversationsPending, conversationsError } =
    useMeRoute()

  const firstConversation = conversations.at(0)

  if (firstConversation) {
    return (
      <Navigate
        to='/workspaces/me/channels/$conversationId'
        params={{ conversationId: firstConversation.id }}
        replace
      />
    )
  }

  if (conversationsPending) {
    return <WorkspaceChannelPlaceholder />
  }

  if (conversationsError) {
    return (
      <div className='p-6'>
        <Alert variant='destructive'>{conversationsError}</Alert>
      </div>
    )
  }

  return (
    <div className='flex h-full min-h-[56vh] items-center justify-center p-6'>
      <EmptyState
        heading='No chats available'
        description='Direct messages and private group chats will appear here once you join them or create one.'
      />
    </div>
  )
}
