import { Link } from '@tanstack/react-router'
import GitHubLink from './GitHubLink'

export default function Footer () {
  return (
    <footer className='site-footer mt-20 px-4'>
      <div className='site-footer-inner'>
        <span className='uppercase tracking-[0.06em] text-[var(--sys-home-fg)]'>
          Optimistic Software LLC
        </span>
        <div className='flex flex-wrap gap-4'>
          <GitHubLink className='sys-link' />
          <Link to='/docs' className='sys-link'>
            Docs
          </Link>
          <Link to='/privacy' className='sys-link'>
            Privacy
          </Link>
          <Link to='/terms' className='sys-link'>
            Terms
          </Link>
        </div>
      </div>
    </footer>
  )
}
