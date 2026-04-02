CREATE TABLE public.workspace_memberships (
    id text NOT NULL,
    account_id text NOT NULL,
    workspace_id text NOT NULL,
    user_id text,
    account_type text NOT NULL DEFAULT 'member',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_user_id_key UNIQUE (user_id);

CREATE INDEX idx_workspace_memberships_account_id
    ON public.workspace_memberships USING btree (account_id);

CREATE INDEX idx_workspace_memberships_workspace_id
    ON public.workspace_memberships USING btree (workspace_id);

INSERT INTO public.workspace_memberships (
    id, account_id, workspace_id, user_id, account_type, created_at, updated_at
)
SELECT
    'WM_' || replace(gen_random_uuid()::text, '-', ''),
    u.account_id,
    u.workspace_id,
    u.id,
    u.account_type,
    u.created_at,
    u.updated_at
FROM public.users u
WHERE u.account_id IS NOT NULL
  AND u.account_id <> '';

ALTER TABLE ONLY public.users
    DROP CONSTRAINT IF EXISTS users_account_id_workspace_id_key;
