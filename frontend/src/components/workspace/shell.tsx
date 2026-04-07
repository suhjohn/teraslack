import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { BookMarked, Home, LoaderCircle, LogOut, ScrollText, Settings2, Users } from 'lucide-react'
import type { ReactNode } from 'react'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { EmptyState } from '../ui/empty-state'
import { Eyebrow } from '../ui/eyebrow'
import { cn } from '../../lib/utils'
import {
  getListConversationParticipantsQueryKey,
  listConversationParticipants,
} from '../../lib/openapi'
import type {
  Conversation,
  ConversationAccessPolicy,
  User,
  UsersCollection,
  Workspace,
  WorkspaceMember,
} from '../../lib/openapi'

const dateFormatter = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
})

const roleOrder: Record<WorkspaceMember['role'], number> = {
  owner: 0,
  admin: 1,
  member: 2,
}

const workspaceRailButtonClassName =
  'workspace-rail-button flex h-11 w-11 shrink-0 items-center justify-center border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] text-[var(--sys-home-muted)] no-underline transition-colors hover:bg-[var(--sys-home-hover-bg)] hover:text-[var(--sys-home-fg)]'

const workspaceRailUtilityButtonClassName =
  'workspace-rail-button inline-flex h-11 w-11 shrink-0 items-center justify-center gap-2 border border-[var(--sys-home-border)] border-b bg-transparent p-0 font-[family-name:var(--font-mono)] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)] no-underline transition-colors sys-hover hover:border-[var(--sys-home-border)]'

type ConversationScope =
  | { kind: 'me' }
  | { kind: 'workspace'; workspaceId: string }

export function sortConversations(conversations: Conversation[]) {
  return [...conversations].sort((left, right) => {
    if (left.archived !== right.archived) {
      return Number(left.archived) - Number(right.archived)
    }

    const leftStamp = Date.parse(left.last_message_at ?? left.updated_at)
    const rightStamp = Date.parse(right.last_message_at ?? right.updated_at)

    if (leftStamp !== rightStamp) {
      return rightStamp - leftStamp
    }

    return getConversationLabel(left).localeCompare(getConversationLabel(right))
  })
}

export function sortMembers(members: WorkspaceMember[]) {
  return [...members].sort((left, right) => {
    if (left.status !== right.status) {
      return left.status === 'active' ? -1 : 1
    }

    if (left.role !== right.role) {
      return roleOrder[left.role] - roleOrder[right.role]
    }

    return getUserDisplayName(left.user, left.user_id).localeCompare(
      getUserDisplayName(right.user, right.user_id),
    )
  })
}

export function getConversationLabel(conversation: Conversation) {
  const title = conversation.title?.trim()
  if (title) {
    return title
  }

  if (conversation.access_policy === 'members') {
    return `direct-${conversation.id.slice(0, 6)}`
  }

  return `channel-${conversation.id.slice(0, 6)}`
}

export function getConversationGlyph(conversation: Conversation) {
  if (conversation.access_policy === 'members') {
    return '@'
  }

  if (conversation.access_policy === 'workspace') {
    return '#'
  }

  return '*'
}

export function getConversationPolicyLabel(
  accessPolicy: ConversationAccessPolicy,
) {
  if (accessPolicy === 'members') {
    return 'Private room'
  }

  if (accessPolicy === 'workspace') {
    return 'Workspace room'
  }

  return 'Authenticated room'
}

export function getUserDisplayName(
  user: User | null | undefined,
  fallbackID: string,
) {
  const displayName = user?.profile.display_name.trim()
  if (displayName) {
    return displayName
  }

  const handle = user?.profile.handle.trim()
  if (handle) {
    return `@${handle}`
  }

  return fallbackID.slice(0, 8)
}

export function getUserMonogram(user: User | null | undefined, fallbackID: string) {
  const label = getUserDisplayName(user, fallbackID).replace(/^@/, '').trim()

  const words = label.split(/\s+/).filter(Boolean)
  if (words.length >= 2) {
    return `${words[0][0]}${words[1][0]}`.toUpperCase()
  }

  return label.slice(0, 2).toUpperCase()
}

export function formatTimestamp(value?: string | null) {
  if (!value) {
    return 'No activity yet'
  }

  return dateFormatter.format(new Date(value))
}

export function WorkspaceRail({
  workspaces,
  activeScope,
  activeWorkspaceID,
  workspaceCreateAction,
  onSelectWorkspace,
  onLogout,
  isSigningOut,
}: {
  workspaces: Workspace[]
  activeScope: 'me' | 'workspace'
  activeWorkspaceID?: string
  workspaceCreateAction?: ReactNode
  onSelectWorkspace: (workspaceID: string) => Promise<void>
  onLogout: () => Promise<void>
  isSigningOut: boolean
}) {
  return (
    <aside className="border-b border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] xl:min-h-dvh xl:w-[84px] xl:shrink-0 xl:border-b-0 xl:border-r">
      <div className="flex items-center gap-2 overflow-x-auto px-3 py-3 xl:min-h-dvh xl:flex-col xl:items-stretch xl:overflow-visible xl:px-3 xl:py-4">
        <div className="flex min-w-0 items-center gap-2 xl:flex-col">
          <Link
            to="/workspaces/me"
            title="@me"
            className={cn(
              workspaceRailButtonClassName,
              'border-b-0',
              'text-[11px] font-bold uppercase tracking-[0.08em]',
              activeScope === 'me' &&
                'bg-[var(--sys-home-accent-bg)] text-[var(--sys-home-accent-fg)]',
            )}
          >
            @
          </Link>

          {workspaces.map((workspace) => {
            const isActive =
              activeScope === 'workspace' && workspace.id === activeWorkspaceID

            return (
              <Link
                key={workspace.id}
                to="/workspaces/$workspaceId"
                params={{ workspaceId: workspace.id }}
                onClick={() => void onSelectWorkspace(workspace.id)}
                title={workspace.name}
                className={cn(
                  workspaceRailButtonClassName,
                  'border-b-0',
                  'text-[11px] font-bold uppercase tracking-[0.08em]',
                  isActive &&
                    'bg-[var(--sys-home-accent-bg)] text-[var(--sys-home-accent-fg)]',
                )}
              >
                {getWorkspaceMonogram(workspace)}
              </Link>
            )
          })}

          {workspaceCreateAction}
        </div>

        <div className="ml-auto flex items-center gap-2 xl:mt-auto xl:ml-0 xl:flex-col">
          <Link
            to="/settings/api-keys"
            data-slot="button"
            className={workspaceRailUtilityButtonClassName}
            title="Settings"
          >
            <Settings2 className="h-4 w-4" />
          </Link>
          <Link
            to="/docs"
            data-slot="button"
            className={workspaceRailUtilityButtonClassName}
            title="Docs"
          >
            <BookMarked className="h-4 w-4" />
          </Link>
          <Link
            to="/"
            data-slot="button"
            className={cn(workspaceRailUtilityButtonClassName, 'xl:hidden')}
            title="Home"
          >
            <Home className="h-4 w-4" />
          </Link>
          <Button
            variant="ghost"
            size="icon"
            className={workspaceRailUtilityButtonClassName}
            onClick={() => void onLogout()}
            disabled={isSigningOut}
            title="Sign out"
          >
            {isSigningOut ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <LogOut className="h-4 w-4" />
            )}
          </Button>
        </div>
      </div>
    </aside>
  )
}

export function WorkspaceConversationRail({
  scope,
  eyebrow,
  title,
  subtitle,
  badgeLabel,
  headerAction,
  sectionLabel,
  sectionAction,
  emptyStateIcon,
  emptyStateHeading,
  emptyStateDescription,
  conversations,
  selectedConversationID,
  conversationsPending,
  conversationsError,
  getConversationFallbackDescription,
  eventsLink,
}: {
  scope: ConversationScope
  eyebrow: string
  title: string
  subtitle: string
  badgeLabel: string
  headerAction?: ReactNode
  sectionLabel: string
  sectionAction?: ReactNode
  emptyStateIcon: ReactNode
  emptyStateHeading: string
  emptyStateDescription: string
  conversations: Conversation[]
  selectedConversationID: string
  conversationsPending: boolean
  conversationsError: string
  getConversationFallbackDescription?: (conversation: Conversation) => string
  eventsLink?: string
}) {
  return (
    <aside className="border-b border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] md:w-[320px] md:shrink-0 md:border-b-0 md:border-r">
      <div className="flex h-full flex-col">
        <div className="border-b border-[var(--sys-home-border)] px-4 py-4">
          <Eyebrow>{eyebrow}</Eyebrow>
          <div className="mt-3 flex items-start justify-between gap-3">
            <div className="min-w-0">
              <h1 className="truncate text-[14px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
                {title}
              </h1>
              <p className="mt-1 text-[11px] uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                {subtitle}
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {headerAction}
              <Badge variant="muted">{badgeLabel}</Badge>
            </div>
          </div>
          {eventsLink ? (
            <Link
              to={eventsLink}
              className="mt-3 inline-flex items-center gap-2 text-[11px] uppercase tracking-[0.06em] text-[var(--sys-home-muted)] no-underline sys-hover"
            >
              <ScrollText className="h-3.5 w-3.5" />
              Events
            </Link>
          ) : null}
        </div>

        <div className="flex flex-1 flex-col overflow-y-auto p-3">
          <div className="mb-2 flex items-center justify-between px-1">
            <span className="text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
              {sectionLabel}
            </span>
            <div className="flex items-center gap-2">
              {sectionAction}
              {conversationsPending ? (
                <LoaderCircle className="h-3.5 w-3.5 animate-spin text-[var(--sys-home-muted)]" />
              ) : null}
            </div>
          </div>

          {conversationsError ? (
            <div className="border border-[#dc2626] px-3 py-3 text-[11px] uppercase tracking-[0.04em] text-[#dc2626]">
              {conversationsError}
            </div>
          ) : null}

          {!conversationsError && conversations.length === 0 && !conversationsPending ? (
            <EmptyState
              icon={emptyStateIcon}
              heading={emptyStateHeading}
              description={emptyStateDescription}
              className="mt-1"
            />
          ) : null}

          <nav className="flex flex-col gap-1">
            {conversations.map((conversation) => {
              const isActive = conversation.id === selectedConversationID
              const description =
                conversation.description?.trim() ||
                getConversationFallbackDescription?.(conversation) ||
                getConversationPolicyLabel(conversation.access_policy)

              const rowClassName = cn(
                'border border-[var(--sys-home-border)] border-b-0 px-3 py-3 text-[12px] text-[var(--sys-home-muted)] no-underline sys-hover',
                isActive &&
                  'bg-[var(--sys-home-accent-bg)] text-[var(--sys-home-accent-fg)]',
                conversation.archived && 'opacity-60',
              )

              const rowContent = (
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-bold text-inherit">
                        {getConversationGlyph(conversation)}
                      </span>
                      <span className="truncate font-bold uppercase tracking-[0.04em] text-inherit">
                        {getConversationLabel(conversation)}
                      </span>
                    </div>
                    <p className="mt-1 truncate text-[11px] text-inherit opacity-80">
                      {description}
                    </p>
                  </div>
                  <div className="shrink-0 text-right text-[10px] uppercase tracking-[0.06em] opacity-70">
                    <div>{conversation.participant_count} users</div>
                    <div className="mt-1">
                      {formatTimestamp(
                        conversation.last_message_at ?? conversation.updated_at,
                      )}
                    </div>
                  </div>
                </div>
              )

              if (scope.kind === 'me') {
                return (
                  <Link
                    key={conversation.id}
                    to="/workspaces/me/channels/$conversationId"
                    params={{ conversationId: conversation.id }}
                    className={rowClassName}
                  >
                    {rowContent}
                  </Link>
                )
              }

              return (
                <Link
                  key={conversation.id}
                  to="/workspaces/$workspaceId/channels/$conversationId"
                  params={{
                    workspaceId: scope.workspaceId,
                    conversationId: conversation.id,
                  }}
                  className={rowClassName}
                >
                  {rowContent}
                </Link>
              )
            })}
          </nav>
        </div>
      </div>
    </aside>
  )
}

export function WorkspaceInfoRail({
  workspace,
  selectedConversation,
  managementActions,
  members,
  membersPending,
  membersError,
}: {
  workspace: Workspace
  selectedConversation: Conversation | null
  managementActions?: ReactNode
  members: WorkspaceMember[]
  membersPending: boolean
  membersError: string
}) {
  const visibleMembers = sortMembers(members).slice(0, 16)

  return (
    <aside className="hidden bg-[var(--sys-home-bg)] xl:flex xl:w-[320px] xl:shrink-0 xl:flex-col xl:border-l xl:border-[var(--sys-home-border)]">
      <div className="border-b border-[var(--sys-home-border)] px-4 py-4">
        <Eyebrow>{selectedConversation ? 'Room info' : 'Workspace info'}</Eyebrow>
        <h2 className="mt-3 text-[14px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
          {selectedConversation
            ? getConversationLabel(selectedConversation)
            : workspace.name}
        </h2>
        {selectedConversation?.description?.trim() ? (
          <p className="mt-1 text-[12px] leading-6 text-[var(--sys-home-muted)]">
            {selectedConversation.description.trim()}
          </p>
        ) : !selectedConversation ? (
          <p className="mt-1 text-[12px] leading-6 text-[var(--sys-home-muted)]">
            {`Workspace ${workspace.slug}`}
          </p>
        ) : null}
        {managementActions ? (
          <div className="mt-4 flex flex-wrap gap-2">{managementActions}</div>
        ) : null}
      </div>

      <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
        {!selectedConversation ? (
          <section className="border border-[var(--sys-home-border)]">
            <div className="border-b border-[var(--sys-home-border)] px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
              Summary
            </div>
            <div className="grid grid-cols-2 gap-px bg-[var(--sys-home-border)]">
              <InfoCell label="Slug" value={workspace.slug} />
              <InfoCell label="Members" value={String(members.length)} />
              <InfoCell
                label="Created"
                value={formatTimestamp(workspace.created_at)}
                className="col-span-2"
              />
            </div>
          </section>
        ) : null}

        <section className="border border-[var(--sys-home-border)]">
          <div className="flex items-center justify-between border-b border-[var(--sys-home-border)] px-3 py-2">
            <span className="text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
              Members
            </span>
            {membersPending ? (
              <LoaderCircle className="h-3.5 w-3.5 animate-spin text-[var(--sys-home-muted)]" />
            ) : (
              <Users className="h-3.5 w-3.5 text-[var(--sys-home-muted)]" />
            )}
          </div>

          {membersError ? (
            <div className="px-3 py-3 text-[11px] uppercase tracking-[0.04em] text-[#dc2626]">
              {membersError}
            </div>
          ) : null}

          {!membersError ? (
            <div className="flex flex-col">
              {visibleMembers.map((member) => (
                <div
                  key={member.user_id}
                  className="flex items-center gap-3 border-b border-[var(--sys-home-border)] px-3 py-3 last:border-b-0"
                >
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center border border-[var(--sys-home-border)] text-[11px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]">
                    {getUserMonogram(member.user, member.user_id)}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[12px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]">
                      {getUserDisplayName(member.user, member.user_id)}
                    </div>
                    <div className="mt-1 truncate text-[11px] text-[var(--sys-home-muted)]">
                      {member.user.profile.handle
                        ? `@${member.user.profile.handle}`
                        : member.user_id}
                    </div>
                  </div>
                  <Badge
                    variant={member.status === 'active' ? 'default' : 'muted'}
                    className="shrink-0"
                  >
                    {member.role}
                  </Badge>
                </div>
              ))}
            </div>
          ) : null}
        </section>
      </div>
    </aside>
  )
}

export function MeInfoRail({
  conversations,
  selectedConversation,
  managementActions,
  workspacesById,
}: {
  conversations: Conversation[]
  selectedConversation: Conversation | null
  managementActions?: ReactNode
  workspacesById: Map<string, Workspace>
}) {
  const conversationID = selectedConversation?.id ?? ''
  const participantsQuery = useQuery<UsersCollection>({
    queryKey: getListConversationParticipantsQueryKey(conversationID),
    queryFn: async () =>
      (await listConversationParticipants(conversationID)) as unknown as UsersCollection,
    enabled: !!conversationID,
    retry: false,
    staleTime: 30_000,
  })

  const participants = participantsQuery.data?.items ?? []
  const workspaceConversationCount = conversations.filter(
    (conversation) => !!conversation.workspace_id,
  ).length
  const selectedWorkspace =
    selectedConversation?.workspace_id != null
      ? workspacesById.get(selectedConversation.workspace_id) ?? null
      : null

  return (
    <aside className="hidden bg-[var(--sys-home-bg)] xl:flex xl:w-[320px] xl:shrink-0 xl:flex-col xl:border-l xl:border-[var(--sys-home-border)]">
      <div className="border-b border-[var(--sys-home-border)] px-4 py-4">
        <Eyebrow>{selectedConversation ? 'Conversation info' : 'Personal inbox'}</Eyebrow>
        <h2 className="mt-3 text-[14px] font-bold uppercase tracking-[0.05em] text-[var(--sys-home-fg)]">
          {selectedConversation ? getConversationLabel(selectedConversation) : '@me'}
        </h2>
        {selectedConversation?.description?.trim() ? (
          <p className="mt-1 text-[12px] leading-6 text-[var(--sys-home-muted)]">
            {selectedConversation.description.trim()}
          </p>
        ) : !selectedConversation ? (
          <p className="mt-1 text-[12px] leading-6 text-[var(--sys-home-muted)]">
            {selectedWorkspace
              ? `Private chat inside ${selectedWorkspace.name}`
              : 'Direct messages and group chats you are part of.'}
          </p>
        ) : null}
        {managementActions ? (
          <div className="mt-4 flex flex-wrap gap-2">{managementActions}</div>
        ) : null}
      </div>

      <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
        {!selectedConversation ? (
          <section className="border border-[var(--sys-home-border)]">
            <div className="border-b border-[var(--sys-home-border)] px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
              Summary
            </div>
            <div className="grid grid-cols-2 gap-px bg-[var(--sys-home-border)]">
              <InfoCell label="Chats" value={String(conversations.length)} />
              <InfoCell label="Workspace chats" value={String(workspaceConversationCount)} />
              <InfoCell
                label="Global chats"
                value={String(conversations.length - workspaceConversationCount)}
              />
              <InfoCell label="Scope" value={selectedWorkspace?.slug ?? '@me'} />
            </div>
          </section>
        ) : null}

        <section className="border border-[var(--sys-home-border)]">
          <div className="flex items-center justify-between border-b border-[var(--sys-home-border)] px-3 py-2">
            <span className="text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
              People
            </span>
            {participantsQuery.isFetching ? (
              <LoaderCircle className="h-3.5 w-3.5 animate-spin text-[var(--sys-home-muted)]" />
            ) : (
              <Users className="h-3.5 w-3.5 text-[var(--sys-home-muted)]" />
            )}
          </div>

          {participantsQuery.isError ? (
            <div className="px-3 py-3 text-[11px] uppercase tracking-[0.04em] text-[#dc2626]">
              {participantsQuery.error.message}
            </div>
          ) : null}

          {!selectedConversation ? (
            <div className="px-3 py-4 text-[11px] leading-6 text-[var(--sys-home-muted)]">
              Open a direct message or group chat to inspect its participants here.
            </div>
          ) : null}

          {selectedConversation && !participantsQuery.isError ? (
            <div className="flex flex-col">
              {participants.map((user) => (
                <div
                  key={user.id}
                  className="flex items-center gap-3 border-b border-[var(--sys-home-border)] px-3 py-3 last:border-b-0"
                >
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center border border-[var(--sys-home-border)] text-[11px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-fg)]">
                    {getUserMonogram(user, user.id)}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[12px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]">
                      {getUserDisplayName(user, user.id)}
                    </div>
                    <div className="mt-1 truncate text-[11px] text-[var(--sys-home-muted)]">
                      {user.profile.handle ? `@${user.profile.handle}` : user.id}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          ) : null}
        </section>
      </div>
    </aside>
  )
}

function InfoCell({
  label,
  value,
  className,
}: {
  label: string
  value: string
  className?: string
}) {
  return (
    <div
      className={cn(
        'bg-[var(--sys-home-bg)] px-3 py-3 text-[11px] text-[var(--sys-home-muted)]',
        className,
      )}
    >
      <div className="text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
        {label}
      </div>
      <div className="mt-2 text-[12px] font-bold uppercase tracking-[0.04em] text-[var(--sys-home-fg)]">
        {value}
      </div>
    </div>
  )
}

function getWorkspaceMonogram(workspace: Workspace) {
  const parts = workspace.name
    .split(/\s+/)
    .map((part) => part.trim())
    .filter(Boolean)

  if (parts.length >= 2) {
    return `${parts[0][0]}${parts[1][0]}`.toUpperCase()
  }

  return workspace.slug.slice(0, 2).toUpperCase()
}
