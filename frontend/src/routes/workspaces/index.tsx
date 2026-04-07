import { createFileRoute, Navigate } from '@tanstack/react-router'

export const Route = createFileRoute('/workspaces/')({
  component: WorkspacesIndexRoute,
})

function WorkspacesIndexRoute() {
  return <Navigate to="/workspaces/me" replace />
}
