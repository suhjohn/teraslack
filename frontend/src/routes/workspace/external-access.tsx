import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { LoaderCircle, Unlink } from 'lucide-react'
import { useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../../components/ui/card'
import { EmptyState } from '../../components/ui/empty-state'
import { Eyebrow } from '../../components/ui/eyebrow'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../../components/ui/table'
import { useAdmin, formatDate, getErrorMessage } from '../../lib/admin'
import {
  getListExternalWorkspacesQueryKey,
  useDisconnectExternalWorkspace,
  useListExternalWorkspaces,
} from '../../lib/openapi'
import type { ExternalWorkspacesCollection } from '../../lib/openapi'

export const Route = createFileRoute('/workspace/external-access')({
  component: ExternalAccessPage,
})

function ExternalAccessPage() {
  const { workspaceID } = useAdmin()
  const queryClient = useQueryClient()
  const [error, setError] = useState('')

  const externalWorkspacesQuery =
    useListExternalWorkspaces<ExternalWorkspacesCollection>(
    workspaceID,
    { query: { enabled: !!workspaceID, retry: false } },
    )

  const disconnectMutation = useDisconnectExternalWorkspace()

  async function handleDisconnect(externalWorkspaceID: string) {
    setError('')
    try {
      await disconnectMutation.mutateAsync({
        id: workspaceID,
        externalWorkspaceId: externalWorkspaceID,
      })
      await queryClient.invalidateQueries({
        queryKey: getListExternalWorkspacesQueryKey(workspaceID),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to disconnect external workspace.'))
    }
  }

  const externalWorkspaces = externalWorkspacesQuery.data?.items ?? []

  return (
    <div className="space-y-6">
      {error ? <Alert>{error}</Alert> : null}

      <Card className="p-5">
        <CardHeader className="mb-4">
          <Eyebrow>External Access</Eyebrow>
          <CardTitle>Connected External Workspaces</CardTitle>
          <CardDescription>
            {externalWorkspaces.length} external workspace
            {externalWorkspaces.length !== 1 ? 's' : ''} connected
          </CardDescription>
        </CardHeader>
        <CardContent>
          {externalWorkspacesQuery.isFetching && !externalWorkspaces.length ? (
            <EmptyState className="min-h-[160px]" description="Loading…">
              <LoaderCircle className="h-4 w-4 animate-spin" />
            </EmptyState>
          ) : !externalWorkspaces.length ? (
            <EmptyState className="min-h-[160px]" description="No external workspaces connected." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>External ID</TableHead>
                  <TableHead>Connection</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Connected</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {externalWorkspaces.map((workspace) => (
                  <TableRow key={workspace.id}>
                    <TableCell className="font-medium">{workspace.name}</TableCell>
                    <TableCell className="font-mono text-xs text-[var(--ink-soft)]">
                      {workspace.external_workspace_id}
                    </TableCell>
                    <TableCell className="text-[var(--ink-soft)]">
                      {workspace.connection_type}
                    </TableCell>
                    <TableCell>
                      {workspace.connected ? (
                        <Badge variant="success">connected</Badge>
                      ) : (
                        <Badge variant="muted">disconnected</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-[var(--ink-soft)]">
                      {formatDate(workspace.created_at)}
                    </TableCell>
                    <TableCell>
                      {workspace.connected ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => void handleDisconnect(workspace.id)}
                          disabled={disconnectMutation.isPending}
                        >
                          <Unlink className="h-3.5 w-3.5" />
                          Disconnect
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
