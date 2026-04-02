DROP INDEX IF EXISTS idx_users_account_id;
DROP INDEX IF EXISTS idx_users_account_workspace_unique;

ALTER TABLE ONLY public.users
    DROP CONSTRAINT IF EXISTS users_account_id_fkey;

ALTER TABLE ONLY public.users
    DROP COLUMN IF EXISTS account_id;
