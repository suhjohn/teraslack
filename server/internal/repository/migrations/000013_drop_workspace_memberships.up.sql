ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_account_id_workspace_id_key UNIQUE (account_id, workspace_id);

DROP INDEX IF EXISTS public.idx_workspace_memberships_workspace_id;
DROP INDEX IF EXISTS public.idx_workspace_memberships_account_id;

DROP TABLE IF EXISTS public.workspace_memberships;
