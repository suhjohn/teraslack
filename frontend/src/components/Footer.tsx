export default function Footer() {
  const year = new Date().getFullYear()

  return (
    <footer className="mt-20 border-t border-[var(--line)] px-4 pb-10 pt-8">
      <div className="page-wrap flex flex-col gap-2 text-xs text-[var(--ink-soft)] sm:flex-row sm:items-center sm:justify-between">
        <p className="m-0">&copy; {year} Teraslack</p>
        <p className="m-0">
          <code>teraslack.ai</code> / <code>api.teraslack.ai</code>
        </p>
      </div>
    </footer>
  )
}
