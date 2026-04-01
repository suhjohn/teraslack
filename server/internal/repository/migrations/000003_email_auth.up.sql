ALTER TABLE public.auth_sessions
DROP CONSTRAINT auth_sessions_provider_check;

ALTER TABLE public.auth_sessions
ADD CONSTRAINT auth_sessions_provider_check
CHECK ((provider = ANY (ARRAY['email'::text, 'github'::text, 'google'::text])));

CREATE TABLE public.email_verification_challenges (
    id text PRIMARY KEY,
    email text NOT NULL,
    code_hash text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX idx_email_verification_challenges_email
    ON public.email_verification_challenges USING btree (LOWER(email));

CREATE INDEX idx_email_verification_challenges_expires_at
    ON public.email_verification_challenges USING btree (expires_at);
