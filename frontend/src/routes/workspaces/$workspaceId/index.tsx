import { createFileRoute, Navigate } from '@tanstack/react-router'
import { Alert } from '../../../components/ui/alert'
import { EmptyState } from '../../../components/ui/empty-state'
import { useWorkspaceRoute } from '../../../lib/workspace-context'

export const Route = createFileRoute('/workspaces/$workspaceId/')({
  component: WorkspaceIndexRoute,
})

function WorkspaceIndexRoute() {
  const { conversations, conversationsPending, conversationsError, workspace } =
    useWorkspaceRoute()

  const firstConversation = conversations.at(0)

  if (firstConversation) {
    return (
      <Navigate
        to="/workspaces/$workspaceId/channels/$conversationId"
        params={{
          workspaceId: workspace.id,
          conversationId: firstConversation.id,
        }}
        replace
      />
    )
  }

  if (conversationsPending) {
    return (
      <div className="flex h-full min-h-[56vh] items-center justify-center p-6">
        <span className="text-[12px] uppercase tracking-[0.06em] text-[var(--sys-home-muted)]">
          Loading workspace rooms…
        </span>
      </div>
    )
  }

  if (conversationsError) {
    return (
      <div className="p-6">
        <Alert variant="destructive">{conversationsError}</Alert>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-[56vh] items-center justify-center p-6">
      <EmptyState
        heading="No rooms available"
        description="Seed a conversation in this workspace to land users directly in a live channel."
      />
    </div>
  )
}
