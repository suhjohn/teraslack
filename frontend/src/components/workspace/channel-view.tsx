import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, LoaderCircle, Send } from 'lucide-react'
import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { EmptyState } from '../ui/empty-state'
import { Eyebrow } from '../ui/eyebrow'
import { getErrorMessage } from '../../lib/admin'
import { cn } from '../../lib/utils'
import {
  useWorkspaceApp,
  useWorkspaceRoute,
} from '../../lib/workspace-context'
import {
  ConversationAccessPolicy,
  getListConversationParticipantsQueryKey,
  getListMessagesQueryKey,
  listConversationParticipants,
  listMessages,
  useCreateMessage,
  useMarkConversationRead,
} from '../../lib/openapi'
import type {
  Conversation,
  MessagesCollection,
  User,
  UsersCollection,
  WorkspaceMember,
} from '../../lib/openapi'
import {
  getConversationGlyph,
  getConversationLabel,
  getUserDisplayName,
  getUserMonogram,
  sortMembers,
} from './shell'

const dayDividerFormatter = new Intl.DateTimeFormat(undefined, {
  month: 'long',
  day: 'numeric',
  year: 'numeric',
})

const timeFormatter = new Intl.DateTimeFormat(undefined, {
  hour: 'numeric',
  minute: '2-digit',
})

type WorkspaceChannelViewContentProps = {
  selectedConversation: Conversation | null
  memberUsersById: Map<string, User>
  workspaceMembers?: WorkspaceMember[]
  workspaceMembersPending?: boolean
  workspaceMembersError?: string
}

export function WorkspaceChannelView() {
  const {
    selectedConversation,
    memberUsersById,
    members,
    membersPending,
    membersError,
  } = useWorkspaceRoute()

  return (
    <WorkspaceChannelViewContent
      selectedConversation={selectedConversation}
      memberUsersById={memberUsersById}
      workspaceMembers={members}
      workspaceMembersPending={membersPending}
      workspaceMembersError={membersError}
    />
  )
}

export function WorkspaceChannelViewContent({
  selectedConversation,
  memberUsersById,
  workspaceMembers = [],
  workspaceMembersPending = false,
  workspaceMembersError = '',
}: WorkspaceChannelViewContentProps) {
  const queryClient = useQueryClient()
  const { auth } = useWorkspaceApp()
  const [draft, setDraft] = useState('')
  const [submitError, setSubmitError] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const lastMarkedMessageIDRef = useRef('')

  const conversationID = selectedConversation?.id ?? ''

  const messagesQuery = useQuery<MessagesCollection>({
    queryKey: getListMessagesQueryKey(conversationID, { limit: 100 }),
    queryFn: async () =>
      (await listMessages(conversationID, {
        limit: 100,
      })) as unknown as MessagesCollection,
    enabled: !!conversationID,
    retry: false,
    staleTime: 5_000,
  })

  const participantsQuery = useQuery<UsersCollection>({
    queryKey: getListConversationParticipantsQueryKey(conversationID),
    queryFn: async () =>
      (await listConversationParticipants(conversationID)) as unknown as UsersCollection,
    enabled:
      !!conversationID &&
      selectedConversation?.access_policy === ConversationAccessPolicy.members,
    retry: false,
    staleTime: 30_000,
  })

  const createMessageMutation = useCreateMessage()
  const markConversationReadMutation = useMarkConversationRead()

  const participantUsersByID = useMemo(
    () => new Map((participantsQuery.data?.items ?? []).map((user) => [user.id, user])),
    [participantsQuery.data?.items],
  )

  const messages = useMemo(
    () => [...(messagesQuery.data?.items ?? [])].reverse(),
    [messagesQuery.data?.items],
  )

  const markConversationRead = useEffectEvent(
    async (targetConversationID: string, messageID: string) => {
      try {
        await markConversationReadMutation.mutateAsync({
          conversationId: targetConversationID,
          data: {
            last_read_message_id: messageID,
          },
        })
      } catch {
        lastMarkedMessageIDRef.current = ''
      }
    },
  )

  useEffect(() => {
    const newestMessageID = messages.at(-1)?.id ?? ''
    if (!conversationID || !newestMessageID) {
      return
    }

    if (lastMarkedMessageIDRef.current === newestMessageID) {
      return
    }

    lastMarkedMessageIDRef.current = newestMessageID
    void markConversationRead(conversationID, newestMessageID)
  }, [conversationID, markConversationRead, messages])

  useEffect(() => {
    const element = scrollRef.current
    if (!element) {
      return
    }

    element.scrollTop = element.scrollHeight
  }, [conversationID, messages.length])

  async function handleSubmit() {
    const bodyText = draft.trim()
    if (!conversationID || !bodyText || selectedConversation?.archived) {
      return
    }

    setSubmitError('')

    try {
      await createMessageMutation.mutateAsync({
        conversationId: conversationID,
        data: {
          body_text: bodyText,
        },
      })
      setDraft('')

      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: getListMessagesQueryKey(conversationID),
        }),
        queryClient.invalidateQueries({
          queryKey: ['/conversations'],
        }),
      ])
    } catch (error) {
      setSubmitError(getErrorMessage(error, 'Failed to send message.'))
    }
  }

  if (!selectedConversation) {
    return (
      <div className="flex h-full min-h-[56vh] items-center justify-center p-6">
        <Alert variant="destructive">The selected conversation could not be found.</Alert>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-[56vh] flex-col bg-[var(--sys-home-bg)]">
      <header className="border-b border-[var(--sys-home-border)] px-4 py-4 md:px-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <Eyebrow>Conversation</Eyebrow>
            <div className="mt-2 flex items-center gap-2">
              <span className="text-[14px] font-bold text-[var(--sys-home-fg)]">
                {getConversationGlyph(selectedConversation)}
              </span>
              <h1 className="truncate text-[15px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                {getConversationLabel(selectedConversation)}
              </h1>
            </div>
            {selectedConversation.description?.trim() ? (
              <p className="mt-2 max-w-3xl text-[12px] leading-6 text-[var(--sys-home-muted)]">
                {selectedConversation.description.trim()}
              </p>
            ) : null}
          </div>

          <div className="flex flex-wrap gap-2">
            <ConversationMembersDropdown
              conversation={selectedConversation}
              currentUserID={auth.user.id}
              participants={participantsQuery.data?.items ?? []}
              participantsPending={participantsQuery.isFetching}
              participantsError={
                participantsQuery.isError
                  ? getErrorMessage(participantsQuery.error, 'Failed to load participants.')
                  : ''
              }
              workspaceMembers={workspaceMembers}
              workspaceMembersPending={workspaceMembersPending}
              workspaceMembersError={workspaceMembersError}
            />
            {messagesQuery.isFetching ? (
              <Badge variant="muted">Refreshing</Badge>
            ) : null}
            {selectedConversation.archived ? (
              <Badge variant="warning">Archived</Badge>
            ) : null}
          </div>
        </div>

      </header>

      <div ref={scrollRef} className="flex-1 overflow-y-auto px-4 py-4 md:px-6 md:py-5">
        {messagesQuery.status === 'pending' ? (
          <div className="flex h-full min-h-[32vh] items-center justify-center">
            <span className="inline-flex items-center gap-3 text-[12px] uppercase tracking-[0.06em] text-[var(--sys-home-muted)]">
              <LoaderCircle className="h-4 w-4 animate-spin" />
              Loading messages…
            </span>
          </div>
        ) : null}

        {messagesQuery.isError ? (
          <Alert variant="destructive">
            {getErrorMessage(messagesQuery.error, 'Failed to load messages.')}
          </Alert>
        ) : null}

        {messagesQuery.status === 'success' && messages.length === 0 ? (
          <EmptyState
            heading="No messages yet"
            description="This room is ready. Post the first message to turn the workspace live."
          />
        ) : null}

        {messagesQuery.status === 'success' ? (
          <div className="space-y-1">
            {messages.map((message, index) => {
              const author =
                memberUsersById.get(message.author_user_id) ??
                participantUsersByID.get(message.author_user_id) ??
                (auth.user.id === message.author_user_id ? auth.user : null)
              const previousMessage = messages[index - 1]
              const showDivider =
                index === 0 ||
                !isSameDay(previousMessage.created_at, message.created_at)

              return (
                <div key={message.id}>
                  {showDivider ? (
                    <div className="flex items-center gap-3 py-3">
                      <div className="h-px flex-1 bg-[var(--sys-home-border)]" />
                      <span className="text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                        {dayDividerFormatter.format(new Date(message.created_at))}
                      </span>
                      <div className="h-px flex-1 bg-[var(--sys-home-border)]" />
                    </div>
                  ) : null}

                  <article
                    className={cn(
                      'grid grid-cols-[40px_minmax(0,1fr)] gap-3 border border-transparent px-1 py-3',
                      message.deleted_at && 'opacity-60',
                    )}
                  >
                    <div className="flex h-10 w-10 items-center justify-center border border-[var(--sys-home-border)] text-[11px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]">
                      {getUserMonogram(author, message.author_user_id)}
                    </div>

                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-[12px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                          {getUserDisplayName(author, message.author_user_id)}
                        </span>
                        <span className="text-[11px] text-[var(--sys-home-muted)]">
                          {timeFormatter.format(new Date(message.created_at))}
                        </span>
                        {message.edited_at ? <Badge variant="muted">Edited</Badge> : null}
                      </div>

                      <p className="mt-1 whitespace-pre-wrap break-words text-[13px] leading-6 text-[var(--sys-home-fg)]">
                        {message.deleted_at
                          ? 'Message deleted.'
                          : message.body_text || '[structured message]'}
                      </p>
                    </div>
                  </article>
                </div>
              )
            })}
          </div>
        ) : null}
      </div>

      <footer className="border-t border-[var(--sys-home-border)] px-4 py-4 md:px-6">
        {submitError ? (
          <Alert variant="destructive" className="mb-3">
            {submitError}
          </Alert>
        ) : null}

        <div className="space-y-3">
          <textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== 'Enter' || event.shiftKey) {
                return
              }

              event.preventDefault()
              void handleSubmit()
            }}
            disabled={selectedConversation.archived || createMessageMutation.isPending}
            rows={3}
            placeholder={
              selectedConversation.archived
                ? 'This room is archived.'
                : `Message ${getConversationLabel(selectedConversation)}`
            }
            className="min-h-[92px] w-full resize-y border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-3 py-3 font-[family-name:var(--font-mono)] text-[13px] text-[var(--sys-home-fg)] outline-none focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--sys-home-fg)]"
          />

          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
              Enter sends. Shift+Enter adds a new line.
            </p>
            <div className="flex items-center gap-2">
              <Button
                className="gap-1.5"
                onClick={() => void handleSubmit()}
                disabled={
                  !draft.trim() ||
                  selectedConversation.archived ||
                  createMessageMutation.isPending
                }
              >
                {createMessageMutation.isPending ? (
                  <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Send className="h-3.5 w-3.5" />
                )}
                Send
              </Button>
            </div>
          </div>
        </div>
      </footer>
    </div>
  )
}

function isSameDay(left: string, right: string) {
  return new Date(left).toDateString() === new Date(right).toDateString()
}

function ConversationMembersDropdown({
  conversation,
  currentUserID,
  participants,
  participantsPending,
  participantsError,
  workspaceMembers,
  workspaceMembersPending,
  workspaceMembersError,
}: {
  conversation: Conversation
  currentUserID: string
  participants: User[]
  participantsPending: boolean
  participantsError: string
  workspaceMembers: WorkspaceMember[]
  workspaceMembersPending: boolean
  workspaceMembersError: string
}) {
  const isPrivateConversation =
    conversation.access_policy === ConversationAccessPolicy.members
  const sortedWorkspaceMembers = sortMembers(workspaceMembers)
  const memberCount = isPrivateConversation
    ? conversation.participant_count
    : sortedWorkspaceMembers.length
  const memberCountLabel = `${memberCount} ${memberCount === 1 ? 'member' : 'members'}`

  return (
    <DropdownMenu.Root modal={false}>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-none border border-[var(--sys-home-border)] bg-transparent px-2 py-0.5 font-[family-name:var(--font-mono)] text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)] sys-hover"
          aria-label="Show conversation members"
        >
          <span>{memberCountLabel}</span>
          {(isPrivateConversation ? participantsPending : workspaceMembersPending) ? (
            <LoaderCircle className="h-3 w-3 animate-spin" />
          ) : (
            <ChevronDown className="h-3 w-3" />
          )}
        </button>
      </DropdownMenu.Trigger>

      <DropdownMenu.Portal>
        <DropdownMenu.Content
          sideOffset={8}
          align="end"
          className="z-50 min-w-[280px] border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] font-[family-name:var(--font-mono)] text-[var(--sys-home-fg)] shadow-[0_12px_32px_rgba(0,0,0,0.22)]"
        >
          <div className="border-b border-[var(--sys-home-border)] px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
            Members
          </div>

          {isPrivateConversation && participantsError ? (
            <div className="px-3 py-4 text-[11px] leading-6 text-[#dc2626]">
              {participantsError}
            </div>
          ) : !isPrivateConversation && workspaceMembersError ? (
            <div className="px-3 py-4 text-[11px] leading-6 text-[#dc2626]">
              {workspaceMembersError}
            </div>
          ) : isPrivateConversation && participantsPending && participants.length === 0 ? (
            <div className="flex items-center gap-2 px-3 py-4 text-[11px] text-[var(--sys-home-muted)]">
              <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
              Loading members…
            </div>
          ) : !isPrivateConversation &&
            workspaceMembersPending &&
            sortedWorkspaceMembers.length === 0 ? (
            <div className="flex items-center gap-2 px-3 py-4 text-[11px] text-[var(--sys-home-muted)]">
              <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
              Loading members…
            </div>
          ) : isPrivateConversation && participants.length === 0 ? (
            <div className="px-3 py-4 text-[11px] leading-6 text-[var(--sys-home-muted)]">
              No participants found for this chat.
            </div>
          ) : !isPrivateConversation && sortedWorkspaceMembers.length === 0 ? (
            <div className="px-3 py-4 text-[11px] leading-6 text-[var(--sys-home-muted)]">
              No workspace members found.
            </div>
          ) : (
            <div className="max-h-[320px] overflow-y-auto">
              {isPrivateConversation
                ? participants.map((user) => (
                    <div
                      key={user.id}
                      className="flex items-center gap-3 border-b border-[var(--sys-home-border)] px-3 py-3 last:border-b-0"
                    >
                      <div className="flex h-8 w-8 shrink-0 items-center justify-center border border-[var(--sys-home-border)] text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]">
                        {getUserMonogram(user, user.id)}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[11px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]">
                          {getUserDisplayName(user, user.id)}
                        </div>
                        <div className="mt-1 truncate text-[10px] text-[var(--sys-home-muted)]">
                          {user.profile.handle ? `@${user.profile.handle}` : user.id}
                        </div>
                      </div>
                      {user.id === currentUserID ? (
                        <span className="shrink-0 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                          You
                        </span>
                      ) : null}
                    </div>
                  ))
                : sortedWorkspaceMembers.map((member) => (
                    <div
                      key={member.user_id}
                      className="flex items-center gap-3 border-b border-[var(--sys-home-border)] px-3 py-3 last:border-b-0"
                    >
                      <div className="flex h-8 w-8 shrink-0 items-center justify-center border border-[var(--sys-home-border)] text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]">
                        {getUserMonogram(member.user, member.user_id)}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[11px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]">
                          {getUserDisplayName(member.user, member.user_id)}
                        </div>
                        <div className="mt-1 truncate text-[10px] text-[var(--sys-home-muted)]">
                          {member.user.profile.handle
                            ? `@${member.user.profile.handle}`
                            : member.user_id}
                        </div>
                      </div>
                      <div className="shrink-0 text-right text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                        <div>{member.role}</div>
                        <div>{member.user_id === currentUserID ? 'You' : member.status}</div>
                      </div>
                    </div>
                  ))}
            </div>
          )}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}
