import { createFileRoute } from '@tanstack/react-router'
import { WorkspaceEventsPanel } from '../../../components/workspace/events-panel'
import { useWorkspaceRoute } from '../../../lib/workspace-context'

export const Route = createFileRoute('/workspaces/$workspaceId/events')({
  component: WorkspaceEventsRoute,
})

function WorkspaceEventsRoute() {
  const { workspace } = useWorkspaceRoute()

  return (
    <WorkspaceEventsPanel
      workspaceId={workspace.id}
      workspaceName={workspace.name}
    />
  )
}
