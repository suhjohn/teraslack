import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { LoaderCircle, Plus } from 'lucide-react'
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
import { DetailTile } from '../../components/ui/detail-tile'
import { EmptyState } from '../../components/ui/empty-state'
import { Eyebrow } from '../../components/ui/eyebrow'
import { Input } from '../../components/ui/input'
import { ListItem } from '../../components/ui/list-item'
import { useAdmin, formatDate, getErrorMessage } from '../../lib/admin'
import {
  getListUsergroupsQueryKey,
  useCreateUsergroup,
  useListUsergroupMembers,
  useListUsergroups,
} from '../../lib/openapi'
import type {
  Usergroup,
  UsergroupsCollection,
  StringsCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspace/usergroups')({
  component: UsergroupsPage,
})

function UsergroupsPage() {
  const { workspaceID } = useAdmin()
  const [selectedGroupID, setSelectedGroupID] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const queryClient = useQueryClient()

  const groupsQuery = useListUsergroups<UsergroupsCollection>(
    { workspace_id: workspaceID },
    { query: { enabled: !!workspaceID, retry: false } },
  )

  const groups: Usergroup[] = groupsQuery.data?.items ?? []

  const effectiveGroupID =
    selectedGroupID && groups.some((g) => g.id === selectedGroupID)
      ? selectedGroupID
      : groups[0]?.id ?? ''

  const selectedGroup = groups.find((g) => g.id === effectiveGroupID) ?? null

  const membersQuery = useListUsergroupMembers<StringsCollection>(
    effectiveGroupID,
    { query: { enabled: !!effectiveGroupID, retry: false } },
  )

  return (
    <div className="grid gap-6 xl:grid-cols-12">
      <Card className="p-5 xl:col-span-5">
        <CardHeader className="mb-4 flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div>
            <Eyebrow>Usergroups</Eyebrow>
            <CardTitle>Groups</CardTitle>
            <CardDescription>
              {groups.length} group{groups.length !== 1 ? 's' : ''} in workspace
            </CardDescription>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowCreate((p) => !p)}
          >
            <Plus className="h-3.5 w-3.5" />
            {showCreate ? 'Cancel' : 'Create'}
          </Button>
        </CardHeader>

        {showCreate ? (
          <CreateGroupForm
            workspaceID={workspaceID}
            onDone={() => {
              setShowCreate(false)
              void queryClient.invalidateQueries({
                queryKey: getListUsergroupsQueryKey({ workspace_id: workspaceID }),
              })
            }}
          />
        ) : null}

        <CardContent className="max-h-[70vh] space-y-1 overflow-y-auto pr-1">
          {groupsQuery.isFetching && !groups.length ? (
            <div className="flex min-h-[160px] items-center justify-center text-sm text-[var(--ink-soft)]">
              <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
              Loading…
            </div>
          ) : null}

          {groups.map((group) => (
            <ListItem
              key={group.id}
              selected={group.id === effectiveGroupID}
              onClick={() => setSelectedGroupID(group.id)}
            >
              <div className="min-w-0">
                <div className="truncate text-sm font-semibold text-[var(--ink)]">
                  {group.name}
                </div>
                <div className="truncate text-xs text-[var(--ink-soft)]">
                  @{group.handle}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Badge>{group.user_count} members</Badge>
                {!group.enabled ? (
                  <Badge variant="muted">disabled</Badge>
                ) : null}
              </div>
            </ListItem>
          ))}

          {!groups.length && !groupsQuery.isFetching ? (
            <EmptyState className="min-h-[160px]" description="No usergroups found." />
          ) : null}
        </CardContent>
      </Card>

      <div className="xl:col-span-7">
        {selectedGroup ? (
          <Card className="space-y-5 p-5">
            <CardHeader className="space-y-3">
              <Eyebrow>Group Detail</Eyebrow>
              <CardTitle className="text-2xl">{selectedGroup.name}</CardTitle>
              <CardDescription>
                {selectedGroup.description || 'No description'}
              </CardDescription>

              <div className="grid gap-3 sm:grid-cols-2">
                <DetailTile label="Handle" value={`@${selectedGroup.handle}`} />
                <DetailTile
                  label="Members"
                  value={String(selectedGroup.user_count)}
                />
                <DetailTile
                  label="External"
                  value={selectedGroup.is_external ? 'Yes' : 'No'}
                />
                <DetailTile
                  label="Enabled"
                  value={selectedGroup.enabled ? 'Yes' : 'No'}
                />
                <DetailTile
                  label="Created"
                  value={formatDate(selectedGroup.created_at)}
                />
                <DetailTile
                  label="Updated"
                  value={formatDate(selectedGroup.updated_at)}
                />
              </div>
            </CardHeader>

            <section className="border-t border-[var(--line)] pt-5">
              <Eyebrow className="mb-3">Members (user IDs)</Eyebrow>
              {membersQuery.data?.items?.length ? (
                <div className="max-h-[40vh] space-y-1 overflow-y-auto">
                  {membersQuery.data.items.map((memberId) => (
                    <div
                      key={memberId}
                      className="border border-[var(--line)] px-3 py-2 font-mono text-xs text-[var(--ink)]"
                    >
                      {memberId}
                    </div>
                  ))}
                </div>
              ) : membersQuery.isFetching ? (
                <div className="flex items-center gap-2 py-4 text-sm text-[var(--ink-soft)]">
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Loading members…
                </div>
              ) : (
                <EmptyState className="min-h-[100px]" description="No members." />
              )}
            </section>
          </Card>
        ) : (
          <Card className="flex min-h-[300px] items-center justify-center p-5">
            <p className="text-sm text-[var(--ink-soft)]">
              Select a usergroup to view details.
            </p>
          </Card>
        )}
      </div>
    </div>
  )
}

function CreateGroupForm({
  workspaceID,
  onDone,
}: {
  workspaceID: string
  onDone: () => void
}) {
  const [name, setName] = useState('')
  const [handle, setHandle] = useState('')
  const [description, setDescription] = useState('')
  const [error, setError] = useState('')
  const createMutation = useCreateUsergroup()

  async function handleCreate() {
    if (!name.trim() || !handle.trim()) return
    setError('')
    try {
      await createMutation.mutateAsync({
        data: {
          workspace_id: workspaceID,
          name: name.trim(),
          handle: handle.trim(),
          description: description.trim(),
        },
      })
      onDone()
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create usergroup.'))
    }
  }

  return (
    <div className="mb-4 space-y-3 border border-[var(--line)] bg-[var(--surface)] p-4">
      {error ? <Alert>{error}</Alert> : null}
      <div className="grid gap-3 sm:grid-cols-2">
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Group name"
        />
        <Input
          value={handle}
          onChange={(e) => setHandle(e.target.value)}
          placeholder="Handle (e.g. engineering)"
        />
      </div>
      <Input
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder="Description"
      />
      <div className="flex gap-3">
        <Button
          onClick={() => void handleCreate()}
          disabled={createMutation.isPending || !name.trim() || !handle.trim()}
        >
          {createMutation.isPending ? 'Creating…' : 'Create Group'}
        </Button>
        <Button variant="outline" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </div>
  )
}
