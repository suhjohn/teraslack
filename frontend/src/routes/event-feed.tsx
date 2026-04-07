import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { LoaderCircle, LogOut, Settings } from 'lucide-react'
import { useState } from 'react'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Eyebrow } from '../components/ui/eyebrow'
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card'
import { APIClientError } from '../lib/api'
import { formatDate, getErrorMessage } from '../lib/admin'
import {
  getGetAuthMeQueryKey,
  getListWorkspacesQueryKey,
  useDeleteCurrentSession,
  useGetAuthMe,
  useListEvents,
} from '../lib/openapi'
import type {
  AuthMeResponse,
  ExternalEvent,
  ExternalEventsCollection,
} from '../lib/openapi'

export const Route = createFileRoute('/event-feed')({
  component: EventsPage,
})

function EventsPage() {
  const queryClient = useQueryClient()
  const [signedOut, setSignedOut] = useState(false)

  const authQuery = useGetAuthMe<AuthMeResponse>({
    query: { retry: false, staleTime: 30_000 },
  })

  const isUnauthorized =
    signedOut ||
    (authQuery.error instanceof APIClientError &&
      authQuery.error.status === 401)

  const eventsQuery = useListEvents<ExternalEventsCollection>(
    { limit: 100 },
    {
      query: {
        enabled: authQuery.isSuccess && !signedOut,
        retry: false,
        staleTime: 10_000,
      },
    },
  )

  const events = eventsQuery.data?.items ?? []
  const deleteSession = useDeleteCurrentSession()

  async function logout() {
    try {
      await deleteSession.mutateAsync()
      setSignedOut(true)
      queryClient.removeQueries({ queryKey: getGetAuthMeQueryKey() })
      queryClient.removeQueries({ queryKey: getListWorkspacesQueryKey() })
    } catch {
      // Handled by query state.
    }
  }

  if (authQuery.status === 'pending') {
    return (
      <main className="admin-shell flex min-h-dvh items-center justify-center">
        <span className="inline-flex items-center gap-3 text-[var(--sys-home-muted)]">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          Loading session…
        </span>
      </main>
    )
  }

  if (isUnauthorized) {
    return (
      <main className="admin-shell mx-auto w-[min(1800px,calc(100%-2rem))] py-12">
        <Card className="p-8">
          <CardHeader>
            <Eyebrow>Events</Eyebrow>
            <CardTitle className="text-4xl">Authentication required</CardTitle>
            <CardDescription className="max-w-2xl text-base leading-7">
              {signedOut
                ? 'The current session has been revoked.'
                : 'Sign in to view events.'}
            </CardDescription>
          </CardHeader>
          <div className="mt-6 flex gap-3">
            <Link to="/" className="sys-command-button no-underline">
              Go to login
            </Link>
          </div>
        </Card>
      </main>
    )
  }

  return (
    <main className="admin-shell min-h-dvh">
      <div className="mx-auto flex min-h-dvh w-full max-w-[1560px]">
        {/* Sidebar */}
        <aside className="hidden w-[240px] shrink-0 border-r border-[var(--sys-home-border)] lg:block">
          <div className="flex min-h-dvh flex-col gap-6 px-3 py-4">
            <div className="px-2 text-[12px] font-bold uppercase tracking-[0.04em]">
              Teraslack
            </div>

            <div className="space-y-2 px-1.5">
              <div className="px-0.5 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                Navigation
              </div>
              <nav className="flex flex-col gap-1">
                <span className="inline-flex items-center gap-2.5 border border-[var(--sys-home-border)] bg-[var(--sys-home-accent-bg)] px-2 py-2 text-[12px] font-bold text-[var(--sys-home-accent-fg)]">
                  Events
                </span>
                <Link
                  to="/settings/api-keys"
                  className="inline-flex items-center gap-2.5 border border-[var(--sys-home-border)] px-2 py-2 text-[12px] text-[var(--sys-home-muted)] no-underline transition hover:bg-[var(--sys-home-accent-bg)] hover:text-[var(--sys-home-accent-fg)]"
                >
                  <Settings className="h-4 w-4" />
                  Settings
                </Link>
              </nav>
            </div>

            <div className="mt-auto px-1.5 pb-0">
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-center"
                onClick={() => void logout()}
              >
                <LogOut className="h-3.5 w-3.5" />
                Sign out
              </Button>
            </div>
          </div>
        </aside>

        {/* Content */}
        <section className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-[1320px] px-4 py-5 md:px-6 md:py-6 xl:px-8">
            <div className="mb-5 flex items-center justify-between">
              <div>
                <Eyebrow>Event feed</Eyebrow>
                <h1 className="mt-1 text-lg font-bold uppercase tracking-[0.04em]">
                  Events
                </h1>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void eventsQuery.refetch()}
                disabled={eventsQuery.isFetching}
              >
                {eventsQuery.isFetching ? (
                  <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                ) : null}
                Refresh
              </Button>
            </div>

            {eventsQuery.error ? (
              <Alert variant="destructive">
                {getErrorMessage(eventsQuery.error, 'Failed to load events.')}
              </Alert>
            ) : eventsQuery.isPending ? (
              <div className="flex min-h-[40vh] items-center justify-center text-[var(--sys-home-muted)]">
                <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
                Loading events…
              </div>
            ) : !events.length ? (
              <div className="border border-dashed border-[var(--sys-home-border)] px-6 py-10 text-center text-[var(--sys-home-muted)]">
                No events yet.
              </div>
            ) : (
              <div className="border border-[var(--sys-home-border)]">
                <table className="w-full text-left text-[12px]">
                  <thead>
                    <tr className="border-b border-[var(--sys-home-border)]">
                      <th className="px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                        ID
                      </th>
                      <th className="px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                        Type
                      </th>
                      <th className="px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                        Resource
                      </th>
                      <th className="hidden px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)] md:table-cell">
                        Workspace
                      </th>
                      <th className="px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-[var(--sys-home-muted)]">
                        Time
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {events.map((event) => (
                      <EventRow key={event.id} event={event} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </section>
      </div>
    </main>
  )
}

function EventRow({ event }: { event: ExternalEvent }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <>
      <tr
        className="cursor-pointer border-b border-[var(--sys-home-border)] transition hover:bg-[var(--sys-home-accent-bg)] hover:text-[var(--sys-home-accent-fg)]"
        onClick={() => setExpanded(!expanded)}
      >
        <td className="px-3 py-2 font-[family-name:var(--font-mono)]">
          {event.id}
        </td>
        <td className="px-3 py-2">
          <Badge variant="muted">{event.type}</Badge>
        </td>
        <td className="px-3 py-2">
          <span className="text-[var(--sys-home-muted)]">
            {event.resource_type}:
          </span>{' '}
          <span className="font-[family-name:var(--font-mono)]">
            {event.resource_id.length > 16
              ? `${event.resource_id.slice(0, 14)}…`
              : event.resource_id}
          </span>
        </td>
        <td className="hidden px-3 py-2 text-[var(--sys-home-muted)] md:table-cell">
          {event.workspace_id ?? '—'}
        </td>
        <td className="px-3 py-2 text-[var(--sys-home-muted)]">
          {formatDate(event.occurred_at)}
        </td>
      </tr>
      {expanded ? (
        <tr className="border-b border-[var(--sys-home-border)]">
          <td colSpan={5} className="px-3 py-3">
            <pre className="overflow-x-auto whitespace-pre-wrap border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-3 font-[family-name:var(--font-mono)] text-[11px] leading-[1.6] text-[var(--sys-home-fg)]">
              {JSON.stringify(event.payload, null, 2)}
            </pre>
          </td>
        </tr>
      ) : null}
    </>
  )
}
