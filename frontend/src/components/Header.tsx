import { Link } from '@tanstack/react-router'
import { Menu, X } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import OAuthButton from './OAuthButton'
import ThemeToggle from './ThemeToggle'

export default function Header () {
  const [menuOpen, setMenuOpen] = useState(false)

  const close = useCallback(() => setMenuOpen(false), [])

  // Close on escape
  useEffect(() => {
    if (!menuOpen) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') close() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [menuOpen, close])

  // Close on route change (resize past breakpoint)
  useEffect(() => {
    const mq = window.matchMedia('(min-width: 901px)')
    const onChange = () => { if (mq.matches) close() }
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [close])

  return (
    <header className='ws-row'>
      <div className='ws-cell ws-header-cell'>
        <nav className='header-nav'>
          <div className='header-nav__bar'>
            <Link
              to='/'
              className='meta-title sys-link header-nav__brand'
            >
              <img
                src='/favicon.svg'
                alt=''
                width={20}
                height={20}
                className='h-5 w-5 shrink-0'
                aria-hidden
              />
              TERASLACK
            </Link>

            <div className='header-nav__bar-right'>
              <Link to='/docs' className='sys-link header-nav__link'>
                DOCS
              </Link>
              <div className='header-nav__desktop-actions'>
                <OAuthButton provider='github' />
                <OAuthButton provider='google' />
              </div>
              <ThemeToggle />
              <button
                type='button'
                className='header-nav__menu-toggle'
                onClick={() => setMenuOpen(o => !o)}
                aria-label={menuOpen ? 'Close menu' : 'Open menu'}
                aria-expanded={menuOpen}
              >
                {menuOpen ? <X className='h-4 w-4' /> : <Menu className='h-4 w-4' />}
              </button>
            </div>
          </div>

          {menuOpen && (
            <div className='header-nav__mobile-menu'>
              <Link to='/docs' className='sys-command-button header-nav__mobile-button' onClick={close}>
                DOCS
              </Link>
              <OAuthButton provider='github' className='header-nav__mobile-button' onClick={close} />
              <OAuthButton provider='google' className='header-nav__mobile-button' onClick={close} />
            </div>
          )}
        </nav>
      </div>
    </header>
  )
}
