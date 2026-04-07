import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { useMemo, useState } from 'react'
import { Alert } from '../ui/alert'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog'
import { FormField } from '../ui/form-field'
import { Input } from '../ui/input'
import { Select } from '../ui/select'
import { getErrorMessage } from '../../lib/admin'
import { useWorkspaceApp } from '../../lib/workspace-context'
import {
  ConversationAccessPolicy,
  CreateConversationRequestAccessPolicy,
  getGetConversationQueryKey,
  getConversationShareLink,
  getListConversationsQueryKey,
  getListWorkspacesQueryKey,
  rotateConversationShareLink,
  useCreateConversation,
  useCreateWorkspace,
  useUpdateConversation,
  useUpdateWorkspace,
} from '../../lib/openapi'
import type {
  Conversation,
  ConversationShareLink,
  ConversationsCollection,
  Workspace,
  WorkspaceMember,
  WorkspacesCollection,
} from '../../lib/openapi'
import { getUserDisplayName, sortConversations, sortMembers } from './shell'

type TriggerVariant = 'default' | 'outline' | 'ghost' | 'destructive' | 'link'
type TriggerSize = 'default' | 'sm' | 'icon'

type TriggerProps = {
  children: ReactNode
  className?: string
  title?: string
  variant?: TriggerVariant
  size?: TriggerSize
}

export function CreateWorkspaceDialogButton({
  children,
  className,
  title,
  variant = 'outline',
  size = 'sm',
}: TriggerProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { selectWorkspace } = useWorkspaceApp()
  const createWorkspaceMutation = useCreateWorkspace()

  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugEdited, setSlugEdited] = useState(false)
  const [error, setError] = useState('')

  function handleOpen() {
    setName('')
    setSlug('')
    setSlugEdited(false)
    setError('')
    setOpen(true)
  }

  async function handleCreate() {
    const trimmedName = name.trim()
    const normalizedSlug = normalizeWorkspaceSlug(slug)

    if (!trimmedName) {
      setError('Workspace name is required.')
      return
    }

    if (!normalizedSlug) {
      setError('Workspace slug is required.')
      return
    }

    setError('')

    try {
      const created = unwrapData<Workspace>(
        await createWorkspaceMutation.mutateAsync({
          data: {
            name: trimmedName,
            slug: normalizedSlug,
          },
        }),
      )

      queryClient.setQueryData<WorkspacesCollection>(
        getListWorkspacesQueryKey(),
        (current) => ({
          items: [...(current?.items ?? []).filter((item) => item.id !== created.id), created],
        }),
      )
      void queryClient.invalidateQueries({ queryKey: getListWorkspacesQueryKey() })

      await selectWorkspace(created.id)
      setOpen(false)
      await navigate({
        to: '/workspaces/$workspaceId',
        params: { workspaceId: created.id },
      })
    } catch (createError) {
      setError(getErrorMessage(createError, 'Failed to create workspace.'))
    }
  }

  return (
    <>
      <Button
        variant={variant}
        size={size}
        className={className}
        title={title}
        onClick={handleOpen}
      >
        {children}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New workspace</DialogTitle>
            <DialogDescription>
              Create a workspace and provision its default general room.
            </DialogDescription>
          </DialogHeader>

          <FormField
            label="Name"
            htmlFor="create-workspace-name"
            error={error.startsWith('Workspace name') ? error : ''}
          >
            <Input
              id="create-workspace-name"
              value={name}
              onChange={(event) => {
                const nextName = event.target.value
                setName(nextName)
                if (!slugEdited) {
                  setSlug(normalizeWorkspaceSlug(nextName))
                }
              }}
              placeholder="Operations"
            />
          </FormField>

          <FormField
            label="Slug"
            htmlFor="create-workspace-slug"
            description="Lowercase letters, numbers, and dashes."
            error={error.startsWith('Workspace slug') ? error : ''}
          >
            <Input
              id="create-workspace-slug"
              value={slug}
              onChange={(event) => {
                setSlugEdited(true)
                setSlug(event.target.value)
              }}
              placeholder="operations"
            />
          </FormField>

          {error &&
          !error.startsWith('Workspace name') &&
          !error.startsWith('Workspace slug') ? (
            <Alert variant="destructive">{error}</Alert>
          ) : null}

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={createWorkspaceMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              onClick={() => void handleCreate()}
              disabled={createWorkspaceMutation.isPending}
            >
              {createWorkspaceMutation.isPending ? 'Creating…' : 'Create workspace'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function ManageWorkspaceDialogButton({
  workspace,
  children,
  className,
  title,
  variant = 'outline',
  size = 'sm',
}: TriggerProps & {
  workspace: Workspace
}) {
  const queryClient = useQueryClient()
  const updateWorkspaceMutation = useUpdateWorkspace()

  const [open, setOpen] = useState(false)
  const [name, setName] = useState(workspace.name)
  const [slug, setSlug] = useState(workspace.slug)
  const [error, setError] = useState('')

  function handleOpen() {
    setName(workspace.name)
    setSlug(workspace.slug)
    setError('')
    setOpen(true)
  }

  async function handleSave() {
    const trimmedName = name.trim()
    const normalizedSlug = normalizeWorkspaceSlug(slug)

    if (!trimmedName) {
      setError('Workspace name is required.')
      return
    }

    if (!normalizedSlug) {
      setError('Workspace slug is required.')
      return
    }

    setError('')

    try {
      const updated = unwrapData<Workspace>(
        await updateWorkspaceMutation.mutateAsync({
          workspaceId: workspace.id,
          data: {
            name: trimmedName,
            slug: normalizedSlug,
          },
        }),
      )

      queryClient.setQueryData<WorkspacesCollection>(
        getListWorkspacesQueryKey(),
        (current) => {
          const currentItems = current?.items ?? []
          const hasMatch = currentItems.some((item) => item.id === updated.id)

          return {
            items: hasMatch
              ? currentItems.map((item) => (item.id === updated.id ? updated : item))
              : [...currentItems, updated],
          }
        },
      )
      void queryClient.invalidateQueries({ queryKey: getListWorkspacesQueryKey() })
      setOpen(false)
    } catch (updateError) {
      setError(getErrorMessage(updateError, 'Failed to update workspace.'))
    }
  }

  return (
    <>
      <Button
        variant={variant}
        size={size}
        className={className}
        title={title}
        onClick={handleOpen}
      >
        {children}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Manage workspace</DialogTitle>
            <DialogDescription>
              Rename the workspace or update its slug.
            </DialogDescription>
          </DialogHeader>

          <FormField label="Name" htmlFor="update-workspace-name">
            <Input
              id="update-workspace-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </FormField>

          <FormField
            label="Slug"
            htmlFor="update-workspace-slug"
            description="Lowercase letters, numbers, and dashes."
          >
            <Input
              id="update-workspace-slug"
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
            />
          </FormField>

          {error ? <Alert variant="destructive">{error}</Alert> : null}

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={updateWorkspaceMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              onClick={() => void handleSave()}
              disabled={updateWorkspaceMutation.isPending}
            >
              {updateWorkspaceMutation.isPending ? 'Saving…' : 'Save workspace'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function CreateChannelDialogButton({
  workspace,
  members,
  children,
  className,
  title,
  variant = 'outline',
  size = 'sm',
}: TriggerProps & {
  workspace: Workspace
  members: WorkspaceMember[]
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { auth } = useWorkspaceApp()
  const createConversationMutation = useCreateConversation()

  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [accessPolicy, setAccessPolicy] = useState<CreateConversationRequestAccessPolicy>(
    CreateConversationRequestAccessPolicy.workspace,
  )
  const [selectedUserIDs, setSelectedUserIDs] = useState<string[]>([])
  const [error, setError] = useState('')

  const inviteableMembers = useMemo(
    () =>
      sortMembers(
        members.filter(
          (member) =>
            member.status === 'active' && member.user_id !== auth.user.id,
        ),
      ),
    [auth.user.id, members],
  )

  function handleOpen() {
    setName('')
    setDescription('')
    setAccessPolicy(CreateConversationRequestAccessPolicy.workspace)
    setSelectedUserIDs([])
    setError('')
    setOpen(true)
  }

  function toggleMember(userID: string) {
    setSelectedUserIDs((current) =>
      current.includes(userID)
        ? current.filter((item) => item !== userID)
        : [...current, userID],
    )
  }

  async function handleCreate() {
    const trimmedName = name.trim()
    const trimmedDescription = description.trim()

    if (!trimmedName) {
      setError('Room name is required.')
      return
    }

    if (
      accessPolicy === CreateConversationRequestAccessPolicy.members &&
      selectedUserIDs.length === 0
    ) {
      setError('Select at least one additional member for a private room.')
      return
    }

    setError('')

    try {
      const created = unwrapData<Conversation>(
        await createConversationMutation.mutateAsync({
          data: {
            workspace_id: workspace.id,
            access_policy: accessPolicy,
            participant_user_ids:
              accessPolicy === CreateConversationRequestAccessPolicy.members
                ? selectedUserIDs
                : undefined,
            title: trimmedName,
            description: trimmedDescription || null,
          },
        }),
      )

      upsertWorkspaceConversation(queryClient, workspace.id, created)
      void queryClient.invalidateQueries({ queryKey: ['/conversations'] })
      setOpen(false)
      await navigate({
        to: '/workspaces/$workspaceId/channels/$conversationId',
        params: {
          workspaceId: workspace.id,
          conversationId: created.id,
        },
      })
    } catch (createError) {
      setError(getErrorMessage(createError, 'Failed to create room.'))
    }
  }

  return (
    <>
      <Button
        variant={variant}
        size={size}
        className={className}
        title={title}
        onClick={handleOpen}
      >
        {children}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>New room</DialogTitle>
            <DialogDescription>
              Create a workspace room or a private member-only room.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 md:grid-cols-2">
            <FormField label="Visibility" htmlFor="create-room-visibility">
              <Select
                id="create-room-visibility"
                value={accessPolicy}
                onChange={(event) => {
                  setAccessPolicy(
                    event.target.value as CreateConversationRequestAccessPolicy,
                  )
                  setSelectedUserIDs([])
                }}
              >
                <option value={CreateConversationRequestAccessPolicy.workspace}>
                  Workspace room
                </option>
                <option value={CreateConversationRequestAccessPolicy.members}>
                  Private room
                </option>
              </Select>
            </FormField>

            <FormField label="Workspace">
              <Input value={workspace.name} readOnly />
            </FormField>

            <FormField
              label="Name"
              htmlFor="create-room-name"
              className="md:col-span-2"
            >
              <Input
                id="create-room-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="incident-ops"
              />
            </FormField>

            <FormField
              label="Description"
              htmlFor="create-room-description"
              className="md:col-span-2"
            >
              <Input
                id="create-room-description"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder="Operational coordination for the current incident."
              />
            </FormField>
          </div>

          {accessPolicy === CreateConversationRequestAccessPolicy.members ? (
            <FormField
              label="Invite members"
              description="Your account is added automatically."
            >
              <div className="max-h-56 overflow-y-auto border border-[var(--sys-home-border)]">
                {inviteableMembers.length ? (
                  inviteableMembers.map((member) => (
                    <label
                      key={member.user_id}
                      className="flex cursor-pointer items-center gap-3 border-b border-[var(--sys-home-border)] px-3 py-3 last:border-b-0"
                    >
                      <Checkbox
                        checked={selectedUserIDs.includes(member.user_id)}
                        onChange={() => toggleMember(member.user_id)}
                      />
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
                    </label>
                  ))
                ) : (
                  <div className="px-3 py-4 text-[11px] text-[var(--sys-home-muted)]">
                    No other active workspace members are available.
                  </div>
                )}
              </div>
            </FormField>
          ) : null}

          {error ? <Alert variant="destructive">{error}</Alert> : null}

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={createConversationMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              onClick={() => void handleCreate()}
              disabled={createConversationMutation.isPending}
            >
              {createConversationMutation.isPending ? 'Creating…' : 'Create room'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function CreateMeConversationWithShareLinkDialogButton({
  children,
  className,
  title,
  variant = 'outline',
  size = 'sm',
}: TriggerProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const createConversationMutation = useCreateConversation()

  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [createdConversation, setCreatedConversation] = useState<Conversation | null>(
    null,
  )
  const [shareLinkURL, setShareLinkURL] = useState('')

  function handleOpen() {
    setName('')
    setDescription('')
    setError('')
    setCopied(false)
    setCreatedConversation(null)
    setShareLinkURL('')
    setOpen(true)
  }

  async function handleCreate() {
    const trimmedName = name.trim()
    const trimmedDescription = description.trim()

    if (!trimmedName) {
      setError('Chat name is required.')
      return
    }

    setError('')

    try {
      const created = unwrapData<Conversation>(
        await createConversationMutation.mutateAsync({
          data: {
            access_policy: CreateConversationRequestAccessPolicy.members,
            title: trimmedName,
            description: trimmedDescription || null,
          },
        }),
      )

      upsertConversationList(
        queryClient,
        {
          access_policy: ConversationAccessPolicy.members,
          limit: 200,
        },
        created,
      )
      queryClient.setQueryData(getGetConversationQueryKey(created.id), created)

      if (!created.share_link) {
        throw new Error('The server did not return a share link for this chat.')
      }

      setCreatedConversation(created)
      setShareLinkURL(getConversationShareURL(created.share_link))
    } catch (createError) {
      setError(getErrorMessage(createError, 'Failed to create chat.'))
    }
  }

  async function handleCopy() {
    if (!shareLinkURL) {
      return
    }

    try {
      await navigator.clipboard.writeText(shareLinkURL)
      setCopied(true)
    } catch {
      setError('Could not copy the share link from this browser session.')
    }
  }

  async function handleOpenChat() {
    if (!createdConversation) {
      return
    }

    setOpen(false)
    await navigate({
      to: '/workspaces/me/channels/$conversationId',
      params: { conversationId: createdConversation.id },
    })
  }

  return (
    <>
      <Button
        variant={variant}
        size={size}
        className={className}
        title={title}
        onClick={handleOpen}
      >
        {children}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New chat</DialogTitle>
            <DialogDescription>
              Create a private group chat and generate a shareable join link.
            </DialogDescription>
          </DialogHeader>

          {createdConversation && shareLinkURL ? (
            <>
              <FormField
                label="Share link"
                description="Anyone with access to this link can join the chat after signing in."
              >
                <Input value={shareLinkURL} readOnly />
              </FormField>

              {copied ? <Alert>Share link copied.</Alert> : null}

              {error ? <Alert variant="destructive">{error}</Alert> : null}

              <DialogFooter className="justify-between">
                <Button variant="outline" onClick={() => void handleCopy()}>
                  Copy link
                </Button>
                <div className="flex items-center gap-2">
                  <Button variant="outline" onClick={() => setOpen(false)}>
                    Done
                  </Button>
                  <Button onClick={() => void handleOpenChat()}>Open chat</Button>
                </div>
              </DialogFooter>
            </>
          ) : (
            <>
              <FormField label="Chat name" htmlFor="create-me-chat-name">
                <Input
                  id="create-me-chat-name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="Planning room"
                />
              </FormField>

              <FormField
                label="Description"
                htmlFor="create-me-chat-description"
                description="Optional context for anyone joining from the invite."
              >
                <Input
                  id="create-me-chat-description"
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  placeholder="Coordination chat for the next launch review."
                />
              </FormField>

              {error ? <Alert variant="destructive">{error}</Alert> : null}

              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setOpen(false)}
                  disabled={createConversationMutation.isPending}
                >
                  Cancel
                </Button>
                <Button
                  onClick={() => void handleCreate()}
                  disabled={createConversationMutation.isPending}
                >
                  {createConversationMutation.isPending ? 'Creating…' : 'Create chat'}
                </Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}

export function CreateConversationShareLinkDialogButton({
  conversation,
  children,
  className,
  title,
  variant = 'outline',
  size = 'sm',
}: TriggerProps & {
  conversation: Conversation
}) {
  const [open, setOpen] = useState(false)
  const [shareLinkURL, setShareLinkURL] = useState('')
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState('')
  const [isLoadingLink, setIsLoadingLink] = useState(false)
  const [isRotatingLink, setIsRotatingLink] = useState(false)

  function handleOpen() {
    setShareLinkURL('')
    setCopied(false)
    setError('')
    setOpen(true)
    void loadCurrentLink()
  }

  async function loadCurrentLink() {
    setError('')
    setIsLoadingLink(true)

    try {
      const shareLink = unwrapData<ConversationShareLink>(
        await getConversationShareLink(conversation.id),
      )
      setShareLinkURL(getConversationShareURL(shareLink))
    } catch (createError) {
      setError(getErrorMessage(createError, 'Failed to load the share link.'))
    } finally {
      setIsLoadingLink(false)
    }
  }

  async function handleRotate() {
    setCopied(false)
    setError('')
    setIsRotatingLink(true)

    try {
      const shareLink = unwrapData<ConversationShareLink>(
        await rotateConversationShareLink(conversation.id),
      )
      setShareLinkURL(getConversationShareURL(shareLink))
    } catch (rotateError) {
      setError(getErrorMessage(rotateError, 'Failed to rotate the share link.'))
    } finally {
      setIsRotatingLink(false)
    }
  }

  async function handleCopy() {
    if (!shareLinkURL) {
      return
    }

    try {
      await navigator.clipboard.writeText(shareLinkURL)
      setCopied(true)
    } catch {
      setError('Could not copy the share link from this browser session.')
    }
  }

  return (
    <>
      <Button
        variant={variant}
        size={size}
        className={className}
        title={title}
        onClick={handleOpen}
      >
        {children}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Share link</DialogTitle>
            <DialogDescription>
              View or rotate the current join link for this member-only chat.
            </DialogDescription>
          </DialogHeader>

          <FormField
            label="Share link"
            description="Share this URL with people who should be able to join."
          >
            <Input
              value={shareLinkURL}
              readOnly
              placeholder={isLoadingLink ? 'Loading current share link…' : ''}
            />
          </FormField>

          {copied ? <Alert>Share link copied.</Alert> : null}
          {error ? <Alert variant="destructive">{error}</Alert> : null}

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => void handleCopy()}
              disabled={!shareLinkURL || isLoadingLink || isRotatingLink}
            >
              Copy link
            </Button>
            <Button
              variant="outline"
              onClick={() => void handleRotate()}
              disabled={isLoadingLink || isRotatingLink}
            >
              {isRotatingLink ? 'Rotating…' : 'Rotate link'}
            </Button>
            <Button onClick={() => setOpen(false)} disabled={isLoadingLink || isRotatingLink}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function ManageConversationDialogButton({
  workspaceID,
  conversation,
  children,
  className,
  title,
  variant = 'outline',
  size = 'sm',
}: TriggerProps & {
  workspaceID: string
  conversation: Conversation
}) {
  const queryClient = useQueryClient()
  const updateConversationMutation = useUpdateConversation()

  const [open, setOpen] = useState(false)
  const [name, setName] = useState(conversation.title ?? '')
  const [description, setDescription] = useState(conversation.description ?? '')
  const [error, setError] = useState('')

  function handleOpen() {
    setName(conversation.title ?? '')
    setDescription(conversation.description ?? '')
    setError('')
    setOpen(true)
  }

  async function handleSave() {
    const trimmedName = name.trim()
    const trimmedDescription = description.trim()

    if (!trimmedName) {
      setError('Room name is required.')
      return
    }

    setError('')

    try {
      const updated = unwrapData<Conversation>(
        await updateConversationMutation.mutateAsync({
          conversationId: conversation.id,
          data: {
            title: trimmedName,
            description: trimmedDescription || null,
          },
        }),
      )

      upsertWorkspaceConversation(queryClient, workspaceID, updated)
      void queryClient.invalidateQueries({ queryKey: ['/conversations'] })
      setOpen(false)
    } catch (updateError) {
      setError(getErrorMessage(updateError, 'Failed to update room.'))
    }
  }

  async function handleArchiveToggle() {
    setError('')

    try {
      const updated = unwrapData<Conversation>(
        await updateConversationMutation.mutateAsync({
          conversationId: conversation.id,
          data: {
            archived: !conversation.archived,
          },
        }),
      )

      upsertWorkspaceConversation(queryClient, workspaceID, updated)
      void queryClient.invalidateQueries({ queryKey: ['/conversations'] })
      setOpen(false)
    } catch (updateError) {
      setError(
        getErrorMessage(
          updateError,
          conversation.archived
            ? 'Failed to restore room.'
            : 'Failed to archive room.',
        ),
      )
    }
  }

  return (
    <>
      <Button
        variant={variant}
        size={size}
        className={className}
        title={title}
        onClick={handleOpen}
      >
        {children}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Manage room</DialogTitle>
            <DialogDescription>
              Rename this room, update its description, or change its archived state.
            </DialogDescription>
          </DialogHeader>

          <FormField label="Visibility">
            <Input value={getConversationVisibilityLabel(conversation)} readOnly />
          </FormField>

          <FormField label="Name" htmlFor="update-room-name">
            <Input
              id="update-room-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </FormField>

          <FormField label="Description" htmlFor="update-room-description">
            <Input
              id="update-room-description"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
            />
          </FormField>

          {error ? <Alert variant="destructive">{error}</Alert> : null}

          <DialogFooter className="justify-between">
            <Button
              variant={conversation.archived ? 'outline' : 'destructive'}
              onClick={() => void handleArchiveToggle()}
              disabled={updateConversationMutation.isPending}
            >
              {updateConversationMutation.isPending
                ? conversation.archived
                  ? 'Restoring…'
                  : 'Archiving…'
                : conversation.archived
                  ? 'Restore room'
                  : 'Archive room'}
            </Button>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                onClick={() => setOpen(false)}
                disabled={updateConversationMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                onClick={() => void handleSave()}
                disabled={updateConversationMutation.isPending}
              >
                {updateConversationMutation.isPending ? 'Saving…' : 'Save room'}
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function normalizeWorkspaceSlug(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function unwrapData<T>(value: { data: T } | T) {
  if (value && typeof value === 'object' && 'data' in value) {
    return value.data
  }

  return value
}

function upsertConversationList(
  queryClient: ReturnType<typeof useQueryClient>,
  params: { workspace_id?: string; access_policy?: ConversationAccessPolicy; limit: number },
  conversation: Conversation,
) {
  queryClient.setQueryData<ConversationsCollection>(
    getListConversationsQueryKey(params),
    (current) => ({
      items: sortConversations([
        ...(current?.items ?? []).filter((item) => item.id !== conversation.id),
        conversation,
      ]),
    }),
  )
}

function upsertWorkspaceConversation(
  queryClient: ReturnType<typeof useQueryClient>,
  workspaceID: string,
  conversation: Conversation,
) {
  upsertConversationList(
    queryClient,
    { workspace_id: workspaceID, limit: 200 },
    conversation,
  )
}

function getConversationVisibilityLabel(conversation: Conversation) {
  if (conversation.access_policy === CreateConversationRequestAccessPolicy.members) {
    return 'Private room'
  }

  if (conversation.access_policy === CreateConversationRequestAccessPolicy.workspace) {
    return 'Workspace room'
  }

  return 'Room'
}

function getConversationShareURL(shareLink: ConversationShareLink) {
  return shareLink.url
}
