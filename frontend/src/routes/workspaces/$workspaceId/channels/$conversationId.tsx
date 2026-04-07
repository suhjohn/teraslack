import { createFileRoute } from '@tanstack/react-router'
import { WorkspaceChannelView } from '../../../../components/workspace/channel-view'

export const Route = createFileRoute(
  '/workspaces/$workspaceId/channels/$conversationId',
)({
  component: WorkspaceChannelRoute,
})

function WorkspaceChannelRoute() {
  return <WorkspaceChannelView />
}
