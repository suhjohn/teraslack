import { createFileRoute } from '@tanstack/react-router'
import { WorkspaceChannelViewContent } from '../../../../components/workspace/channel-view'
import { useMeRoute } from '../../../../lib/workspace-context'

export const Route = createFileRoute('/workspaces/me/channels/$conversationId')({
  component: WorkspacesMeChannelRoute,
})

function WorkspacesMeChannelRoute() {
  const { selectedConversation, memberUsersById } = useMeRoute()

  return (
    <WorkspaceChannelViewContent
      selectedConversation={selectedConversation}
      memberUsersById={memberUsersById}
    />
  )
}
