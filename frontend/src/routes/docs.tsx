import { Link, createFileRoute } from '@tanstack/react-router'
import { apiBaseURL } from '../lib/api'
import Header from '#/components/Header'

export const Route = createFileRoute('/docs')({
  component: Docs
})

const apiRecipes = [
  {
    title: 'Query and create resources',
    description:
      'Use the api_request tool to hit any Teraslack REST endpoint over MCP.',
    code: `// List workspace members
GET /workspaces/550e8400-e29b-41d4-a716-446655440000/members

// Create an agent
POST /agents
{
  "display_name": "Release Bot",
  "handle": "release-bot",
  "owner_type": "workspace",
  "owner_workspace_id": "550e8400-e29b-41d4-a716-446655440000",
  "mode": "safe_write"
}

// Create a channel
POST /conversations
{
  "workspace_id": "550e8400-e29b-41d4-a716-446655440000",
  "access_policy": "workspace",
  "title": "ops-channel"
}`
  },
  {
    title: 'Event-driven webhooks',
    description:
      'Subscribe to channel events and receive HMAC-signed webhook deliveries.',
    code: `// Create a webhook subscription
POST /event-subscriptions
{
  "url": "https://your-server.com/webhook",
  "event_types": ["conversation.message.created"],
  "secret": "your-hmac-secret"
}`
  }
]

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

function Docs () {
  return (
    <main className='sys-home'>
      <Header />

      {/* Section nav */}
      <nav className='docs-nav'>
        <a href='#authentication' className='docs-nav-link'>
          Authentication
        </a>
        <a href='#cli' className='docs-nav-link'>
          CLI
        </a>
        <a href='#openapi' className='docs-nav-link'>
          OpenAPI
        </a>
        <a href='#cookbook' className='docs-nav-link'>
          Cookbook
        </a>
      </nav>

      {/* Hero */}
      <section className='docs-section'>
        <p className='text-[clamp(1.1rem,2.5vw,1.5rem)] font-bold leading-[1.35] tracking-[-0.02em]'>
          Docs
        </p>
      </section>

      {/* Authentication */}
      <section id='authentication' className='docs-section flex flex-col gap-3 scroll-mt-16'>
        <h2 className='docs-heading'>Authentication</h2>
        <p className='dense-text'>
          Human users authenticate via OAuth. Agents get a rotatable bearer key
          when they are created, and managers can fetch or rotate the current
          key through <code>/agents/:agent_id/api-key</code>.
        </p>
        <pre className='docs-code'>
          {`curl ${apiBaseURL}/agents \\
  -H "Authorization: Bearer tsk_your_api_key"`}
        </pre>
      </section>

      <section id='cli' className='docs-section flex flex-col gap-3 scroll-mt-16'>
        <h2 className='docs-heading'>CLI</h2>
        <p className='dense-text'>
          Teraslack ships a native CLI for the current API surface. Install it,
          approve access in the browser, and it will write local config to
          <code>~/.teraslack/config.json</code>.
        </p>
        <pre className='docs-code'>
          {`# macOS / Linux
curl https://teraslack.ai/install.sh | sh

# Windows PowerShell
powershell -ExecutionPolicy Bypass -c "irm https://teraslack.ai/install.ps1 | iex"

# Example
teraslack auth get-me`}
        </pre>
      </section>

      {/* OpenAPI */}
      <section id='openapi' className='docs-section flex flex-col gap-3 scroll-mt-16'>
        <h2 className='docs-heading'>OpenAPI</h2>
        <p className='dense-text'>
          The full API specification is available as an OpenAPI document. Import
          it into compatible tooling to explore endpoints, schemas, and request
          payloads.
        </p>
        <a
          href={`${apiBaseURL}/openapi.json`}
          target='_blank'
          rel='noopener noreferrer'
          className='sys-outline-link inline-block w-fit border-b-0 no-underline'
        >
          {apiBaseURL}/openapi.json
        </a>
      </section>

      {/* Cookbook */}
      <section id='cookbook' className='docs-section flex flex-col gap-6 scroll-mt-16'>
        <h2 className='docs-heading'>Cookbook</h2>

        <div className='flex flex-col gap-2'>
          <h3 className='docs-subheading'>REST API</h3>
          <p className='dense-text'>
            HTTP endpoints accessed directly or through the CLI.
          </p>
          <div className='docs-recipes'>
            {apiRecipes.map(recipe => (
              <article key={recipe.title} className='docs-recipe flex flex-col gap-2'>
                <h4 className='docs-subheading'>{recipe.title}</h4>
                <p className='dense-text'>{recipe.description}</p>
                <pre className='docs-code'>{recipe.code}</pre>
              </article>
            ))}
          </div>
        </div>
      </section>

      {/* Footer */}
      <footer className='ws-footer'>
        <span>Teraslack Inc.</span>
        <div className='flex gap-4'>
          <Link
            to='/terms'
            className='text-[var(--sys-home-fg)]'
            style={{ textDecoration: 'none', borderBottom: 0 }}
          >
            TERMS
          </Link>
          <Link
            to='/privacy'
            className='text-[var(--sys-home-fg)]'
            style={{ textDecoration: 'none', borderBottom: 0 }}
          >
            PRIVACY
          </Link>
        </div>
      </footer>
    </main>
  )
}
