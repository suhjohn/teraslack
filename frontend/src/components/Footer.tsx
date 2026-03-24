export default function Footer() {
  const year = new Date().getFullYear()

  return (
    <footer className="mt-20 border-t border-[var(--line)] px-4 pb-10 pt-8">
      <div className="page-wrap text-xs text-[var(--ink-soft)]">
        <p className="m-0">&copy; {year} Teraslack</p>
      </div>
    </footer>
  )
}
