import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import {
  Archive,
  ChevronDown,
  ChevronLeft,
  Link2,
  LoaderCircle,
  Send
} from 'lucide-react'
import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { EmptyState } from '../ui/empty-state'
import { Eyebrow } from '../ui/eyebrow'
import { Skeleton } from '../ui/skeleton'
import { getErrorMessage } from '../../lib/admin'
import { cn } from '../../lib/utils'
import {
  useReadyWorkspaceApp,
  useWorkspaceRoute
} from '../../lib/workspace-context'
import {
  ConversationAccessPolicy,
  getListConversationsQueryKey,
  getListConversationParticipantsQueryKey,
  getListMessagesQueryKey,
  listConversationParticipants,
  listMessages,
  useCreateMessage,
  useMarkConversationRead,
  useUpdateConversation
} from '../../lib/openapi'
import type {
  Conversation,
  ConversationsCollection,
  MessagesCollection,
  User,
  UsersCollection,
  WorkspaceMember
} from '../../lib/openapi'
import {
  getConversationGlyph,
  getConversationLabel,
  getUserPrincipalSections,
  getUserDisplayName,
  getUserMonogram,
  getWorkspaceMemberPrincipalSections,
  sortConversations,
  sortMembers
} from './shell'
import { CreateConversationShareLinkDialogButton } from './management'

const dayDividerFormatter = new Intl.DateTimeFormat(undefined, {
  month: 'long',
  day: 'numeric',
  year: 'numeric'
})

const timeFormatter = new Intl.DateTimeFormat(undefined, {
  hour: 'numeric',
  minute: '2-digit'
})

const loadingMessageSkeletonRowIDs = Array.from(
  { length: 6 },
  (_, index) => index
)

type MetadataEntry = {
  key: string
  value: string
}

type WorkspaceChannelViewContentProps = {
  selectedConversation: Conversation | null
  conversationsPending?: boolean
  memberUsersById: Map<string, User>
  workspaceMembers?: WorkspaceMember[]
  workspaceMembersPending?: boolean
  workspaceMembersError?: string
  onBack?: () => void
}

export function WorkspaceChannelView () {
  const {
    workspace,
    selectedConversation,
    conversationsPending,
    memberUsersById,
    members,
    membersPending,
    membersError
  } = useWorkspaceRoute()
  const navigate = useNavigate()

  return (
    <WorkspaceChannelViewContent
      selectedConversation={selectedConversation}
      conversationsPending={conversationsPending}
      memberUsersById={memberUsersById}
      workspaceMembers={members}
      workspaceMembersPending={membersPending}
      workspaceMembersError={membersError}
      onBack={() =>
        navigate({
          to: '/workspaces/$workspaceId',
          params: { workspaceId: workspace.id }
        })
      }
    />
  )
}

export function WorkspaceChannelViewContent ({
  selectedConversation,
  conversationsPending = false,
  memberUsersById,
  workspaceMembers = [],
  workspaceMembersPending = false,
  workspaceMembersError = '',
  onBack
}: WorkspaceChannelViewContentProps) {
  const queryClient = useQueryClient()
  const { auth } = useReadyWorkspaceApp()
  const [draft, setDraft] = useState('')
  const [submitError, setSubmitError] = useState('')
  const [conversationActionError, setConversationActionError] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const lastMarkedMessageIDRef = useRef('')

  const conversationID = selectedConversation?.id ?? ''

  const messagesQuery = useQuery<MessagesCollection>({
    queryKey: getListMessagesQueryKey(conversationID, { limit: 100 }),
    queryFn: async () =>
      (await listMessages(conversationID, {
        limit: 100
      })) as unknown as MessagesCollection,
    enabled: !!conversationID,
    retry: false,
    staleTime: 5_000
  })

  const participantsQuery = useQuery<UsersCollection>({
    queryKey: getListConversationParticipantsQueryKey(conversationID),
    queryFn: async () =>
      (await listConversationParticipants(
        conversationID
      )) as unknown as UsersCollection,
    enabled:
      !!conversationID &&
      selectedConversation?.access_policy === ConversationAccessPolicy.members,
    retry: false,
    staleTime: 30_000
  })

  const createMessageMutation = useCreateMessage()
  const markConversationReadMutation = useMarkConversationRead()
  const updateConversationMutation = useUpdateConversation()

  const participantUsersByID = useMemo(
    () =>
      new Map(
        (participantsQuery.data?.items ?? []).map(user => [user.id, user])
      ),
    [participantsQuery.data?.items]
  )

  const messages = useMemo(
    () => [...(messagesQuery.data?.items ?? [])].reverse(),
    [messagesQuery.data?.items]
  )
  const selectedWorkspaceMembership =
    selectedConversation?.workspace_id != null
      ? auth.workspaces.find(
          workspace =>
            workspace.workspace_id === selectedConversation.workspace_id &&
            workspace.status === 'active'
        ) ?? null
      : null
  const canManageConversation =
    selectedConversation != null &&
    (selectedConversation.created_by_user_id === auth.user.id ||
      selectedWorkspaceMembership?.role === 'owner' ||
      selectedWorkspaceMembership?.role === 'admin')

  const canShareConversation =
    selectedConversation != null &&
    selectedConversation.access_policy === ConversationAccessPolicy.members

  const markConversationRead = useEffectEvent(
    async (targetConversationID: string, messageID: string) => {
      try {
        await markConversationReadMutation.mutateAsync({
          conversationId: targetConversationID,
          data: {
            last_read_message_id: messageID
          }
        })
      } catch {
        lastMarkedMessageIDRef.current = ''
      }
    }
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

  async function handleSubmit () {
    const bodyText = draft.trim()
    if (!conversationID || !bodyText || selectedConversation?.archived) {
      return
    }

    setSubmitError('')

    try {
      await createMessageMutation.mutateAsync({
        conversationId: conversationID,
        data: {
          body_text: bodyText
        }
      })
      setDraft('')

      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: getListMessagesQueryKey(conversationID)
        }),
        queryClient.invalidateQueries({
          queryKey: ['/conversations']
        })
      ])
    } catch (error) {
      setSubmitError(getErrorMessage(error, 'Failed to send message.'))
    }
  }

  async function handleArchiveToggle () {
    if (!selectedConversation || !canManageConversation) {
      return
    }

    setConversationActionError('')

    try {
      const response = await updateConversationMutation.mutateAsync({
        conversationId: selectedConversation.id,
        data: {
          archived: !selectedConversation.archived
        }
      })
      if (response.status !== 200) {
        throw new Error(response.data.message)
      }
      const updatedConversation = response.data

      queryClient.setQueriesData<ConversationsCollection>(
        { queryKey: getListConversationsQueryKey() },
        current => {
          if (!current) {
            return current
          }

          return {
            ...current,
            items: sortConversations(
              current.items.map(conversation =>
                conversation.id === updatedConversation.id
                  ? updatedConversation
                  : conversation
              )
            )
          }
        }
      )
      void queryClient.invalidateQueries({
        queryKey: getListConversationsQueryKey()
      })
    } catch (error) {
      setConversationActionError(
        getErrorMessage(
          error,
          selectedConversation.archived
            ? 'Failed to restore conversation.'
            : 'Failed to archive conversation.'
        )
      )
    }
  }

  if (!selectedConversation) {
    if (conversationsPending) {
      return <WorkspaceChannelPlaceholder />
    }

    return (
      <div className='flex h-full items-center justify-center p-6'>
        <Alert variant='destructive'>
          The selected conversation could not be found.
        </Alert>
      </div>
    )
  }

  const memberDropdownProps = {
    conversation: selectedConversation,
    currentUserID: auth.user.id,
    participants: participantsQuery.data?.items ?? [],
    participantsPending: participantsQuery.isFetching,
    participantsError: participantsQuery.isError
      ? getErrorMessage(participantsQuery.error, 'Failed to load participants.')
      : '',
    workspaceMembers,
    workspaceMembersPending,
    workspaceMembersError
  }

  return (
    <div className='flex h-full flex-col bg-[var(--sys-home-bg)]'>
      <header className='border-b border-[var(--sys-home-border)] px-4 py-3 md:py-4 md:px-6'>
        {/* Mobile header: back + compact title + icon controls */}
        <div className='flex items-center gap-2 md:hidden'>
          {onBack ? (
            <button
              type='button'
              onClick={onBack}
              className='flex h-8 w-8 shrink-0 items-center justify-center text-[var(--sys-home-muted)] transition-colors hover:text-[var(--sys-home-fg)]'
              aria-label='Back'
            >
              <ChevronLeft className='h-4 w-4' />
            </button>
          ) : null}
          <div className='flex min-w-0 flex-1 items-center gap-1.5'>
            <span className='text-[13px] font-bold text-[var(--sys-home-fg)]'>
              {getConversationGlyph(selectedConversation)}
            </span>
            <h1 className='truncate text-[13px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]'>
              {getConversationLabel(selectedConversation)}
            </h1>
            {selectedConversation.archived ? (
              <Badge variant='warning' className='shrink-0'>
                Archived
              </Badge>
            ) : null}
          </div>
          <div className='flex shrink-0 items-center gap-1'>
            <ConversationMembersDropdown {...memberDropdownProps} />
            {canShareConversation ? (
              <CreateConversationShareLinkDialogButton
                conversation={selectedConversation}
                size='icon'
                variant='ghost'
                className='h-8 w-8'
                title='Share link'
              >
                <Link2 className='h-3.5 w-3.5' />
              </CreateConversationShareLinkDialogButton>
            ) : null}
            {canManageConversation ? (
              <Button
                variant='ghost'
                size='icon'
                className='h-8 w-8'
                title={selectedConversation.archived ? 'Restore' : 'Archive'}
                onClick={() => void handleArchiveToggle()}
                disabled={updateConversationMutation.isPending}
              >
                {updateConversationMutation.isPending ? (
                  <LoaderCircle className='h-3.5 w-3.5 animate-spin' />
                ) : (
                  <Archive className='h-3.5 w-3.5' />
                )}
              </Button>
            ) : null}
          </div>
        </div>

        {/* Desktop header: full layout */}
        <div className='hidden flex-wrap items-start justify-between gap-3 md:flex'>
          <div className='min-w-0'>
            <Eyebrow>Conversation</Eyebrow>
            <div className='mt-2 flex items-center gap-2'>
              <span className='text-[14px] font-bold text-[var(--sys-home-fg)]'>
                {getConversationGlyph(selectedConversation)}
              </span>
              <h1 className='truncate text-[15px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]'>
                {getConversationLabel(selectedConversation)}
              </h1>
            </div>
            {selectedConversation.description?.trim() ? (
              <p className='mt-2 max-w-3xl text-[12px] leading-6 text-[var(--sys-home-muted)]'>
                {selectedConversation.description.trim()}
              </p>
            ) : null}
          </div>

          <div className='flex gap-2'>
            <ConversationMembersDropdown {...memberDropdownProps} />
            {canShareConversation ? (
              <CreateConversationShareLinkDialogButton
                conversation={selectedConversation}
                className='h-8'
              >
                Share link
              </CreateConversationShareLinkDialogButton>
            ) : null}
            {canManageConversation ? (
              <Button
                variant={
                  selectedConversation.archived ? 'outline' : 'destructive'
                }
                size='sm'
                className='xl:hidden'
                onClick={() => void handleArchiveToggle()}
                disabled={updateConversationMutation.isPending}
              >
                {updateConversationMutation.isPending
                  ? selectedConversation.archived
                    ? 'Restoring…'
                    : 'Archiving…'
                  : selectedConversation.archived
                  ? 'Restore'
                  : 'Archive'}
              </Button>
            ) : null}
            {messagesQuery.isFetching ? (
              <Badge variant='muted'>Refreshing</Badge>
            ) : null}
            {selectedConversation.archived ? (
              <Badge variant='warning'>Archived</Badge>
            ) : null}
          </div>
        </div>

        {conversationActionError ? (
          <Alert variant='destructive' className='mt-3'>
            {conversationActionError}
          </Alert>
        ) : null}
      </header>

      <div
        ref={scrollRef}
        className='workspace-pane-scroll flex-1 overflow-y-auto px-4 py-4 md:px-6 md:py-5'
      >
        {messagesQuery.status === 'pending' ? <MessageListSkeleton /> : null}

        {messagesQuery.isError ? (
          <Alert variant='destructive'>
            {getErrorMessage(messagesQuery.error, 'Failed to load messages.')}
          </Alert>
        ) : null}

        {messagesQuery.status === 'success' && messages.length === 0 ? (
          <EmptyState
            heading='No messages yet'
            description='This room is ready. Post the first message to turn the workspace live.'
          />
        ) : null}

        {messagesQuery.status === 'success' ? (
          <div className='space-y-1'>
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
                    <div className='flex items-center gap-3 py-3'>
                      <div className='h-px flex-1 bg-[var(--sys-home-border)]' />
                      <span className='text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
                        {dayDividerFormatter.format(
                          new Date(message.created_at)
                        )}
                      </span>
                      <div className='h-px flex-1 bg-[var(--sys-home-border)]' />
                    </div>
                  ) : null}

                  <article
                    className={cn(
                      'grid grid-cols-[40px_minmax(0,1fr)] gap-3 border border-transparent px-1 py-3',
                      message.deleted_at && 'opacity-60'
                    )}
                  >
                    <div className='flex h-10 w-10 items-center justify-center border border-[var(--sys-home-border)] text-[11px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]'>
                      {getUserMonogram(author, message.author_user_id)}
                    </div>

                    <div className='min-w-0'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <span className='text-[12px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]'>
                          {getUserDisplayName(author, message.author_user_id)}
                        </span>
                        <span className='text-[11px] text-[var(--sys-home-muted)]'>
                          {timeFormatter.format(new Date(message.created_at))}
                        </span>
                        {message.edited_at ? (
                          <Badge variant='muted'>Edited</Badge>
                        ) : null}
                      </div>

                      <AuthorMetadataSummary author={author} />

                      <MessageMetadataSummary metadata={message.metadata} />

                      <p className='mt-1 whitespace-pre-wrap break-words text-[13px] leading-6 text-[var(--sys-home-fg)]'>
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

      <footer className='border-t border-[var(--sys-home-border)] px-4 py-4 md:px-6'>
        {submitError ? (
          <Alert variant='destructive' className='mb-3'>
            {submitError}
          </Alert>
        ) : null}

        <div className='space-y-3'>
          <textarea
            value={draft}
            onChange={event => setDraft(event.target.value)}
            onKeyDown={event => {
              if (event.key !== 'Enter' || event.shiftKey) {
                return
              }

              event.preventDefault()
              void handleSubmit()
            }}
            disabled={
              selectedConversation.archived || createMessageMutation.isPending
            }
            rows={3}
            placeholder={
              selectedConversation.archived
                ? 'This room is archived.'
                : `Message ${getConversationLabel(selectedConversation)}`
            }
            className='min-h-[92px] w-full resize-y border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] px-3 py-3 font-[family-name:var(--font-mono)] text-[13px] text-[var(--sys-home-fg)] outline-none focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--sys-home-fg)]'
          />

          <div className='flex flex-wrap items-center justify-between gap-3'>
            <p className='text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
              Enter sends. Shift+Enter adds a new line.
            </p>
            <div className='flex items-center gap-2'>
              <Button
                className='gap-1.5'
                onClick={() => void handleSubmit()}
                disabled={
                  !draft.trim() ||
                  selectedConversation.archived ||
                  createMessageMutation.isPending
                }
              >
                {createMessageMutation.isPending ? (
                  <LoaderCircle className='h-3.5 w-3.5 animate-spin' />
                ) : (
                  <Send className='h-3.5 w-3.5' />
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

type WorkspaceChannelPlaceholderProps = {
  title?: string
  description?: string
  showComposer?: boolean
}

export function WorkspaceChannelPlaceholder ({
  showComposer = true
}: WorkspaceChannelPlaceholderProps) {
  return (
    <div className='flex h-full min-h-0 flex-col bg-[var(--sys-home-bg)]'>
      <div className='border-b border-[var(--sys-home-border)] px-4 py-4 md:px-6 flex items-start justify-between'>
        <div className='min-w-0'>
          <Eyebrow>Conversation</Eyebrow>
          <Skeleton className='mt-2 h-6 w-40 border border-[var(--sys-home-border)]' />
        </div>
        <div className='flex flex-wrap gap-2'>
          <div className='flex items-center gap-2'>
            <Skeleton className='h-8 w-60 border border-[var(--sys-home-border)]' />
          </div>
        </div>
      </div>
      <div className='workspace-pane-scroll flex-1 overflow-y-auto px-4 py-4 md:px-6 md:py-5'>
        <MessageListSkeleton />
      </div>
      {showComposer ? (
        <footer className='border-t border-[var(--sys-home-border)] px-4 py-4 md:px-6'>
          <ComposerSkeleton />
        </footer>
      ) : null}
    </div>
  )
}

function MessageListSkeleton () {
  return (
    <div aria-hidden='true' className='space-y-1'>
      {loadingMessageSkeletonRowIDs.map((rowID, index) => (
        <div key={rowID}>
          {index === 0 || index === 3 ? (
            <div className='flex items-center gap-3 py-3'>
              <div className='h-px flex-1 bg-[var(--sys-home-border)]' />
              <Skeleton className='h-2 w-28' />
              <div className='h-px flex-1 bg-[var(--sys-home-border)]' />
            </div>
          ) : null}

          <div className='grid grid-cols-[40px_minmax(0,1fr)] gap-3 px-1 py-3'>
            <Skeleton className='h-10 w-10 border border-[var(--sys-home-border)]' />

            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-2'>
                <Skeleton className='h-3 w-28' />
                <Skeleton className='h-3 w-16' />
              </div>
              <div className='mt-2 space-y-2'>
                <Skeleton
                  className={cn(
                    'h-3',
                    index % 2 === 0 ? 'w-[72%]' : 'w-[58%]'
                  )}
                />
                <Skeleton
                  className={cn(
                    'h-3',
                    index % 3 === 0 ? 'w-[84%]' : 'w-[66%]'
                  )}
                />
              </div>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

function ComposerSkeleton () {
  return (
    <div aria-hidden='true' className='space-y-3'>
      <Skeleton className='min-h-[92px] border border-[var(--sys-home-border)]' />
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Skeleton className='h-3 w-52' />
        <Skeleton className='h-9 w-24 border border-[var(--sys-home-border)]' />
      </div>
    </div>
  )
}

function isSameDay (left: string, right: string) {
  return new Date(left).toDateString() === new Date(right).toDateString()
}

function MessageMetadataSummary ({
  metadata
}: {
  metadata?: Record<string, unknown> | null
}) {
  const entries = getMetadataEntries(metadata)

  if (entries.length === 0) {
    return null
  }

  return (
    <div className='mt-1'>
      <div className='group relative inline-flex max-w-full'>
        <div
          tabIndex={0}
          className='inline-flex max-w-full flex-wrap gap-x-3 gap-y-1 text-[10px] leading-4 text-[var(--sys-home-muted)] outline-none'
        >
          {entries.map(entry => (
            <span
              key={entry.key}
              className='max-w-40 cursor-help truncate font-[family-name:var(--font-mono)]'
            >
              {entry.value}
            </span>
          ))}
        </div>

        <div className='pointer-events-none absolute left-0 top-full z-20 mt-2 hidden min-w-60 max-w-[32rem] border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] shadow-[0_14px_40px_rgba(0,0,0,0.28)] group-hover:block group-focus-within:block'>
          <div className='border-b border-[var(--sys-home-border)] px-3 py-2 text-[9px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
            Metadata
          </div>
          <div className='grid gap-y-2 px-3 py-3'>
            {entries.map(entry => (
              <div
                key={entry.key}
                className='grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3'
              >
                <span className='text-[9px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
                  {entry.key}
                </span>
                <span className='min-w-0 break-words font-[family-name:var(--font-mono)] text-[11px] leading-5 text-[var(--sys-home-fg)]'>
                  {entry.value}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

function AuthorMetadataSummary ({ author }: { author?: User | null }) {
  const entries = getAuthorMetadataEntries(author)

  if (entries.length === 0) {
    return null
  }

  return (
    <div className='mt-1'>
      <div className='group relative inline-flex max-w-full'>
        <div
          tabIndex={0}
          className='inline-flex max-w-full flex-wrap items-center text-[10px] leading-4 text-[var(--sys-home-muted)] outline-none'
        >
          {entries.map((entry, index) => (
            <span key={entry.key} className='inline-flex min-w-0 items-center'>
              {index > 0 ? (
                <span
                  aria-hidden='true'
                  className='px-2 text-[var(--sys-home-border)]'
                >
                  ·
                </span>
              ) : null}
              <span className='max-w-40 cursor-help truncate font-[family-name:var(--font-mono)]'>
                {entry.value}
              </span>
            </span>
          ))}
        </div>

        <div className='pointer-events-none absolute left-0 top-full z-20 mt-2 hidden min-w-60 max-w-[32rem] border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] shadow-[0_14px_40px_rgba(0,0,0,0.28)] group-hover:block group-focus-within:block'>
          <div className='border-b border-[var(--sys-home-border)] px-3 py-2 text-[9px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
            Metadata
          </div>
          <div className='grid gap-y-2 px-3 py-3'>
            {entries.map(entry => (
              <div
                key={entry.key}
                className='grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3'
              >
                <span className='text-[9px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
                  {entry.key}
                </span>
                <span className='min-w-0 break-words font-[family-name:var(--font-mono)] text-[11px] leading-5 text-[var(--sys-home-fg)]'>
                  {entry.value}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

function getMetadataEntries (
  metadata?: Record<string, unknown> | null
): MetadataEntry[] {
  if (!metadata) {
    return []
  }

  const entries: MetadataEntry[] = []

  for (const [key, value] of Object.entries(metadata)) {
    appendMetadataEntry(entries, key, value)
  }

  return entries
}

function getAuthorMetadataEntries (author?: User | null): MetadataEntry[] {
  if (!author) {
    return []
  }

  const principalType = formatMessageMetadataValue(author.principal_type)
  const metadataEntries = getMetadataEntries(author.metadata)
  if (!principalType) {
    return metadataEntries
  }

  return [
    {
      key: 'principal_type',
      value: principalType
    },
    ...metadataEntries
  ]
}

function appendMetadataEntry (
  entries: MetadataEntry[],
  key: string,
  value: unknown
) {
  if (isPlainMetadataObject(value)) {
    const nestedEntries = Object.entries(value)
    if (nestedEntries.length === 0) {
      return
    }

    for (const [nestedKey, nestedValue] of nestedEntries) {
      appendMetadataEntry(entries, `${key}.${nestedKey}`, nestedValue)
    }

    return
  }

  const formattedValue = formatMessageMetadataValue(value)
  if (!formattedValue) {
    return
  }

  entries.push({
    key,
    value: formattedValue
  })
}

function isPlainMetadataObject (
  value: unknown
): value is Record<string, unknown> {
  return (
    typeof value === 'object' &&
    value !== null &&
    !Array.isArray(value) &&
    Object.getPrototypeOf(value) === Object.prototype
  )
}

function getConversationActorCountLabel ({
  users,
  isPending
}: {
  users: User[]
  isPending: boolean
}) {
  if (users.length === 0 && isPending) {
    return 'Members'
  }

  const counts = countConversationActors(users)
  return `${formatCountLabel(counts.humanCount, 'human')} · ${formatCountLabel(
    counts.agentCount,
    'agent'
  )}`
}

function countConversationActors (users: User[]) {
  let humanCount = 0
  let agentCount = 0

  for (const user of users) {
    if (user.principal_type === 'agent') {
      agentCount += 1
      continue
    }

    humanCount += 1
  }

  return { humanCount, agentCount }
}

function formatCountLabel (count: number, singular: string) {
  return `${count} ${count === 1 ? singular : `${singular}s`}`
}

function formatMessageMetadataValue (value: unknown) {
  if (typeof value === 'string') {
    return value.replace(/\s+/g, ' ').trim()
  }

  if (
    typeof value === 'number' ||
    typeof value === 'boolean' ||
    typeof value === 'bigint'
  ) {
    return String(value)
  }

  if (value === null) {
    return 'null'
  }

  if (value === undefined) {
    return ''
  }

  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function ConversationMembersDropdown ({
  conversation,
  currentUserID,
  participants,
  participantsPending,
  participantsError,
  workspaceMembers,
  workspaceMembersPending,
  workspaceMembersError
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
  const participantSections = getUserPrincipalSections(participants)
  const workspaceMemberSections = getWorkspaceMemberPrincipalSections(
    sortedWorkspaceMembers
  )
  const memberCountLabel = getConversationActorCountLabel({
    users: isPrivateConversation
      ? participants
      : sortedWorkspaceMembers.map(member => member.user),
    isPending: isPrivateConversation
      ? participantsPending
      : workspaceMembersPending
  })

  return (
    <DropdownMenu.Root modal={false}>
      <DropdownMenu.Trigger asChild>
        <button
          type='button'
          className='h-8 flex items-center justify-between gap-2 rounded-none border border-[var(--sys-home-border)] bg-transparent px-2 py-0.5 font-[family-name:var(--font-mono)] text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)] sys-hover'
          aria-label='Show conversation members'
        >
          <span>{memberCountLabel}</span>
          <span className='flex h-3 w-3 shrink-0 items-center justify-center'>
            {(
              isPrivateConversation
                ? participantsPending
                : workspaceMembersPending
            ) ? (
              <LoaderCircle className='h-3 w-3 animate-spin' />
            ) : (
              <ChevronDown className='h-3 w-3' />
            )}
          </span>
        </button>
      </DropdownMenu.Trigger>

      <DropdownMenu.Portal>
        <DropdownMenu.Content
          sideOffset={8}
          align='end'
          className='z-50 min-w-[280px] border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] font-[family-name:var(--font-mono)] text-[var(--sys-home-fg)] shadow-[0_12px_32px_rgba(0,0,0,0.22)]'
        >
          <div className='border-b border-[var(--sys-home-border)] px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
            {isPrivateConversation ? 'Participants' : 'Members'}
          </div>

          {isPrivateConversation && participantsError ? (
            <div className='px-3 py-4 text-[11px] leading-6 text-[#dc2626]'>
              {participantsError}
            </div>
          ) : !isPrivateConversation && workspaceMembersError ? (
            <div className='px-3 py-4 text-[11px] leading-6 text-[#dc2626]'>
              {workspaceMembersError}
            </div>
          ) : isPrivateConversation &&
            participantsPending &&
            participants.length === 0 ? (
            <div className='flex items-center gap-2 px-3 py-4 text-[11px] text-[var(--sys-home-muted)]'>
              <LoaderCircle className='h-3.5 w-3.5 animate-spin' />
              Loading members…
            </div>
          ) : !isPrivateConversation &&
            workspaceMembersPending &&
            sortedWorkspaceMembers.length === 0 ? (
            <div className='flex items-center gap-2 px-3 py-4 text-[11px] text-[var(--sys-home-muted)]'>
              <LoaderCircle className='h-3.5 w-3.5 animate-spin' />
              Loading members…
            </div>
          ) : isPrivateConversation && participants.length === 0 ? (
            <div className='px-3 py-4 text-[11px] leading-6 text-[var(--sys-home-muted)]'>
              No participants found for this chat.
            </div>
          ) : !isPrivateConversation && sortedWorkspaceMembers.length === 0 ? (
            <div className='px-3 py-4 text-[11px] leading-6 text-[var(--sys-home-muted)]'>
              No workspace members found.
            </div>
          ) : (
            <div className='max-h-[320px] overflow-y-auto'>
              {isPrivateConversation
                ? participantSections.map(section => (
                    <div
                      key={section.key}
                      className='border-b border-[var(--sys-home-border)] last:border-b-0'
                    >
                      <div className='border-b border-[var(--sys-home-border)] px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
                        {`${section.label} · ${section.items.length}`}
                      </div>
                      {section.items.length === 0 ? (
                        <div className='px-3 py-4 text-[11px] leading-6 text-[var(--sys-home-muted)]'>
                          {`No ${section.label.toLowerCase()} in this conversation.`}
                        </div>
                      ) : (
                        section.items.map(user => (
                          <div
                            key={user.id}
                            className='flex items-center gap-3 border-b border-[var(--sys-home-border)] px-3 py-3 last:border-b-0'
                          >
                            <div className='flex h-8 w-8 shrink-0 items-center justify-center border border-[var(--sys-home-border)] text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]'>
                              {getUserMonogram(user, user.id)}
                            </div>
                            <div className='min-w-0 flex-1'>
                              <div className='truncate text-[11px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]'>
                                {getUserDisplayName(user, user.id)}
                              </div>
                              <div className='mt-1 truncate text-[10px] text-[var(--sys-home-muted)]'>
                                {user.profile.handle
                                  ? `@${user.profile.handle}`
                                  : user.id}
                              </div>
                            </div>
                            {user.id === currentUserID ? (
                              <span className='shrink-0 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
                                You
                              </span>
                            ) : null}
                          </div>
                        ))
                      )}
                    </div>
                  ))
                : workspaceMemberSections.map(section => (
                    <div
                      key={section.key}
                      className='border-b border-[var(--sys-home-border)] last:border-b-0'
                    >
                      <div className='border-b border-[var(--sys-home-border)] px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
                        {`${section.label} · ${section.items.length}`}
                      </div>
                      {section.items.length === 0 ? (
                        <div className='px-3 py-4 text-[11px] leading-6 text-[var(--sys-home-muted)]'>
                          {`No ${section.label.toLowerCase()} in this workspace.`}
                        </div>
                      ) : (
                        section.items.map(member => (
                          <div
                            key={member.user_id}
                            className='flex items-center gap-3 border-b border-[var(--sys-home-border)] px-3 py-3 last:border-b-0'
                          >
                            <div className='flex h-8 w-8 shrink-0 items-center justify-center border border-[var(--sys-home-border)] text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]'>
                              {getUserMonogram(member.user, member.user_id)}
                            </div>
                            <div className='min-w-0 flex-1'>
                              <div className='truncate text-[11px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]'>
                                {getUserDisplayName(
                                  member.user,
                                  member.user_id
                                )}
                              </div>
                              <div className='mt-1 truncate text-[10px] text-[var(--sys-home-muted)]'>
                                {member.user.profile.handle
                                  ? `@${member.user.profile.handle}`
                                  : member.user_id}
                              </div>
                            </div>
                            <div className='shrink-0 text-right text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]'>
                              <div>{member.role}</div>
                              <div>
                                {member.user_id === currentUserID
                                  ? 'You'
                                  : member.status}
                              </div>
                            </div>
                          </div>
                        ))
                      )}
                    </div>
                  ))}
            </div>
          )}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}
