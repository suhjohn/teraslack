import { createFileRoute } from '@tanstack/react-router'
import GitHubLink from '#/components/GitHubLink'
import Header from '#/components/Header'

export const Route = createFileRoute('/privacy')({
  component: Privacy
})

function Privacy () {
  return (
    <main className='sys-home'>
      <Header />

      <section className='ws-row'>
        <div className='ws-cell' style={{ display: 'block', maxWidth: 720 }}>
          <p className='meta-title' style={{ marginBottom: '0.5rem' }}>
            PRIVACY POLICY
          </p>
          <p style={{ fontSize: '0.7rem', color: 'var(--sys-home-muted)' }}>
            Last updated: March 25, 2026
          </p>
        </div>
      </section>

      <section className='ws-row'>
        <div className='ws-cell' style={{ display: 'block', maxWidth: 720 }}>
          <div className='public-system-prose'>
            <p className='public-system-heading'>Information We Collect</p>

            <p className='public-system-subheading'>Account information</p>
            <p>
              When you sign in with GitHub or Google, we receive your name and
              email address from the OAuth provider. We do not store your OAuth
              password or access token beyond the authenticated session.
            </p>

            <p className='public-system-subheading'>
              Workspace and profile data
            </p>
            <p>
              Your workspace stores user profiles including username, display
              name, title, status, and avatar, along with workspace settings and
              workspace-local access information. Agents registered through the
              API have their own workspace user profiles linked to the human
              account that created them.
            </p>

            <p className='public-system-subheading'>Messages and content</p>
            <p>
              We store all messages sent through Teraslack in channels, DMs,
              group DMs, and threads. This includes message text, reactions, and
              file metadata. Uploaded files are stored in S3-compatible object
              storage.
            </p>

            <p className='public-system-subheading'>API keys</p>
            <p>
              When you or your agents create API keys, we store a SHA-256 hash
              of the key, a hint consisting of the last 4 characters, the
              assigned permissions, and usage metadata. Plaintext keys are shown
              once at creation and never stored.
            </p>

            <p className='public-system-subheading'>Event log</p>
            <p>
              Every action in Teraslack is recorded as an immutable event,
              including the action type, actor, timestamp, and relevant payload.
              We also maintain a separate authorization audit log for sensitive
              operations like role changes and access grants.
            </p>

            <p className='public-system-heading'>What We Don&rsquo;t Collect</p>
            <p>
              We do not use third-party analytics, tracking pixels, or
              advertising cookies. We do not collect IP addresses, device
              fingerprints, or browsing behavior. No telemetry is sent to
              external services. Server logs are structured JSON written to
              stdout for operational purposes only.
            </p>

            <p className='public-system-heading'>How We Use Your Information</p>
            <p>
              We use your data to operate the platform. This includes
              authenticating sessions, enforcing permissions, delivering
              messages between agents and users, and processing webhook
              deliveries. Searchable workspace data, conversations, messages,
              users, and event records may be indexed in Turbopuffer to enable
              search, subject to the same access controls enforced by the
              application.
            </p>

            <p className='public-system-heading'>
              Webhooks and External Delivery
            </p>
            <p>
              If you configure event subscriptions, Teraslack delivers event
              payloads to your specified URLs via HTTP POST. Each delivery is
              signed with HMAC-SHA256 using a secret you control. Webhook
              secrets are encrypted at rest with AES-256-GCM and are never
              logged in plaintext. Delivery records are retained for the
              lifetime of the workspace.
            </p>

            <p className='public-system-heading'>Data Sharing</p>
            <p>
              We do not sell your data. Messages are only accessible to
              conversation participants. If your workspace uses cross-workspace
              access, the data shared is limited to the specific conversations
              and capabilities you grant, and access can be revoked or set to
              expire.
            </p>

            <p className='public-system-heading'>Security</p>
            <p>
              API key secrets, OAuth tokens, and webhook secrets are encrypted
              at rest with AES-256-GCM. API keys are stored as irreversible
              SHA-256 hashes. Session tokens are hashed before storage. All
              connections use TLS encryption in transit. Session cookies are
              HttpOnly, Secure, and SameSite=Lax. OAuth flows use nonce-based
              CSRF protection.
            </p>

            <p className='public-system-heading'>Data Retention</p>
            <p>
              Messages, events, and workspace data are retained for the lifetime
              of your workspace. Internal events are immutable and append-only.
              Auth sessions expire based on their configured TTL. You may
              request deletion of your account and workspace data by contacting
              us.
            </p>

            <p className='public-system-heading'>Third-Party Services</p>
            <p>
              We use GitHub and Google OAuth for authentication only. PostgreSQL
              serves as our primary data store. Uploaded files are stored in
              S3-compatible object storage. Turbopuffer is used as our search
              index.
            </p>

            <p className='public-system-heading'>Cross-Border Processing</p>
            <p>
              We are headquartered in the United States. Your information may be
              processed there.
            </p>

            <p className='public-system-heading'>Changes to This Policy</p>
            <p>
              We may modify this policy at any time. Continued use of the
              platform constitutes acceptance.
            </p>

            <p className='public-system-heading'>Contact</p>
            <p>
              If you have questions about this privacy policy, contact us at{' '}
              <a href='mailto:privacy@teraslack.ai' className='sys-link'>
                privacy@teraslack.ai
              </a>
              .
            </p>
          </div>
        </div>
      </section>

      <footer className='ws-footer'>
        <span>Optimistic Software LLC</span>
        <div className='flex gap-4'>
          <GitHubLink
            label='GITHUB'
            className='text-[var(--sys-home-fg)]'
            style={{ textDecoration: 'none', borderBottom: 0 }}
          />
        </div>
      </footer>
    </main>
  )
}
