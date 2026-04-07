const MODAL_SERVER_URL =
  process.env.MODAL_EMBEDDING_SERVER_URL?.trim() ??
  process.env.MODAL_SERVER_URL?.trim() ??
  ''
const MODAL_SERVER_API_KEY = process.env.MODAL_SERVER_API_KEY?.trim() ?? ''

type QueryEmbeddingResponse = {
  embedding: number[]
  model: string
}

type DocumentEmbeddingsResponse = {
  embeddings: number[][]
  model: string
}

const EMBEDDING_REQUEST_TIMEOUT_MS = 30_000
const EMBEDDING_REQUEST_MAX_ATTEMPTS = 3

export function isEmbeddingServiceConfigured (): boolean {
  return MODAL_SERVER_URL.length > 0 && MODAL_SERVER_API_KEY.length > 0
}

function assertEmbeddingServiceConfigured (): void {
  if (MODAL_SERVER_URL.length === 0) {
    throw new Error(
      'MODAL_SERVER_URL is required to call the Modal embedding service.'
    )
  }

  if (MODAL_SERVER_API_KEY.length === 0) {
    throw new Error(
      'MODAL_SERVER_API_KEY is required to call the Modal embedding service.'
    )
  }
}

function resolveEndpoint (path: string): string {
  const trimmedBaseUrl = MODAL_SERVER_URL.replace(/\/+$/, '')
  const trimmedPath = path.replace(/^\/+/, '')
  return `${trimmedBaseUrl}/${trimmedPath}`
}

function sleep (ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

function isRetriableEmbeddingError (
  status: number | null,
  message: string
): boolean {
  if (status != null && status >= 500) {
    return true
  }

  return /terminated by signal|internal error|timeout|timed out|econnreset|socket hang up|fetch failed/i.test(
    message
  )
}

async function postEmbeddingRequest<TResponse> (
  path: string,
  body: unknown
): Promise<TResponse> {
  assertEmbeddingServiceConfigured()

  let lastError: Error | null = null

  for (
    let attempt = 1;
    attempt <= EMBEDDING_REQUEST_MAX_ATTEMPTS;
    attempt += 1
  ) {
    try {
      const response = await fetch(resolveEndpoint(path), {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${MODAL_SERVER_API_KEY}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(EMBEDDING_REQUEST_TIMEOUT_MS)
      })

      if (!response.ok) {
        const message = await response.text()
        const error = new Error(
          `Modal embedding request to '${path}' failed (${response.status}): ${message}`
        )

        if (
          attempt < EMBEDDING_REQUEST_MAX_ATTEMPTS &&
          isRetriableEmbeddingError(response.status, message)
        ) {
          console.warn(
            `[embeddings] retrying ${path} after attempt ${attempt}/${EMBEDDING_REQUEST_MAX_ATTEMPTS}: ${message}`
          )
          await sleep(250 * attempt)
          continue
        }

        throw error
      }

      return (await response.json()) as TResponse
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      lastError = error instanceof Error ? error : new Error(message)

      if (
        attempt < EMBEDDING_REQUEST_MAX_ATTEMPTS &&
        isRetriableEmbeddingError(null, message)
      ) {
        console.warn(
          `[embeddings] retrying ${path} after attempt ${attempt}/${EMBEDDING_REQUEST_MAX_ATTEMPTS}: ${message}`
        )
        await sleep(250 * attempt)
        continue
      }

      throw lastError
    }
  }

  throw lastError ?? new Error(`Embedding request to '${path}' failed.`)
}

export async function embedQuery (text: string): Promise<number[]> {
  const normalized = text.trim()

  if (normalized.length === 0) {
    throw new Error('Cannot embed an empty query.')
  }

  const response = await postEmbeddingRequest<QueryEmbeddingResponse>(
    '/embed/query',
    { text: normalized }
  )

  return response.embedding
}

export async function embedDocuments (texts: string[]): Promise<number[][]> {
  const normalized = texts
    .map(text => text.trim())
    .filter(text => text.length > 0)

  if (normalized.length === 0) {
    return []
  }

  const response = await postEmbeddingRequest<DocumentEmbeddingsResponse>(
    '/embed/documents',
    { texts: normalized }
  )

  if (response.embeddings.length !== normalized.length) {
    throw new Error(
      `Modal embedding service returned ${response.embeddings.length} embeddings for ${normalized.length} inputs.`
    )
  }

  return response.embeddings
}
