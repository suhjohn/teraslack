export default function Footer() {
  const year = new Date().getFullYear()

  return (
    <footer className="mt-20 border-t border-[var(--line)] px-4 pb-14 pt-10 text-[var(--ink-soft)]">
      <div className="page-wrap flex flex-col items-center justify-between gap-4 text-center sm:flex-row sm:text-left">
        <p className="m-0 text-sm">
          &copy; {year} Teraslack. API-first collaboration tooling.
        </p>
        <p className="eyebrow m-0">Built with TanStack Start</p>
      </div>
      <div className="page-wrap mt-4 text-sm text-[var(--ink-soft)]">
        Frontend on <code>teraslack.ai</code>, authenticated API on{' '}
        <code>api.teraslack.ai</code>.
      </div>
    </footer>
  )
}
