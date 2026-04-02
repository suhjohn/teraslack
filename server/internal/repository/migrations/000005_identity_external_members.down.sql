DROP INDEX IF EXISTS idx_external_members_active_unique;
DROP INDEX IF EXISTS idx_external_members_external_workspace_id;
DROP INDEX IF EXISTS idx_external_members_host_workspace_id;
DROP INDEX IF EXISTS idx_external_members_conversation_id;
DROP TABLE IF EXISTS public.external_members;

DROP INDEX IF EXISTS idx_workspace_memberships_workspace_id;
DROP INDEX IF EXISTS idx_workspace_memberships_account_id;
DROP TABLE IF EXISTS public.workspace_memberships;

DROP INDEX IF EXISTS idx_accounts_email_lower;
DROP TABLE IF EXISTS public.accounts;
