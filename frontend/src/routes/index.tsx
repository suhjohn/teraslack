import { createFileRoute } from '@tanstack/react-router'
import { ArrowRight, Building2, ShieldCheck, Workflow } from 'lucide-react'

export const Route = createFileRoute('/')({ component: App })

function App() {
  return (
    <main className="page-wrap px-4 pb-12 pt-12">
      <section className="hero-panel rise-in relative overflow-hidden rounded-[2rem] px-6 py-10 sm:px-10 sm:py-14">
        <div className="pointer-events-none absolute -left-20 -top-24 h-56 w-56 rounded-full bg-[radial-gradient(circle,rgba(209,78,45,0.28),transparent_66%)]" />
        <div className="pointer-events-none absolute -bottom-20 -right-20 h-56 w-56 rounded-full bg-[radial-gradient(circle,rgba(13,28,56,0.16),transparent_66%)]" />
        <p className="eyebrow mb-3">Teraslack</p>
        <h1 className="display-title mb-5 max-w-4xl text-4xl leading-[0.94] font-semibold tracking-tight text-[var(--ink)] sm:text-7xl">
          Slack-style infrastructure with explicit access control and a real admin surface.
        </h1>
        <p className="mb-8 max-w-2xl text-base leading-7 text-[var(--ink-soft)] sm:text-lg">
          The API stays on <code>api.teraslack.ai</code>. This frontend on
          TanStack Start handles the landing experience, login handoff, and the
          first admin workflows for teams, users, and delegated roles.
        </p>
        <div className="flex flex-wrap gap-3">
          <a
            href="/login"
            className="action-button no-underline"
          >
            Login to admin
            <ArrowRight className="h-4 w-4" />
          </a>
          <a
            href="/admin"
            className="secondary-button no-underline"
          >
            Open admin console
          </a>
        </div>
      </section>

      <section className="mt-8 grid gap-4 lg:grid-cols-3">
        {[
          {
            icon: <Building2 className="h-5 w-5" />,
            title: 'Workspace controls',
            desc: 'Inspect teams, domains, and workspace configuration from a single operational surface.',
          },
          {
            icon: <ShieldCheck className="h-5 w-5" />,
            title: 'Access management',
            desc: 'Promote admins, edit delegated roles, and keep access changes visible instead of implicit.',
          },
          {
            icon: <Workflow className="h-5 w-5" />,
            title: 'Frontend + API split',
            desc: 'A dedicated TanStack Start frontend on teraslack.ai and the existing API on api.teraslack.ai.',
          },
        ].map((item, index) => (
          <article
            key={item.title}
            className="admin-card rise-in rounded-2xl p-5"
            style={{ animationDelay: `${index * 90 + 80}ms` }}
          >
            <div className="mb-4 inline-flex h-11 w-11 items-center justify-center rounded-2xl bg-[rgba(209,78,45,0.12)] text-[var(--accent)]">
              {item.icon}
            </div>
            <h2 className="mb-2 text-base font-semibold text-[var(--ink)]">
              {item.title}
            </h2>
            <p className="m-0 text-sm leading-6 text-[var(--ink-soft)]">
              {item.desc}
            </p>
          </article>
        ))}
      </section>

      <section className="admin-card mt-8 rounded-2xl p-6">
        <p className="eyebrow mb-2">Initial Surface</p>
        <ul className="m-0 list-disc space-y-2 pl-5 text-sm leading-6 text-[var(--ink-soft)]">
          <li>
            OAuth handoff through the API with redirect back to the frontend.
          </li>
          <li>
            Session-aware admin dashboard backed by <code>/auth/me</code>,{' '}
            <code>/teams</code>, <code>/users</code>, and role endpoints.
          </li>
          <li>
            Backend CORS and secure cookie handling for cross-origin frontend
            requests on Railway.
          </li>
        </ul>
      </section>
    </main>
  )
}
