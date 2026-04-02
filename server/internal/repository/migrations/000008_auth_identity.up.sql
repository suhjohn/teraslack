ALTER TABLE public.auth_sessions
    ADD COLUMN account_id text,
    ADD COLUMN membership_id text;

ALTER TABLE public.auth_sessions
    ALTER COLUMN user_id DROP NOT NULL;

ALTER TABLE public.oauth_accounts
    ADD COLUMN account_id text,
    ADD COLUMN membership_id text;

ALTER TABLE public.oauth_accounts
    ALTER COLUMN user_id DROP NOT NULL;
