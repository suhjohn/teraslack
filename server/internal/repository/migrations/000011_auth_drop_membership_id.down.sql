ALTER TABLE ONLY public.auth_sessions
    ADD COLUMN membership_id text;

ALTER TABLE ONLY public.oauth_accounts
    ADD COLUMN membership_id text;
