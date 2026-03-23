import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/about')({
  component: About,
})

function About() {
  return (
    <main className="page-wrap px-4 py-12">
      <section className="admin-card rounded-2xl p-6 sm:p-8">
        <p className="eyebrow mb-2">Platform</p>
        <h1 className="display-title mb-3 text-4xl font-semibold text-[var(--ink)] sm:text-5xl">
          Teraslack now has a real frontend boundary.
        </h1>
        <p className="m-0 max-w-3xl text-base leading-8 text-[var(--ink-soft)]">
          The backend remains the system of record and serves the authenticated
          API on <code>api.teraslack.ai</code>. The TanStack Start app becomes
          the browser-facing layer for product pages, sign-in, and operational
          admin tooling on <code>teraslack.ai</code>.
        </p>
      </section>
    </main>
  )
}
