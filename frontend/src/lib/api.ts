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
  team_id: string
  user_id: string
  principal_type: 'human' | 'agent' | 'system' | string
  account_type?: 'primary_admin' | 'admin' | 'member' | ''
  is_bot: boolean
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
  team_id: string
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
  'usergroups_admin',
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

export const oauthTeamID = import.meta.env.VITE_TEAM_ID?.trim() ?? ''

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

export function startOAuth(provider: 'github' | 'google', redirectPath = '/admin') {
  if (typeof window === 'undefined') {
    return
  }
  if (!oauthTeamID) {
    throw new Error('VITE_TEAM_ID is not configured.')
  }

  const redirectTo = new URL(redirectPath, window.location.origin).toString()
  const target = new URL(`/auth/oauth/${provider}/start`, apiBaseURL)
  target.searchParams.set('team_id', oauthTeamID)
  target.searchParams.set('redirect_to', redirectTo)
  window.location.assign(target.toString())
}

export function getProviderLabel(provider: 'github' | 'google') {
  return provider === 'github' ? 'Continue with GitHub' : 'Continue with Google'
}
