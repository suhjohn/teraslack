CREATE TABLE IF NOT EXISTS public.external_principal_access (
    id text NOT NULL,
    host_workspace_id text NOT NULL,
    principal_id text NOT NULL,
    principal_type text NOT NULL,
    home_workspace_id text NOT NULL,
    access_mode text NOT NULL,
    allowed_capabilities jsonb NOT NULL DEFAULT '[]'::jsonb,
    granted_by text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone
);

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_pkey PRIMARY KEY (id);

CREATE INDEX IF NOT EXISTS idx_external_principal_access_principal
    ON public.external_principal_access USING btree (host_workspace_id, principal_id);

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_host_workspace_id_fkey FOREIGN KEY (host_workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES public.users(id);

CREATE TABLE IF NOT EXISTS public.external_principal_conversation_assignments (
    access_id text NOT NULL,
    conversation_id text NOT NULL,
    granted_by text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now()
);

ALTER TABLE ONLY public.external_principal_conversation_assignments
    ADD CONSTRAINT external_principal_conversation_assignments_access_id_conversation_key UNIQUE (access_id, conversation_id);

ALTER TABLE ONLY public.external_principal_conversation_assignments
    ADD CONSTRAINT external_principal_conversation_assignments_access_id_fkey FOREIGN KEY (access_id) REFERENCES public.external_principal_access(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_conversation_assignments
    ADD CONSTRAINT external_principal_conversation_assignments_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_conversation_assignments
    ADD CONSTRAINT external_principal_conversation_assignments_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES public.users(id);
