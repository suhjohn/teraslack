DROP INDEX IF EXISTS public.idx_workspace_invites_accepted_by_membership_id;
DROP INDEX IF EXISTS public.idx_workspace_invites_accepted_by_account_id;

ALTER TABLE ONLY public.workspace_invites
    DROP CONSTRAINT IF EXISTS workspace_invites_accepted_by_membership_id_fkey;

ALTER TABLE ONLY public.workspace_invites
    DROP CONSTRAINT IF EXISTS workspace_invites_accepted_by_account_id_fkey;

ALTER TABLE ONLY public.workspace_invites
    DROP COLUMN IF EXISTS accepted_by_membership_id,
    DROP COLUMN IF EXISTS accepted_by_account_id;
