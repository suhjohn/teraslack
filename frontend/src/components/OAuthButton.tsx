import { getProviderLabel, startOAuth } from '#/lib/api'

export type OAuthProvider = 'github' | 'google'

type OAuthButtonProps = {
  provider: OAuthProvider
  className?: string
  onClick?: () => void
}

function GoogleIcon () {
  return (
    <svg viewBox='0 0 18 18' aria-hidden='true' focusable='false'>
      <path
        d='M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.9c1.7-1.56 2.7-3.86 2.7-6.62Z'
        fill='#4285F4'
      />
      <path
        d='M9 18c2.43 0 4.47-.8 5.96-2.18l-2.9-2.26c-.8.54-1.82.86-3.06.86-2.35 0-4.34-1.58-5.06-3.7H.96v2.34A9 9 0 0 0 9 18Z'
        fill='#34A853'
      />
      <path
        d='M3.94 10.72A5.41 5.41 0 0 1 3.66 9c0-.6.1-1.18.28-1.72V4.94H.96A9 9 0 0 0 0 9c0 1.45.35 2.82.96 4.06l2.98-2.34Z'
        fill='#FBBC05'
      />
      <path
        d='M9 3.58c1.32 0 2.5.46 3.44 1.36l2.58-2.58C13.46.9 11.43 0 9 0A9 9 0 0 0 .96 4.94l2.98 2.34c.72-2.12 2.7-3.7 5.06-3.7Z'
        fill='#EA4335'
      />
    </svg>
  )
}

function GitHubIcon () {
  return (
    <svg viewBox='0 0 16 16' aria-hidden='true' focusable='false'>
      <path
        fill='currentColor'
        d='M8 0C3.58 0 0 3.67 0 8.2c0 3.63 2.29 6.7 5.47 7.8.4.08.55-.18.55-.4 0-.2 0-.72-.01-1.42-2.23.5-2.7-.98-2.7-.98-.36-.95-.9-1.2-.9-1.2-.73-.51.06-.5.06-.5.81.06 1.24.86 1.24.86.72 1.27 1.88.9 2.34.69.07-.54.28-.9.5-1.11-1.78-.21-3.64-.92-3.64-4.07 0-.9.31-1.63.82-2.2-.08-.2-.36-1.03.08-2.14 0 0 .67-.22 2.2.84a7.33 7.33 0 0 1 4 0c1.53-1.06 2.2-.84 2.2-.84.44 1.11.16 1.94.08 2.14.5.57.82 1.3.82 2.2 0 3.16-1.87 3.86-3.66 4.07.29.25.54.74.54 1.5 0 1.08-.01 1.95-.01 2.22 0 .22.14.49.55.4A8.23 8.23 0 0 0 16 8.2C16 3.67 12.42 0 8 0Z'
      />
    </svg>
  )
}

export default function OAuthButton ({
  provider,
  className,
  onClick
}: OAuthButtonProps) {
  const label = getProviderLabel(provider)
  const classes = className === undefined
    ? `oauth-button oauth-button--${provider}`
    : `oauth-button oauth-button--${provider} ${className}`

  return (
    <button
      type='button'
      onClick={() => {
        onClick?.()
        void startOAuth(provider)
      }}
      className={classes}
      aria-label={label}
    >
      <span className='oauth-button__icon'>
        {provider === 'google' ? <GoogleIcon /> : <GitHubIcon />}
      </span>
      <span>{label}</span>
    </button>
  )
}
