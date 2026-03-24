CREATE TABLE public.workspace_invites (
    id text NOT NULL,
    team_id text NOT NULL,
    email text NOT NULL,
    invited_by text NOT NULL,
    token_hash text NOT NULL,
    accepted_by_user_id text,
    expires_at timestamp with time zone NOT NULL,
    accepted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_token_hash_key UNIQUE (token_hash);

CREATE INDEX idx_workspace_invites_team_id ON public.workspace_invites USING btree (team_id);
CREATE INDEX idx_workspace_invites_email ON public.workspace_invites USING btree (LOWER(email));

CREATE TRIGGER trg_workspace_invites_updated_at BEFORE UPDATE ON public.workspace_invites
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_team_id_fkey FOREIGN KEY (team_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_accepted_by_user_id_fkey FOREIGN KEY (accepted_by_user_id) REFERENCES public.users(id);
