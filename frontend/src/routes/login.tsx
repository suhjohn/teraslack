import { createFileRoute } from '@tanstack/react-router'
import { LockKeyhole, ShieldCheck } from 'lucide-react'
import { getProviderLabel, startOAuth } from '../lib/api'

export const Route = createFileRoute('/login')({
  component: LoginRoute,
})

function LoginRoute() {
  return (
    <main className="page-wrap px-4 py-12">
      <section className="hero-panel rounded-[2rem] px-6 py-10 sm:px-10 sm:py-14">
        <div className="grid gap-8 lg:grid-cols-[1.1fr_0.9fr]">
          <div className="space-y-5">
            <p className="eyebrow">Sign In</p>
            <h1 className="display-title max-w-2xl text-4xl leading-[0.95] font-semibold sm:text-6xl">
              Login flows through the Teraslack API, then drops you into admin.
            </h1>
            <p className="max-w-2xl text-base leading-7 text-[var(--ink-soft)] sm:text-lg">
              OAuth is handled on <code>api.teraslack.ai</code>. After the
              callback completes, you come back here with an authenticated
              session already established for the API.
            </p>
            <div className="flex flex-wrap gap-3">
              <button
                type="button"
                className="action-button"
                onClick={() => startOAuth('github')}
              >
                {getProviderLabel('github')}
              </button>
              <button
                type="button"
                className="secondary-button"
                onClick={() => startOAuth('google')}
              >
                {getProviderLabel('google')}
              </button>
            </div>
          </div>

          <aside className="admin-card rounded-[1.5rem] p-6">
            <div className="space-y-5">
              <div className="inline-flex h-12 w-12 items-center justify-center rounded-2xl bg-[rgba(209,78,45,0.12)] text-[var(--accent)]">
                <ShieldCheck className="h-6 w-6" />
              </div>
              <div>
                <h2 className="text-lg font-semibold text-[var(--ink)]">
                  What happens next
                </h2>
                <p className="mt-2 text-sm leading-6 text-[var(--ink-soft)]">
                  The API sets the session cookie and redirects back to
                  <code>/admin</code>. The frontend then loads your team,
                  user directory, and admin controls over authenticated API
                  requests.
                </p>
              </div>
              <div className="space-y-3 text-sm text-[var(--ink-soft)]">
                <div className="info-row">
                  <LockKeyhole className="h-4 w-4" />
                  Cookies stay on the API origin and requests use credentials.
                </div>
                <div className="info-row">
                  <ShieldCheck className="h-4 w-4" />
                  Admin controls only appear once <code>/auth/me</code>
                  confirms your session.
                </div>
              </div>
            </div>
          </aside>
        </div>
      </section>
    </main>
  )
}
