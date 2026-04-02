ALTER TABLE ONLY public.users
    ADD COLUMN IF NOT EXISTS account_id text;

UPDATE public.users u
SET account_id = wm.account_id
FROM public.workspace_memberships wm
WHERE wm.user_id = u.id
  AND (u.account_id IS NULL OR u.account_id = '');

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_account_id_fkey
        FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_account_workspace_unique
    ON public.users USING btree (account_id, workspace_id)
    WHERE account_id IS NOT NULL AND account_id <> '';

CREATE INDEX IF NOT EXISTS idx_users_account_id
    ON public.users USING btree (account_id);
