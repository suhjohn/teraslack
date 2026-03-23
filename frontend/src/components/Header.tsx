import { Link } from '@tanstack/react-router'
import { PanelsTopLeft } from 'lucide-react'
import ThemeToggle from './ThemeToggle'

export default function Header() {
  return (
    <header className="sticky top-0 z-50 border-b border-[var(--line)] bg-[var(--header-bg)] px-4 backdrop-blur-lg">
      <nav className="page-wrap flex flex-wrap items-center gap-x-3 gap-y-2 py-3 sm:py-4">
        <h2 className="m-0 flex-shrink-0 text-base font-semibold tracking-tight">
          <Link
            to="/"
            className="inline-flex items-center gap-2 rounded-full border border-[var(--chip-line)] bg-[var(--chip-bg)] px-3 py-1.5 text-sm text-[var(--ink)] no-underline shadow-[0_8px_24px_rgba(16,32,57,0.08)] sm:px-4 sm:py-2"
          >
            <PanelsTopLeft className="h-4 w-4 text-[var(--accent)]" />
            Teraslack
          </Link>
        </h2>

        <div className="ml-auto flex items-center gap-1.5 sm:ml-0 sm:gap-2">
          <ThemeToggle />
        </div>

        <div className="order-3 flex w-full flex-wrap items-center gap-x-4 gap-y-1 pb-1 text-sm font-semibold sm:order-2 sm:w-auto sm:flex-nowrap sm:pb-0">
          <Link
            to="/"
            className="nav-link"
            activeProps={{ className: 'nav-link is-active' }}
          >
            Home
          </Link>
          <Link
            to="/about"
            className="nav-link"
            activeProps={{ className: 'nav-link is-active' }}
          >
            Platform
          </Link>
          <Link
            to="/login"
            className="nav-link"
            activeProps={{ className: 'nav-link is-active' }}
          >
            Login
          </Link>
          <Link
            to="/admin"
            className="nav-link"
            activeProps={{ className: 'nav-link is-active' }}
          >
            Admin
          </Link>
          <a
            href="https://tanstack.com/start/latest/docs/framework/react/overview"
            className="nav-link"
            target="_blank"
            rel="noreferrer"
          >
            Docs
          </a>
        </div>
      </nav>
    </header>
  )
}
