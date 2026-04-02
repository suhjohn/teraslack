ALTER TABLE public.oauth_accounts
    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE public.oauth_accounts
    DROP COLUMN membership_id,
    DROP COLUMN account_id;

ALTER TABLE public.auth_sessions
    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE public.auth_sessions
    DROP COLUMN membership_id,
    DROP COLUMN account_id;
