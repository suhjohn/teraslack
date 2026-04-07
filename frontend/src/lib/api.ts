export type APIError = {
  code: string
  message: string
  request_id?: string
}

export type CollectionResponse<T> = {
  items: T[]
  next_cursor?: string
}

export type AuthContext = {
  workspace_id: string
  account_id?: string
  user_id?: string
  principal_type: 'human' | 'agent' | 'system' | string
  account_type?: 'primary_admin' | 'admin' | 'member' | ''
  is_bot: boolean
  permissions?: string[]
  scopes?: string[]
  account?: Account
  user?: User
}

export type Account = {
  id: string
  email: string
  principal_type: string
  is_bot: boolean
  deleted: boolean
  created_at: string
  updated_at: string
}

export type Workspace = {
  id: string
  name: string
  domain: string
  email_domain: string
  description: string
  discoverability: string
  created_at: string
  updated_at: string
}

export type User = {
  id: string
  account_id?: string
  workspace_id: string
  name: string
  real_name: string
  display_name: string
  email: string
  principal_type: string
  account_type?: 'primary_admin' | 'admin' | 'member' | ''
  is_bot: boolean
  deleted: boolean
  created_at: string
  updated_at: string
  profile: {
    title: string
    image_192: string
    image_48: string
    status_text: string
  }
}

export type UserRolesResponse = {
  user_id: string
  delegated_roles: string[]
}

export const delegatedRoles = [
  'channels_admin',
  'roles_admin',
  'security_admin',
  'integrations_admin',
  'support_readonly',
] as const

export type DelegatedRole = (typeof delegatedRoles)[number]

export class APIClientError extends Error {
  status: number
  code: string
  requestId?: string

  constructor(status: number, error: APIError) {
    super(error.message)
    this.name = 'APIClientError'
    this.status = status
    this.code = error.code
    this.requestId = error.request_id
  }
}

export const apiBaseURL =
  import.meta.env.VITE_API_BASE_URL?.replace(/\/$/, '') ?? 'http://localhost:8080'

export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(`${apiBaseURL}${path}`, {
    ...init,
    headers,
    credentials: 'include',
  })

  if (!response.ok) {
    let errorBody: APIError = {
      code: 'request_failed',
      message: `Request failed with status ${response.status}.`,
    }

    try {
      errorBody = (await response.json()) as APIError
    } catch {
      // Ignore malformed error payloads and fall back to a generic message.
    }

    throw new APIClientError(response.status, errorBody)
  }

  if (response.status === 204) {
    return undefined as T
  }

  return (await response.json()) as T
}

export async function startOAuth(provider: 'github' | 'google', redirectPath = '/workspace') {
  if (typeof window === 'undefined') {
    return
  }

  const redirectTo = new URL(redirectPath, window.location.origin)
  const params = new URLSearchParams(window.location.search)
  const workspaceID = params.get('workspace_id')?.trim()
  const inviteToken = params.get('invite')?.trim()
  if (workspaceID) {
    redirectTo.searchParams.set('workspace_id', workspaceID)
  }
  if (inviteToken) {
    redirectTo.searchParams.set('invite', inviteToken)
  }

  try {
    const response = await apiFetch<{ auth_url: string }>(
      `/auth/oauth/${provider}/start`,
      {
        method: 'POST',
        body: JSON.stringify({
          redirect_uri: redirectTo.toString(),
        }),
      },
    )
    window.location.assign(response.auth_url)
  } catch (error) {
    console.error(`Failed to start ${provider} OAuth`, error)
  }
}

export function getProviderLabel(provider: 'github' | 'google') {
  return provider === 'github' ? 'Continue with GitHub' : 'Continue with Google'
}
