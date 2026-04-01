DROP INDEX IF EXISTS public.idx_email_verification_challenges_expires_at;
DROP INDEX IF EXISTS public.idx_email_verification_challenges_email;
DROP TABLE IF EXISTS public.email_verification_challenges;

ALTER TABLE public.auth_sessions
DROP CONSTRAINT auth_sessions_provider_check;

ALTER TABLE public.auth_sessions
ADD CONSTRAINT auth_sessions_provider_check
CHECK ((provider = ANY (ARRAY['github'::text, 'google'::text])));
