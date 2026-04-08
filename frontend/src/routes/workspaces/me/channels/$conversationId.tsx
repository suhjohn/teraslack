import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { WorkspaceChannelViewContent } from '../../../../components/workspace/channel-view'
import { useMeRoute } from '../../../../lib/workspace-context'

export const Route = createFileRoute('/workspaces/me/channels/$conversationId')({
  component: WorkspacesMeChannelRoute,
})

function WorkspacesMeChannelRoute() {
  const { selectedConversation, conversationsPending, memberUsersById } = useMeRoute()
  const navigate = useNavigate()

  return (
    <WorkspaceChannelViewContent
      selectedConversation={selectedConversation}
      conversationsPending={conversationsPending}
      memberUsersById={memberUsersById}
      onBack={() => navigate({ to: '/workspaces/me' })}
    />
  )
}
