import { createFileRoute } from '@tanstack/react-router'
import { LoaderCircle } from 'lucide-react'
import { useState } from 'react'
import { CodeBlock } from '../../components/ui/code-block'
import { useAdmin, formatDate } from '../../lib/admin'
import { useListWorkspaceAuthorizationAuditLogs } from '../../lib/openapi'
import type { AuthorizationAuditLogsCollection } from '../../lib/openapi'

export const Route = createFileRoute('/workspace/audit')({
  component: AuditPage,
})

function AuditPage() {
  const { workspaceID } = useAdmin()

  const auditQuery =
    useListWorkspaceAuthorizationAuditLogs<AuthorizationAuditLogsCollection>(
      workspaceID,
      { limit: 100 },
      { query: { enabled: !!workspaceID, retry: false } },
    )

  const items = auditQuery.data?.items ?? []

  return (
    <div>
      <div className="mb-3">
        <h2 className="text-sm font-bold text-[var(--ink)]">Audit log</h2>
        <p className="mt-0.5 text-xs text-[var(--ink-soft)]">
          Privileged mutations — who changed what, when, and how.
        </p>
      </div>

      {auditQuery.isFetching && !items.length ? (
        <div className="flex items-center justify-center py-10">
          <LoaderCircle className="h-4 w-4 animate-spin text-[var(--ink-soft)]" />
        </div>
      ) : items.length ? (
        <div className="border border-[var(--line)]">
          {items.map((log, index) => (
            <AuditRow key={log.id} log={log} index={index} />
          ))}
        </div>
      ) : (
        <p className="py-4 text-xs text-[var(--ink-soft)]">
          No audit log entries for this workspace.
        </p>
      )}
    </div>
  )
}

function AuditRow({
  log,
  index,
}: {
  log: AuthorizationAuditLogsCollection['items'][number]
  index: number
}) {
  const [expanded, setExpanded] = useState(false)
  const hasMetadata =
    log.metadata && Object.keys(log.metadata).length > 0

  return (
    <div className={index > 0 ? 'border-t border-[var(--line)]' : ''}>
      <button
        type="button"
        className="flex w-full items-start gap-4 px-4 py-3 text-left hover:bg-[var(--accent-faint)] disabled:cursor-default"
        onClick={() => hasMetadata && setExpanded((p) => !p)}
        disabled={!hasMetadata}
      >
        {/* Action */}
        <span className="min-w-[220px] font-mono text-xs font-medium text-[var(--ink)]">
          {log.action}
        </span>

        {/* Resource */}
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-[var(--ink-soft)]">
          {log.resource}/{log.resource_id}
        </span>

        {/* Actor */}
        <span className="w-48 flex-none truncate text-right text-[11px] text-[var(--ink-soft)]">
          {log.api_key_id ? (
            <span title={`via API key ${log.api_key_id}`}>
              {log.actor_id || 'system'}{' '}
              <span className="opacity-50">via key</span>
            </span>
          ) : (
            log.actor_id || 'system'
          )}
          {log.on_behalf_of ? (
            <span className="opacity-50"> → {log.on_behalf_of}</span>
          ) : null}
        </span>

        {/* Time */}
        <span className="w-36 flex-none text-right text-[11px] text-[var(--ink-soft)]">
          {formatDate(log.created_at)}
        </span>
      </button>

      {expanded && hasMetadata ? (
        <div className="border-t border-[var(--line)] bg-[var(--surface)] px-4 py-3">
          <CodeBlock className="max-h-[240px] overflow-auto text-xs">
            {JSON.stringify(log.metadata, null, 2)}
          </CodeBlock>
        </div>
      ) : null}
    </div>
  )
}
