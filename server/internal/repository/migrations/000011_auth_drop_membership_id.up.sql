ALTER TABLE ONLY public.auth_sessions
    DROP COLUMN IF EXISTS membership_id;

ALTER TABLE ONLY public.oauth_accounts
    DROP COLUMN IF EXISTS membership_id;
