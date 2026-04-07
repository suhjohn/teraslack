import { useQueryClient } from '@tanstack/react-query'
import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { LoaderCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Card, CardDescription, CardHeader, CardTitle } from '../../../components/ui/card'
import { APIClientError } from '../../../lib/api'
import { getErrorMessage } from '../../../lib/admin'
import { useJoinConversation } from '../../../lib/openapi'
import type { Conversation } from '../../../lib/openapi'

export const Route = createFileRoute('/join/conversations/$token')({
  component: JoinConversationRoute,
})

function JoinConversationRoute() {
  const { token } = Route.useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const joinConversationMutation = useJoinConversation()

  const [error, setError] = useState('')
  const [isUnauthorized, setIsUnauthorized] = useState(false)

  useEffect(() => {
    let cancelled = false

    async function joinConversation() {
      try {
        const conversation = unwrapData<Conversation>(
          await joinConversationMutation.mutateAsync({ data: { token } }),
        )

        await queryClient.invalidateQueries({ queryKey: ['/conversations'] })

        if (cancelled) {
          return
        }

        await navigate({
          to: '/workspaces/me/channels/$conversationId',
          params: { conversationId: conversation.id },
          replace: true,
        })
      } catch (joinError) {
        if (cancelled) {
          return
        }

        if (joinError instanceof APIClientError && joinError.status === 401) {
          setIsUnauthorized(true)
          setError('Sign in to join this conversation, then open the link again.')
          return
        }

        setError(getErrorMessage(joinError, 'Failed to join conversation.'))
      }
    }

    void joinConversation()

    return () => {
      cancelled = true
    }
  }, [joinConversationMutation, navigate, queryClient, token])

  if (!error) {
    return (
      <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
        <div className="mx-auto flex min-h-dvh w-full max-w-[960px] items-center justify-center px-4 py-12">
          <Card className="flex min-h-[32vh] w-full items-center justify-center rounded-[2rem]">
            <span className="inline-flex items-center gap-3 text-[var(--sys-home-muted)]">
              <LoaderCircle className="h-5 w-5 animate-spin" />
              Joining conversation…
            </span>
          </Card>
        </div>
      </main>
    )
  }

  return (
    <main className="admin-shell min-h-dvh bg-[var(--sys-home-bg)]">
      <div className="mx-auto w-full max-w-[960px] px-4 py-12">
        <Card className="rounded-[2rem] p-8">
          <CardHeader>
            <CardTitle>
              {isUnauthorized ? 'Authentication required' : 'Link unavailable'}
            </CardTitle>
            <CardDescription>{error}</CardDescription>
          </CardHeader>
          <div className="mt-6 flex gap-3">
            <Link to="/" className="sys-command-button no-underline">
              {isUnauthorized ? 'Go to login' : 'Back home'}
            </Link>
            {!isUnauthorized ? (
              <Link to="/workspaces/me" className="sys-outline-link no-underline">
                Open inbox
              </Link>
            ) : null}
          </div>
        </Card>
      </div>
    </main>
  )
}

function unwrapData<T>(value: { data: T } | T) {
  if (value && typeof value === 'object' && 'data' in value) {
    return value.data
  }

  return value
}
