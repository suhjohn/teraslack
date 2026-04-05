DROP INDEX IF EXISTS public.idx_messages_author_workspace_membership_id;
DROP INDEX IF EXISTS public.idx_messages_author_account_id;

ALTER TABLE public.messages
    DROP CONSTRAINT IF EXISTS messages_author_workspace_membership_id_fkey,
    DROP CONSTRAINT IF EXISTS messages_author_account_id_fkey,
    DROP COLUMN IF EXISTS author_workspace_membership_id,
    DROP COLUMN IF EXISTS author_account_id;

DROP TABLE IF EXISTS public.conversation_posting_policy_allowed_accounts_v2;
DROP TABLE IF EXISTS public.conversation_manager_assignments_v2;
DROP TABLE IF EXISTS public.conversation_reads_v2;
DROP TABLE IF EXISTS public.conversation_members_v2;
DROP TABLE IF EXISTS public.workspace_membership_conversation_access;
DROP TABLE IF EXISTS public.workspace_profiles;
DROP TABLE IF EXISTS public.workspace_memberships;

DROP INDEX IF EXISTS public.idx_conversations_owner_workspace_id;
DROP INDEX IF EXISTS public.idx_conversations_owner_account_id;
DROP INDEX IF EXISTS public.idx_conversations_owner_type;

ALTER TABLE public.conversations
    DROP CONSTRAINT IF EXISTS conversations_owner_workspace_id_fkey,
    DROP CONSTRAINT IF EXISTS conversations_owner_account_id_fkey,
    DROP CONSTRAINT IF EXISTS conversations_exactly_one_owner_check,
    DROP CONSTRAINT IF EXISTS conversations_owner_type_check,
    DROP COLUMN IF EXISTS owner_workspace_id,
    DROP COLUMN IF EXISTS owner_account_id,
    DROP COLUMN IF EXISTS owner_type;
