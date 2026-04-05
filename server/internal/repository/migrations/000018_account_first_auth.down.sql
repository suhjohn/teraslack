DELETE FROM public.auth_sessions
WHERE workspace_id IS NULL;

DELETE FROM public.oauth_accounts
WHERE workspace_id IS NULL;

UPDATE public.conversations
SET workspace_id = owner_workspace_id
WHERE workspace_id IS NULL
  AND owner_workspace_id IS NOT NULL;

DELETE FROM public.conversations
WHERE workspace_id IS NULL
   OR creator_id IS NULL;

DROP INDEX IF EXISTS idx_oauth_accounts_identity;

CREATE UNIQUE INDEX idx_oauth_accounts_identity
    ON public.oauth_accounts USING btree (workspace_id, provider, provider_subject);

ALTER TABLE public.auth_sessions
    ALTER COLUMN workspace_id SET NOT NULL;

ALTER TABLE public.oauth_accounts
    ALTER COLUMN workspace_id SET NOT NULL;

ALTER TABLE public.conversations
    ALTER COLUMN workspace_id SET NOT NULL,
    ALTER COLUMN creator_id SET NOT NULL;
