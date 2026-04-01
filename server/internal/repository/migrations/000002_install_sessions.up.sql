CREATE TABLE public.install_sessions (
    id text PRIMARY KEY,
    poll_token_hash text NOT NULL UNIQUE,
    status text NOT NULL,
    workspace_id text NOT NULL DEFAULT '',
    approved_by_user_id text NOT NULL DEFAULT '',
    credential_id text NOT NULL DEFAULT '',
    raw_credential_encrypted text NOT NULL DEFAULT '',
    device_name text NOT NULL DEFAULT '',
    client_kind text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    approved_at timestamptz NULL,
    consumed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT install_sessions_status_check CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'consumed'::text, 'expired'::text, 'cancelled'::text]))
);

CREATE INDEX idx_install_sessions_expires_at ON public.install_sessions(expires_at);
CREATE INDEX idx_install_sessions_status ON public.install_sessions(status);

CREATE TRIGGER trg_install_sessions_updated_at
BEFORE UPDATE ON public.install_sessions
FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();
