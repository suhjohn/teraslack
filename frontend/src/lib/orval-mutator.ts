import { APIClientError, apiBaseURL } from './api'
import type { APIError } from './api'

export type ErrorType<TError> = TError extends never
  ? APIClientError
  : APIClientError
export type BodyType<TBody> = TBody

function buildUrl(path: string) {
  return new URL(path, apiBaseURL).toString()
}

async function parseError(response: Response) {
  let errorBody: APIError = {
    code: 'request_failed',
    message: `Request failed with status ${response.status}.`,
  }

  try {
    errorBody = (await response.json()) as APIError
  } catch {
    // Ignore malformed error payloads and fall back to a generic message.
  }

  return new APIClientError(response.status, errorBody)
}

export async function orvalFetch<T>(
  url: string,
  config: RequestInit,
): Promise<T> {
  const headers = new Headers(config.headers)
  const response = await fetch(buildUrl(url), {
    ...config,
    headers,
    credentials: 'include',
  })

  if (!response.ok) {
    throw await parseError(response)
  }

  if (response.status === 204) {
    return undefined as T
  }

  const contentType = response.headers.get('Content-Type') ?? ''
  if (!contentType.includes('application/json')) {
    return (await response.text()) as T
  }

  return (await response.json()) as T
}
