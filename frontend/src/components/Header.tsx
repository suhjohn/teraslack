import { Link } from '@tanstack/react-router'
import ThemeToggle from './ThemeToggle'
import { startOAuth } from '#/lib/api'

export default function Header () {
  return (
    <header className='ws-row'>
      <div className='ws-cell ws-header-cell'>
        <nav className='flex w-full justify-between items-center gap-8 text-[0.75rem]'>
          <div className='flex items-center gap-8'>
            <Link
              to='/'
              className='meta-title sys-link inline-flex items-center gap-2 text-[15px] font-bold tracking-[-0.02em]'
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
            <Link to='/docs' className='sys-link'>
              DOCS
            </Link>
          </div>
          <div className='flex items-center gap-4'>
            <button
              type='button'
              className='sys-command-button'
              onClick={() => startOAuth('github')}
            >
              LOGIN WITH GITHUB
            </button>
            <button
              type='button'
              className='sys-command-button'
              onClick={() => startOAuth('google')}
            >
              LOGIN WITH GOOGLE
            </button>
            <ThemeToggle />
          </div>
        </nav>
      </div>
    </header>
  )
}
