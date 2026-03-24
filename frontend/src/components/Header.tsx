import { Link } from '@tanstack/react-router'
import { Terminal } from 'lucide-react'
import ThemeToggle from './ThemeToggle'

export default function Header() {
  return (
    <header className="sticky top-0 z-50 border-b border-[var(--line)] bg-[var(--header-bg)] px-4 backdrop-blur-md">
      <nav className="page-wrap flex items-center gap-6 py-3">
        <h2 className="m-0 flex-shrink-0">
          <Link
            to="/"
            className="inline-flex items-center gap-2 border-b-0 text-sm font-bold text-[var(--ink)] no-underline"
          >
            <Terminal className="h-4 w-4" />
            teraslack
          </Link>
        </h2>

        <div className="flex items-center gap-4 text-sm font-medium">
          <Link
            to="/login"
            className="nav-link"
            activeProps={{ className: 'nav-link is-active' }}
          >
            login
          </Link>
        </div>

        <div className="ml-auto">
          <ThemeToggle />
        </div>
      </nav>
    </header>
  )
}
