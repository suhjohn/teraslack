ALTER TABLE public.auth_sessions
    ALTER COLUMN workspace_id DROP NOT NULL;

ALTER TABLE public.oauth_accounts
    ALTER COLUMN workspace_id DROP NOT NULL;

ALTER INDEX IF EXISTS public.idx_oauth_accounts_identity
    RENAME TO idx_oauth_accounts_identity_workspace;

DROP INDEX IF EXISTS public.idx_oauth_accounts_identity_workspace;

CREATE UNIQUE INDEX idx_oauth_accounts_identity
    ON public.oauth_accounts USING btree (provider, provider_subject);

ALTER TABLE public.conversations
    ALTER COLUMN workspace_id DROP NOT NULL,
    ALTER COLUMN creator_id DROP NOT NULL;
