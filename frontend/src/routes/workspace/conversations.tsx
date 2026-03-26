import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  Archive,
  ChevronRight,
  Hash,
  LoaderCircle,
  Lock,
  MessageSquare,
  Plus,
  Trash2,
  Users,
  X,
} from 'lucide-react'
import { startTransition, useDeferredValue, useMemo, useState } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Checkbox } from '../../components/ui/checkbox'
import { Input } from '../../components/ui/input'
import { Select } from '../../components/ui/select'
import { getErrorMessage, useAdmin } from '../../lib/admin'
import {
  ConversationType,
  getGetConversationQueryKey,
  getListConversationMembersQueryKey,
  getListConversationsQueryKey,
  useAddConversationMembers,
  useGetConversation,
  useListConversationMembers,
  useListConversations,
  useListMessages,
  useListUsers,
  useRemoveConversationMember,
  useCreateConversation,
  useUpdateConversation,
} from '../../lib/openapi'
import type {
  Conversation,
  ConversationType as ConversationTypeValue,
  ConversationsCollection,
  Message,
  MessagesCollection,
  StringsCollection,
  User,
  UsersCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspace/conversations')({
  component: ConversationsPage,
})

const textareaClassName =
  'w-full border border-[var(--line)] bg-[var(--surface)] px-3 py-2 text-sm text-[var(--ink)] placeholder:text-[var(--ink-soft)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--ink)]'

function ConversationsPage() {
  const { workspaceID } = useAdmin()
  const [search, setSearch] = useState('')
  const [selectedConversationID, setSelectedConversationID] = useState('')
  const [showDetails, setShowDetails] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const queryClient = useQueryClient()
  const deferredSearch = useDeferredValue(search.trim().toLowerCase())
  const conversationListQueryKey = getListConversationsQueryKey({
    workspace_id: workspaceID,
    limit: 100,
    exclude_archived: false,
  })

  const conversationsQuery = useListConversations<ConversationsCollection>(
    { workspace_id: workspaceID, limit: 100, exclude_archived: false },
    { query: { enabled: !!workspaceID, retry: false } },
  )
  const usersQuery = useListUsers<UsersCollection>(
    { workspace_id: workspaceID, limit: 200 },
    { query: { enabled: !!workspaceID, retry: false, staleTime: 30_000 } },
  )

  const conversations = useMemo(
    () =>
      [...(conversationsQuery.data?.items ?? [])].sort((left, right) => {
        const updatedAtDelta =
          Date.parse(right.updated_at) - Date.parse(left.updated_at)
        if (updatedAtDelta !== 0) {
          return updatedAtDelta
        }
        return left.name.localeCompare(right.name)
      }),
    [conversationsQuery.data?.items],
  )
  const users = usersQuery.data?.items ?? []

  const filteredConversations = useMemo(() => {
    if (!deferredSearch) {
      return conversations
    }

    return conversations.filter((conversation) =>
      [
        conversation.name,
        conversation.id,
        conversation.type,
        conversation.topic.value,
        conversation.purpose.value,
      ]
        .join(' ')
        .toLowerCase()
        .includes(deferredSearch),
    )
  }, [conversations, deferredSearch])

  const selectedConversation =
    selectedConversationID &&
    filteredConversations.some(
      (conversation) => conversation.id === selectedConversationID,
    )
      ? (filteredConversations.find(
          (conversation) => conversation.id === selectedConversationID,
        ) ?? null)
      : (filteredConversations[0] ?? null)

  const effectiveConversationID = selectedConversation?.id ?? ''
  const isFiltering = search.trim().toLowerCase() !== deferredSearch

  return (
    <div className="flex h-full min-h-[600px] overflow-hidden border border-[var(--line)]">
      {/* Sidebar */}
      <div className="flex w-[260px] flex-none flex-col border-r border-[var(--line)] bg-[var(--surface-strong)]">
        <div className="flex flex-none items-center justify-between border-b border-[var(--line)] px-4 py-3">
          <div>
            <h2 className="text-[15px] font-bold text-[var(--ink)]">
              Conversations
            </h2>
            <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
              {conversations.length} total
            </p>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={() => setShowCreate(!showCreate)}
            title="Create channel"
          >
            {showCreate ? (
              <X className="h-4 w-4" />
            ) : (
              <Plus className="h-4 w-4" />
            )}
          </Button>
        </div>

        {showCreate ? (
          <CreateConversationForm
            workspaceID={workspaceID}
            users={users}
            onCreated={(id) => {
              setShowCreate(false)
              void queryClient.invalidateQueries({
                queryKey: conversationListQueryKey,
              })
              startTransition(() => setSelectedConversationID(id))
            }}
            onCancel={() => setShowCreate(false)}
          />
        ) : null}

        <div className="flex-none border-b border-[var(--line)] px-3 py-2">
          <Input
            className="h-7 text-xs"
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search conversations…"
          />
          {isFiltering ? (
            <div className="mt-1.5 flex items-center gap-1.5 text-[11px] text-[var(--ink-soft)]">
              <LoaderCircle className="h-3 w-3 animate-spin" />
              Filtering…
            </div>
          ) : search ? (
            <div className="mt-1.5 text-[11px] text-[var(--ink-soft)]">
              {filteredConversations.length} results
            </div>
          ) : null}
        </div>

        <div className="flex-1 overflow-y-auto">
          {conversationsQuery.isFetching && !conversations.length ? (
            <div className="flex items-center justify-center py-10">
              <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
            </div>
          ) : filteredConversations.length ? (
            <div className="py-1.5">
              {filteredConversations.map((conversation) => (
                <button
                  key={conversation.id}
                  type="button"
                  className={`flex w-full items-center gap-2 px-3 py-1 text-left transition-colors ${
                    conversation.id === effectiveConversationID
                      ? 'bg-[var(--accent-faint)] text-[var(--ink)]'
                      : 'text-[var(--ink-soft)] hover:bg-[var(--accent-faint)]'
                  }`}
                  onClick={() =>
                    startTransition(() =>
                      setSelectedConversationID(conversation.id),
                    )
                  }
                >
                  <span className="flex-none">
                    <ChannelIcon
                      type={conversation.type}
                      className="h-3.5 w-3.5"
                    />
                  </span>
                  <span className="min-w-0 flex-1 truncate text-[13px]">
                    {conversation.name}
                  </span>
                  {conversation.is_archived ? (
                    <Archive className="h-3 w-3 flex-none opacity-40" />
                  ) : null}
                </button>
              ))}
            </div>
          ) : (
            <div className="px-3 py-6 text-center text-xs text-[var(--ink-soft)]">
              No conversations found.
            </div>
          )}
        </div>
      </div>

      {/* Main chat area */}
      <div className="flex min-w-0 flex-1 flex-col">
        {selectedConversation ? (
          <>
            {/* Channel header */}
            <div className="flex flex-none items-center justify-between border-b border-[var(--line)] bg-[var(--surface-strong)] px-5 py-2.5">
              <div className="flex items-center gap-2 min-w-0">
                <ChannelIcon
                  type={selectedConversation.type}
                  className="h-4 w-4 flex-none text-[var(--ink-soft)]"
                />
                <h3 className="truncate text-[15px] font-bold text-[var(--ink)]">
                  {selectedConversation.name}
                </h3>
                {selectedConversation.is_archived ? (
                  <Badge variant="muted" className="flex-none">
                    archived
                  </Badge>
                ) : null}
              </div>
              <div className="flex items-center gap-1.5">
                <Button
                  variant="ghost"
                  size="sm"
                  className="gap-1.5 text-xs text-[var(--ink-soft)]"
                  onClick={() => setShowDetails(!showDetails)}
                >
                  <Users className="h-3.5 w-3.5" />
                  {selectedConversation.num_members}
                </Button>
                <Button
                  variant={showDetails ? 'outline' : 'ghost'}
                  size="icon"
                  onClick={() => setShowDetails(!showDetails)}
                  title={showDetails ? 'Hide details' : 'Show details'}
                >
                  {showDetails ? (
                    <X className="h-4 w-4" />
                  ) : (
                    <ChevronRight className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>

            <div className="flex min-h-0 flex-1">
              {/* Messages */}
              <ConversationMessages
                conversationID={effectiveConversationID}
                conversationSummary={selectedConversation}
                users={users}
              />

              {/* Details panel */}
              {showDetails ? (
                <div className="w-[340px] flex-none overflow-y-auto border-l border-[var(--line)] bg-[var(--surface-strong)]">
                  <ConversationInspector
                    conversationID={effectiveConversationID}
                    conversationSummary={selectedConversation}
                    conversationListQueryKey={conversationListQueryKey}
                    users={users}
                  />
                </div>
              ) : null}
            </div>
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-sm text-[var(--ink-soft)]">
              Select a conversation to view messages.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}

function CreateConversationForm({
  workspaceID,
  users,
  onCreated,
  onCancel,
}: {
  workspaceID: string
  users: User[]
  onCreated: (id: string) => void
  onCancel: () => void
}) {
  const [name, setName] = useState('')
  const [type, setType] = useState<ConversationTypeValue>(
    ConversationType.public_channel,
  )
  const [creatorID, setCreatorID] = useState(users[0]?.id ?? '')
  const [topic, setTopic] = useState('')
  const [purpose, setPurpose] = useState('')
  const [error, setError] = useState('')
  const createConversation = useCreateConversation()

  async function handleCreate() {
    if (!name.trim() || !creatorID) {
      return
    }

    setError('')
    try {
      const result = await createConversation.mutateAsync({
        data: {
          workspace_id: workspaceID,
          name: name.trim(),
          type,
          creator_id: creatorID,
          topic: topic.trim(),
          purpose: purpose.trim(),
        },
      })
      if (result.status !== 201) {
        throw new Error(getErrorMessage(result.data, 'Failed to create conversation.'))
      }
      onCreated(result.data.id)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create conversation.'))
    }
  }

  return (
    <div className="flex-none border-b border-[var(--line)] bg-[var(--surface)] px-3 py-3">
      <div className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
        New channel
      </div>
      <div className="space-y-2">
        {error ? (
          <div className="text-xs text-[#dc2626]">{error}</div>
        ) : null}
        <Input
          className="h-7 text-xs"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Channel name"
          autoFocus
        />
        <Select
          className="h-7 text-xs"
          value={type}
          onChange={(event) =>
            setType(event.target.value as ConversationTypeValue)
          }
        >
          <option value={ConversationType.public_channel}>Public channel</option>
          <option value={ConversationType.private_channel}>
            Private channel
          </option>
          <option value={ConversationType.im}>Direct message</option>
          <option value={ConversationType.mpim}>Group DM</option>
        </Select>
        <Select
          className="h-7 text-xs"
          value={creatorID}
          onChange={(event) => setCreatorID(event.target.value)}
        >
          <option value="">Creator…</option>
          {users.map((user) => (
            <option key={user.id} value={user.id}>
              {getUserLabel(users, user.id)}
            </option>
          ))}
        </Select>
        <Input
          className="h-7 text-xs"
          value={topic}
          onChange={(event) => setTopic(event.target.value)}
          placeholder="Topic (optional)"
        />
        <Input
          className="h-7 text-xs"
          value={purpose}
          onChange={(event) => setPurpose(event.target.value)}
          placeholder="Purpose (optional)"
        />
        <div className="flex gap-2">
          <Button
            size="sm"
            className="flex-1 text-xs"
            onClick={() => void handleCreate()}
            disabled={createConversation.isPending || !name.trim() || !creatorID}
          >
            {createConversation.isPending ? 'Creating…' : 'Create'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="text-xs"
            onClick={onCancel}
          >
            Cancel
          </Button>
        </div>
      </div>
    </div>
  )
}

function ConversationMessages({
  conversationID,
  conversationSummary,
  users,
}: {
  conversationID: string
  conversationSummary: Conversation | null
  users: User[]
}) {
  const conversationQuery = useGetConversation<Conversation>(conversationID, {
    query: { enabled: !!conversationID, retry: false, staleTime: 30_000 },
  })
  const messagesQuery = useListMessages<MessagesCollection>(
    { conversation_id: conversationID, limit: 50 },
    { query: { enabled: !!conversationID, retry: false } },
  )

  const conversation = conversationQuery.data ?? conversationSummary
  const sortedMessages = (messagesQuery.data?.items ?? [])
    .slice()
    .sort(
      (left, right) =>
        Date.parse(left.created_at) - Date.parse(right.created_at),
    )

  if (!conversationID) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          Select a conversation.
        </p>
      </div>
    )
  }

  if (conversationQuery.isFetching && !conversation) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
      </div>
    )
  }

  return (
    <div className="flex min-w-0 flex-1 flex-col">
      {/* Topic bar */}
      {conversation?.topic.value ? (
        <div className="flex-none border-b border-[var(--line)] bg-[var(--surface)] px-5 py-1.5">
          <p className="truncate text-xs text-[var(--ink-soft)]">
            {conversation.topic.value}
          </p>
        </div>
      ) : null}

      {/* Message list */}
      <div className="flex-1 overflow-y-auto px-5 py-4">
        {messagesQuery.isFetching && !sortedMessages.length ? (
          <div className="flex items-center justify-center py-10">
            <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
          </div>
        ) : sortedMessages.length ? (
          <div>
            {sortedMessages.map((message, index, list) => {
              const previous = index > 0 ? list[index - 1] : undefined
              const isGrouped =
                previous &&
                previous.user_id === message.user_id &&
                Date.parse(message.created_at) -
                  Date.parse(previous.created_at) <=
                  5 * 60 * 1000
              const showDayDivider =
                !previous || !isSameDay(previous.created_at, message.created_at)

              return (
                <div key={message.ts}>
                  {showDayDivider ? (
                    <div className="relative my-5 flex items-center justify-center">
                      <div className="absolute inset-x-0 top-1/2 h-px bg-[var(--line)]" />
                      <span className="relative z-10 bg-[var(--surface-strong)] px-4 text-[11px] font-bold uppercase tracking-wide text-[var(--ink-soft)]">
                        {formatDay(message.created_at)}
                      </span>
                    </div>
                  ) : null}

                  <MessageRow
                    message={message}
                    users={users}
                    grouped={!!isGrouped}
                  />
                </div>
              )
            })}
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <MessageSquare className="mb-3 h-8 w-8 text-[var(--ink-soft)] opacity-30" />
            <p className="text-sm text-[var(--ink-soft)]">
              No messages in this conversation yet.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}

function ConversationInspector({
  conversationID,
  conversationSummary,
  conversationListQueryKey,
  users,
}: {
  conversationID: string
  conversationSummary: Conversation | null
  conversationListQueryKey: ReturnType<typeof getListConversationsQueryKey>
  users: User[]
}) {
  const queryClient = useQueryClient()

  const conversationQuery = useGetConversation<Conversation>(conversationID, {
    query: { enabled: !!conversationID, retry: false, staleTime: 30_000 },
  })
  const membersQuery = useListConversationMembers<StringsCollection>(
    conversationID,
    { limit: 100 },
    { query: { enabled: !!conversationID, retry: false } },
  )

  const conversation = conversationQuery.data ?? conversationSummary
  const members = membersQuery.data?.items ?? []

  if (!conversationID) {
    return null
  }

  return (
    <div>
      {conversation ? (
        <ConversationMetadataForm
          key={`meta-${conversation.id}-${conversation.updated_at}`}
          conversation={conversation}
          users={users}
          onSaved={() => {
            void Promise.all([
              queryClient.invalidateQueries({
                queryKey: getGetConversationQueryKey(conversation.id),
              }),
              queryClient.invalidateQueries({
                queryKey: conversationListQueryKey,
              }),
            ])
          }}
        />
      ) : (
        <div className="flex items-center justify-center py-10">
          <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
        </div>
      )}

      <MembersPanel
        key={`members-${conversationID}-${members.join(',')}`}
        conversationID={conversationID}
        conversationListQueryKey={conversationListQueryKey}
        members={members}
        users={users}
        loading={membersQuery.isFetching && !members.length}
      />
    </div>
  )
}

function ConversationMetadataForm({
  conversation,
  users,
  onSaved,
}: {
  conversation: Conversation
  users: User[]
  onSaved: () => void
}) {
  const [name, setName] = useState(conversation.name)
  const [topic, setTopic] = useState(conversation.topic.value)
  const [purpose, setPurpose] = useState(conversation.purpose.value)
  const [archived, setArchived] = useState(conversation.is_archived)
  const [error, setError] = useState('')
  const updateConversation = useUpdateConversation()

  async function save() {
    setError('')
    try {
      await updateConversation.mutateAsync({
        id: conversation.id,
        data: {
          name: name.trim() || undefined,
          topic: topic.trim() || undefined,
          purpose: purpose.trim() || undefined,
          is_archived: archived,
        },
      })
      onSaved()
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to save conversation settings.'))
    }
  }

  return (
    <div>
      <div className="border-b border-[var(--line)] px-4 py-3">
        <h3 className="text-[15px] font-bold text-[var(--ink)]">Details</h3>
      </div>
      <div className="space-y-3 px-4 py-4">
        {error ? <Alert>{error}</Alert> : null}

        <div>
          <label className="mb-1 block text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
            Channel name
          </label>
          <Input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Conversation name"
          />
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
            Topic
          </label>
          <textarea
            className={`min-h-[60px] ${textareaClassName}`}
            value={topic}
            onChange={(event) => setTopic(event.target.value)}
            placeholder="Add a topic"
          />
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
            Purpose
          </label>
          <textarea
            className={`min-h-[72px] ${textareaClassName}`}
            value={purpose}
            onChange={(event) => setPurpose(event.target.value)}
            placeholder="Add a purpose"
          />
        </div>

        <label className="flex items-center gap-2.5 text-sm text-[var(--ink)]">
          <Checkbox
            checked={archived}
            onChange={(event) => setArchived(event.target.checked)}
          />
          Archive this conversation
        </label>

        <div className="space-y-2 border-t border-[var(--line)] pt-3 text-xs text-[var(--ink-soft)]">
          <div className="flex justify-between">
            <span>Type</span>
            <span className="text-[var(--ink)]">
              {conversationTypeLabel(conversation.type)}
            </span>
          </div>
          <div className="flex justify-between">
            <span>Created by</span>
            <span className="text-[var(--ink)]">
              {getUserLabel(users, conversation.creator_id)}
            </span>
          </div>
          <div className="flex justify-between">
            <span>Updated</span>
            <span className="text-[var(--ink)]">
              {formatRelativeTime(conversation.updated_at)}
            </span>
          </div>
          <div className="flex justify-between">
            <span>ID</span>
            <span className="font-mono text-[var(--ink)]">
              {conversation.id}
            </span>
          </div>
        </div>

        <Button
          className="w-full"
          onClick={() => void save()}
          disabled={updateConversation.isPending}
        >
          {updateConversation.isPending ? 'Saving…' : 'Save changes'}
        </Button>
      </div>
    </div>
  )
}

function MembersPanel({
  conversationID,
  conversationListQueryKey,
  members,
  users,
  loading,
}: {
  conversationID: string
  conversationListQueryKey: ReturnType<typeof getListConversationsQueryKey>
  members: string[]
  users: User[]
  loading: boolean
}) {
  const queryClient = useQueryClient()
  const availableUsers = users.filter((user) => !members.includes(user.id))
  const [selectedUserID, setSelectedUserID] = useState(
    availableUsers[0]?.id ?? '',
  )
  const [invitees, setInvitees] = useState('')
  const [error, setError] = useState('')
  const addMembers = useAddConversationMembers()
  const removeMember = useRemoveConversationMember()

  async function refreshMembership() {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: getListConversationMembersQueryKey(conversationID, {
          limit: 100,
        }),
      }),
      queryClient.invalidateQueries({
        queryKey: getGetConversationQueryKey(conversationID),
      }),
      queryClient.invalidateQueries({
        queryKey: conversationListQueryKey,
      }),
    ])
  }

  async function addUserIDs(userIDs: string[], fallbackMessage: string) {
    if (!userIDs.length) {
      return
    }

    setError('')
    try {
      await addMembers.mutateAsync({
        id: conversationID,
        data: { user_ids: userIDs },
      })
      setInvitees('')
      await refreshMembership()
      const nextAvailableUser = availableUsers.find(
        (user) => !userIDs.includes(user.id),
      )
      setSelectedUserID(nextAvailableUser?.id ?? '')
    } catch (err) {
      setError(getErrorMessage(err, fallbackMessage))
    }
  }

  async function remove(userID: string) {
    setError('')
    try {
      await removeMember.mutateAsync({ id: conversationID, userId: userID })
      await refreshMembership()
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to remove member.'))
    }
  }

  return (
    <div>
      <div className="border-y border-[var(--line)] px-4 py-3">
        <div className="flex items-center justify-between">
          <h3 className="text-[15px] font-bold text-[var(--ink)]">Members</h3>
          <span className="text-xs text-[var(--ink-soft)]">
            {members.length}
          </span>
        </div>
      </div>
      <div className="space-y-3 px-4 py-4">
        {error ? <Alert>{error}</Alert> : null}

        {availableUsers.length ? (
          <div className="flex gap-2">
            <Select
              className="flex-1 text-xs"
              value={selectedUserID}
              onChange={(event) => setSelectedUserID(event.target.value)}
            >
              <option value="">Select user…</option>
              {availableUsers.map((user) => (
                <option key={user.id} value={user.id}>
                  {getUserLabel(users, user.id)}
                </option>
              ))}
            </Select>
            <Button
              size="sm"
              onClick={() =>
                void addUserIDs([selectedUserID], 'Failed to add member.')
              }
              disabled={addMembers.isPending || !selectedUserID}
            >
              Add
            </Button>
          </div>
        ) : null}

        <div className="flex gap-2">
          <Input
            className="flex-1 text-xs"
            value={invitees}
            onChange={(event) => setInvitees(event.target.value)}
            placeholder="User IDs, comma-separated"
          />
          <Button
            size="sm"
            onClick={() =>
              void addUserIDs(
                parseCommaSeparated(invitees),
                'Failed to add members.',
              )
            }
            disabled={
              addMembers.isPending || !parseCommaSeparated(invitees).length
            }
          >
            Add
          </Button>
        </div>

        <div className="max-h-[300px] space-y-0.5 overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center py-6">
              <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
            </div>
          ) : members.length ? (
            members.map((userID) => (
              <div
                key={userID}
                className="group flex items-center gap-2.5 rounded-sm px-2 py-1.5 hover:bg-[var(--accent-faint)]"
              >
                <div className="flex h-7 w-7 flex-none items-center justify-center rounded bg-[var(--accent-faint)] text-[10px] font-bold text-[var(--ink-soft)]">
                  {initialsForUser(getUserLabel(users, userID))}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[13px] font-medium text-[var(--ink)]">
                    {getUserLabel(users, userID)}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6 opacity-0 group-hover:opacity-100"
                  onClick={() => void remove(userID)}
                  disabled={removeMember.isPending}
                  title="Remove member"
                >
                  <Trash2 className="h-3 w-3" />
                </Button>
              </div>
            ))
          ) : (
            <p className="py-4 text-center text-xs text-[var(--ink-soft)]">
              No members found.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function MessageRow({
  message,
  users,
  grouped,
}: {
  message: Message
  users: User[]
  grouped: boolean
}) {
  const authorLabel = getUserLabel(users, message.user_id)

  if (grouped) {
    return (
      <div className="group flex gap-3 px-1 py-0.5 hover:bg-[var(--accent-faint)]">
        <div className="w-9 flex-none pt-0.5 text-center text-[10px] text-transparent group-hover:text-[var(--ink-soft)]">
          {formatTime(message.created_at)}
        </div>
        <div className="min-w-0 flex-1">
          <div className="whitespace-pre-wrap text-[15px] leading-[1.46] text-[var(--ink)]">
            {message.text || (
              <span className="italic text-[var(--ink-soft)]">
                No text content
              </span>
            )}
          </div>
          <MessageMeta message={message} />
        </div>
      </div>
    )
  }

  return (
    <div className="group mt-2 flex gap-3 px-1 py-1 hover:bg-[var(--accent-faint)]">
      <div className="flex h-9 w-9 flex-none items-center justify-center rounded-lg bg-[var(--accent-faint)] text-xs font-bold text-[var(--ink-soft)]">
        {initialsForUser(authorLabel)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="text-[15px] font-bold text-[var(--ink)]">
            {authorLabel}
          </span>
          <span className="text-xs text-[var(--ink-soft)]">
            {formatTime(message.created_at)}
          </span>
          {message.is_deleted ? (
            <Badge variant="muted" className="text-[10px]">
              deleted
            </Badge>
          ) : null}
          {message.subtype ? (
            <Badge variant="muted" className="text-[10px]">
              {humanizeEnum(message.subtype)}
            </Badge>
          ) : null}
        </div>
        <div className="mt-0.5 whitespace-pre-wrap text-[15px] leading-[1.46] text-[var(--ink)]">
          {message.text || (
            <span className="italic text-[var(--ink-soft)]">
              No text content
            </span>
          )}
        </div>
        <MessageMeta message={message} />
      </div>
    </div>
  )
}

function MessageMeta({
  message,
}: {
  message: Message
}) {
  const hasReactions = message.reactions && message.reactions.length > 0
  const hasReplies = !!message.reply_count
  const hasEdited = !!message.edited_at

  if (!hasReactions && !hasReplies && !hasEdited) {
    return null
  }

  return (
    <div className="mt-1 flex flex-wrap items-center gap-1.5">
      {message.reactions?.map((reaction) => (
        <span
          key={reaction.name}
          className="inline-flex items-center gap-1 rounded-full border border-[var(--line)] bg-[var(--surface)] px-2 py-0.5 text-[11px] text-[var(--ink-soft)] hover:border-[var(--ink-soft)]"
          title={
            reaction.users.length ? reaction.users.join(', ') : undefined
          }
        >
          <span>{reaction.name}</span>
          <span className="font-medium">{reaction.count}</span>
        </span>
      ))}
      {hasReplies ? (
        <span className="text-xs font-medium text-[color:var(--accent,#1264a3)]">
          {message.reply_count} repl{message.reply_count === 1 ? 'y' : 'ies'}
        </span>
      ) : null}
      {hasEdited ? (
        <span className="text-[11px] text-[var(--ink-soft)]">(edited)</span>
      ) : null}
    </div>
  )
}

function ChannelIcon({
  type,
  className,
}: {
  type: ConversationTypeValue
  className?: string
}) {
  switch (type) {
    case ConversationType.public_channel:
      return <Hash className={className} />
    case ConversationType.private_channel:
      return <Lock className={className} />
    case ConversationType.im:
      return <MessageSquare className={className} />
    case ConversationType.mpim:
      return <Users className={className} />
    default:
      return <Hash className={className} />
  }
}

function parseCommaSeparated(value: string) {
  return value
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)
}

function conversationTypeLabel(type: ConversationTypeValue) {
  switch (type) {
    case ConversationType.public_channel:
      return 'Public channel'
    case ConversationType.private_channel:
      return 'Private channel'
    case ConversationType.im:
      return 'Direct message'
    case ConversationType.mpim:
      return 'Group DM'
    default:
      return humanizeEnum(type)
  }
}

function humanizeEnum(value: string) {
  return value
    .split('_')
    .filter(Boolean)
    .map((part) => part[0].toUpperCase() + part.slice(1))
    .join(' ')
}

function formatRelativeTime(value: string) {
  const timestamp = Date.parse(value)
  if (Number.isNaN(timestamp)) {
    return 'Unknown time'
  }

  const diffMs = timestamp - Date.now()
  const tense = diffMs >= 0 ? 1 : -1
  const absoluteMs = Math.abs(diffMs)

  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ['year', 365 * 24 * 60 * 60 * 1000],
    ['month', 30 * 24 * 60 * 60 * 1000],
    ['week', 7 * 24 * 60 * 60 * 1000],
    ['day', 24 * 60 * 60 * 1000],
    ['hour', 60 * 60 * 1000],
    ['minute', 60 * 1000],
  ]

  for (const [unit, unitMs] of units) {
    if (absoluteMs >= unitMs) {
      const valueForUnit = Math.round(absoluteMs / unitMs) * tense
      return new Intl.RelativeTimeFormat(undefined, {
        numeric: 'auto',
      }).format(valueForUnit, unit)
    }
  }

  return 'just now'
}

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return new Intl.DateTimeFormat(undefined, {
    hour: 'numeric',
    minute: '2-digit',
  }).format(date)
}

function getUserLabel(users: User[], userID: string) {
  const user = users.find((candidate) => candidate.id === userID)
  return user?.display_name || user?.real_name || user?.name || userID
}

function isSameDay(left: string, right: string) {
  return new Date(left).toDateString() === new Date(right).toDateString()
}

function formatDay(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    weekday: 'long',
    month: 'long',
    day: 'numeric',
  }).format(new Date(value))
}

function initialsForUser(label: string) {
  return (
    label
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0].toUpperCase())
      .join('') || 'U'
  )
}
