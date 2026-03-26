import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { LoaderCircle } from 'lucide-react'
import { useState } from 'react'
import type { ComponentProps } from 'react'
import { Alert } from '../../components/ui/alert'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { CodeBlock } from '../../components/ui/code-block'
import { Input } from '../../components/ui/input'
import { Select } from '../../components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../../components/ui/table'
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '../../components/ui/tabs'
import { useAdmin, formatDate, getErrorMessage } from '../../lib/admin'
import {
  getListWorkspacesQueryKey,
  getGetWorkspaceQueryKey,
  useCreateWorkspace,
  useGetWorkspace,
  useGetWorkspaceBillableInfo,
  useGetWorkspaceBilling,
  useGetWorkspacePreferences,
  useListWorkspaceProfileFields,
  useTransferPrimaryAdmin,
  useUpdateWorkspace,
} from '../../lib/openapi'
import type {
  FreeFormObject,
  Workspace,
  WorkspaceBillableInfoMap,
  WorkspaceBilling,
  WorkspaceDiscoverability,
  WorkspaceProfileFieldsCollection,
} from '../../lib/openapi'

export const Route = createFileRoute('/workspace/settings')({
  validateSearch: (search: Record<string, unknown>) => ({
    create:
      search.create === true ||
      search.create === 'true' ||
      search.create === '1',
  }),
  component: WorkspaceSettingsPage,
})

type WorkspaceDataTab = 'billable' | 'billing' | 'preferences'

function WorkspaceSettingsPage() {
  const navigate = useNavigate({ from: '/workspace/settings' })
  const search = Route.useSearch()
  const { workspaceID, selectWorkspace } = useAdmin()
  const [activeDataTab, setActiveDataTab] =
    useState<WorkspaceDataTab>('billable')
  const isCreatingWorkspace = search.create

  const workspaceQuery = useGetWorkspace<Workspace>(workspaceID, {
    query: { enabled: !!workspaceID, retry: false, staleTime: 30_000 },
  })
  const billableQuery = useGetWorkspaceBillableInfo<WorkspaceBillableInfoMap>(
    workspaceID,
    {
      query: { enabled: !!workspaceID, retry: false },
    },
  )
  const billingQuery = useGetWorkspaceBilling<WorkspaceBilling>(workspaceID, {
    query: { enabled: !!workspaceID, retry: false },
  })
  const preferencesQuery = useGetWorkspacePreferences<FreeFormObject>(workspaceID, {
    query: { enabled: !!workspaceID, retry: false },
  })
  const profileFieldsQuery =
    useListWorkspaceProfileFields<WorkspaceProfileFieldsCollection>(workspaceID, {
      query: { enabled: !!workspaceID, retry: false },
    })

  const workspace = workspaceQuery.data

  const hasWorkspaceData =
    !!billableQuery.data || !!billingQuery.data || !!preferencesQuery.data

  if (isCreatingWorkspace) {
    return (
      <CreateWorkspacePanel
        onCancel={() =>
          void navigate({
            to: '/workspace/settings',
            search: { create: false },
            replace: true,
          })
        }
        onCreated={async (workspaceID) => {
          await selectWorkspace(workspaceID)
          void navigate({
            to: '/workspace/settings',
            search: { create: false },
            replace: true,
          })
        }}
      />
    )
  }

  if (!workspaceID) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          Select a workspace from the sidebar to inspect workspace settings, or{' '}
          <Link
            to="/workspace/settings"
            search={{ create: true }}
            className="text-[var(--ink)] underline underline-offset-4"
          >
            create a new workspace
          </Link>
          .
        </p>
      </div>
    )
  }

  if (workspaceQuery.isFetching && !workspace) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <span className="inline-flex items-center gap-2 text-sm text-[var(--ink-soft)]">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          Loading workspace…
        </span>
      </div>
    )
  }

  if (!workspace) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center">
        <p className="text-sm text-[var(--ink-soft)]">
          The selected workspace could not be loaded.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <WorkspaceDetails key={workspace.id} workspace={workspace} />

      {profileFieldsQuery.data?.items.length ? (
        <ProfileFields fields={profileFieldsQuery.data.items} />
      ) : null}

      {hasWorkspaceData ? (
        <WorkspaceData
          activeTab={activeDataTab}
          billable={billableQuery.data}
          billing={billingQuery.data}
          preferences={preferencesQuery.data}
          onTabChange={(value) => setActiveDataTab(value)}
        />
      ) : null}

      <TransferPrimaryAdmin workspaceID={workspaceID} />
    </div>
  )
}

function CreateWorkspacePanel({
  onCancel,
  onCreated,
}: {
  onCancel: () => void
  onCreated: (workspaceID: string) => Promise<void>
}) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [domain, setDomain] = useState('')
  const [emailDomain, setEmailDomain] = useState('')
  const [description, setDescription] = useState('')
  const [discoverability, setDiscoverability] =
    useState<WorkspaceDiscoverability>('invite_only')
  const [error, setError] = useState('')
  const createWorkspace = useCreateWorkspace()

  async function handleCreate() {
    const trimmedName = name.trim()
    const trimmedDomain = domain.trim()
    const trimmedEmailDomain = emailDomain.trim()
    const trimmedDescription = description.trim()

    if (!trimmedName) {
      setError('Workspace name is required.')
      return
    }
    if (!trimmedDomain) {
      setError('Workspace domain is required.')
      return
    }

    setError('')

    try {
      const created = (await createWorkspace.mutateAsync({
        data: {
          name: trimmedName,
          domain: trimmedDomain,
          email_domain: trimmedEmailDomain,
          description: trimmedDescription,
          icon: {},
          discoverability,
          default_channels: [],
          preferences: {},
          profile_fields: [],
          billing: {
            plan: 'free',
            status: 'active',
          },
        },
      })) as unknown as Workspace

      await queryClient.invalidateQueries({
        queryKey: getListWorkspacesQueryKey(),
      })
      await queryClient.invalidateQueries({
        queryKey: getGetWorkspaceQueryKey(created.id),
      })
      await onCreated(created.id)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to create workspace.'))
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-bold tracking-tight text-[var(--ink)]">
            Create workspace
          </h1>
          <p className="mt-1 text-sm text-[var(--ink-soft)]">
            Add a new workspace to the admin catalog.
          </p>
        </div>
        <Button variant="outline" onClick={onCancel}>
          Cancel
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <FieldLabel
          label="Workspace name"
          description="Displayed throughout the workspace."
        >
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Acme"
          />
        </FieldLabel>
        <FieldLabel
          label="Domain"
          description="Unique workspace slug used by the backend."
        >
          <Input
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="acme"
          />
        </FieldLabel>
        <FieldLabel
          label="Email domain"
          description="Optional email domain for user identities."
        >
          <Input
            value={emailDomain}
            onChange={(e) => setEmailDomain(e.target.value)}
            placeholder="acme.com"
          />
        </FieldLabel>
        <FieldLabel
          label="Discoverability"
          description="Controls whether users can find the workspace directly."
        >
          <Select
            value={discoverability}
            onChange={(e) =>
              setDiscoverability(e.target.value as WorkspaceDiscoverability)
            }
          >
            <option value="invite_only">Invite only</option>
            <option value="open">Open</option>
          </Select>
        </FieldLabel>
      </div>

      <FieldLabel
        label="Description"
        description="Optional summary shown in workspace metadata."
      >
        <Input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Internal engineering workspace"
        />
      </FieldLabel>

      {error ? <Alert variant="destructive">{error}</Alert> : null}

      <div className="flex flex-wrap gap-3">
        <Button
          onClick={() => void handleCreate()}
          disabled={createWorkspace.isPending}
        >
          {createWorkspace.isPending ? 'Creating…' : 'Create workspace'}
        </Button>
        <Button
          variant="outline"
          onClick={onCancel}
          disabled={createWorkspace.isPending}
        >
          Back
        </Button>
      </div>
    </div>
  )
}

function WorkspaceDetails({ workspace }: { workspace: Workspace }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState(workspace.name)
  const [domain, setDomain] = useState(workspace.domain || '')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const updateWorkspace = useUpdateWorkspace()

  async function save() {
    setError('')
    setSuccess(false)
    try {
      await updateWorkspace.mutateAsync({
        id: workspace.id,
        data: {
          name: name.trim() || undefined,
          domain: domain.trim() || undefined,
        },
      })
      await queryClient.invalidateQueries({
        queryKey: getGetWorkspaceQueryKey(workspace.id),
      })
      setSuccess(true)
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to update workspace settings.'))
    }
  }

  return (
    <div>
      {/* Header with editable name */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Workspace name"
            aria-label="Workspace name"
            className="h-auto border-transparent bg-transparent px-0 py-0 text-2xl font-bold tracking-tight focus-visible:outline-none"
          />
          <p className="mt-1 text-sm text-[var(--ink-soft)]">
            Workspace settings and configuration.
          </p>
        </div>
        <Badge variant="muted">{workspace.discoverability}</Badge>
      </div>

      {/* Metadata row */}
      <div className="mt-5 grid grid-cols-2 gap-px border border-[var(--line)] bg-[var(--line)] sm:grid-cols-4">
        <MetaCell label="ID" value={workspace.id} mono />
        <MetaCell label="Created" value={formatDate(workspace.created_at)} />
        <MetaCell label="Updated" value={formatDate(workspace.updated_at)} />
        <MetaCell
          label="Default channels"
          value={
            workspace.default_channels.length
              ? workspace.default_channels.join(', ')
              : 'None'
          }
        />
      </div>

      {/* Domain + Save */}
      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-end">
        <FieldLabel
          className="min-w-0 flex-1"
          label="Domain"
          description="The domain associated with this workspace."
        >
          <Input
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="workspace-domain"
          />
        </FieldLabel>
        <Button onClick={() => void save()} disabled={updateWorkspace.isPending}>
          {updateWorkspace.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>

      {error ? (
        <Alert className="mt-3">{error}</Alert>
      ) : success ? (
        <Alert className="mt-3">Workspace settings saved.</Alert>
      ) : null}
    </div>
  )
}

function WorkspaceData({
  activeTab,
  billable,
  billing,
  preferences,
  onTabChange,
}: {
  activeTab: WorkspaceDataTab
  billable?: WorkspaceBillableInfoMap
  billing?: WorkspaceBilling
  preferences?: FreeFormObject
  onTabChange: (value: WorkspaceDataTab) => void
}) {
  const availableTabs = [
    billable ? { value: 'billable', label: 'Billable', data: billable } : null,
    billing ? { value: 'billing', label: 'Billing', data: billing } : null,
    preferences
      ? { value: 'preferences', label: 'Preferences', data: preferences }
      : null,
  ].filter(Boolean) as Array<{
    value: WorkspaceDataTab
    label: string
    data: unknown
  }>

  const visibleTab = availableTabs.some((tab) => tab.value === activeTab)
    ? activeTab
    : (availableTabs[0]?.value ?? 'billable')

  return (
    <div>
      <div className="mb-3">
        <h2 className="text-sm font-bold text-[var(--ink)]">Diagnostics</h2>
        <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
          Raw billing and preference payloads for troubleshooting.
        </p>
      </div>
      <Tabs
        value={visibleTab}
        onValueChange={(value) => onTabChange(value as WorkspaceDataTab)}
      >
        <TabsList className="mb-0">
          {availableTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {availableTabs.map((tab) => (
          <TabsContent key={tab.value} value={tab.value}>
            <CodeBlock className="max-h-[400px] overflow-auto text-xs text-[var(--ink-soft)]">
              {JSON.stringify(tab.data, null, 2)}
            </CodeBlock>
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}

function ProfileFields({
  fields,
}: {
  fields: NonNullable<WorkspaceProfileFieldsCollection['items']>
}) {
  return (
    <div>
      <div className="mb-3">
        <h2 className="text-sm font-bold text-[var(--ink)]">Profile fields</h2>
        <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
          Custom profile metadata configured for this workspace.
        </p>
      </div>
      <div className="overflow-x-auto border border-[var(--line)]">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Label</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Hint</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {fields.map((field, i) => (
              <TableRow key={i}>
                <TableCell className="font-medium">{field.label}</TableCell>
                <TableCell className="text-[var(--ink-soft)]">
                  {field.type}
                </TableCell>
                <TableCell className="text-[var(--ink-soft)]">
                  {field.hint || '—'}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function TransferPrimaryAdmin({ workspaceID }: { workspaceID: string }) {
  const queryClient = useQueryClient()
  const [targetUserID, setTargetUserID] = useState('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const transfer = useTransferPrimaryAdmin()

  async function handleTransfer() {
    if (!targetUserID.trim()) return
    setError('')
    setSuccess(false)
    try {
      await transfer.mutateAsync({
        id: workspaceID,
        data: { user_id: targetUserID.trim() },
      })
      setSuccess(true)
      setTargetUserID('')
      await queryClient.invalidateQueries({
        queryKey: getGetWorkspaceQueryKey(workspaceID),
      })
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to transfer primary admin.'))
    }
  }

  return (
    <div className="border-t border-[var(--line)] pt-6">
      <div className="mb-3">
        <h2 className="text-sm font-bold text-[#dc2626]">
          Transfer primary admin
        </h2>
        <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
          Reassign the primary admin role to a different user. This cannot be
          undone from this screen.
        </p>
      </div>

      {error ? (
        <Alert variant="destructive" className="mb-3">
          {error}
        </Alert>
      ) : null}
      {success ? (
        <Alert className="mb-3">Primary admin transferred successfully.</Alert>
      ) : null}

      <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
        <FieldLabel className="min-w-0 flex-1" label="Target user ID">
          <Input
            value={targetUserID}
            onChange={(e) => setTargetUserID(e.target.value)}
            placeholder="U12345678"
          />
        </FieldLabel>
        <Button
          variant="destructive"
          onClick={() => void handleTransfer()}
          disabled={transfer.isPending || !targetUserID.trim()}
        >
          {transfer.isPending ? 'Transferring…' : 'Transfer'}
        </Button>
      </div>
    </div>
  )
}

function MetaCell({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="bg-[var(--surface-strong)] px-4 py-3">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
        {label}
      </div>
      <div
        className={`mt-0.5 truncate text-sm text-[var(--ink)] ${mono ? 'font-mono text-xs' : ''}`}
      >
        {value}
      </div>
    </div>
  )
}

function FieldLabel({
  label,
  description,
  className,
  children,
}: {
  label: string
  description?: string
  className?: string
  children: ComponentProps<typeof Input>['children'] | React.ReactNode
}) {
  return (
    <label className={className}>
      <span className="mb-0.5 block text-[11px] font-semibold uppercase tracking-wide text-[var(--ink-soft)]">
        {label}
      </span>
      {description ? (
        <span className="mb-1.5 block text-xs text-[var(--ink-soft)]">
          {description}
        </span>
      ) : null}
      {children}
    </label>
  )
}
