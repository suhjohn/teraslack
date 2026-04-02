CREATE TABLE public.accounts (
    id text NOT NULL,
    principal_type text NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    real_name text DEFAULT ''::text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    email text DEFAULT ''::text NOT NULL,
    is_bot boolean DEFAULT false NOT NULL,
    deleted boolean DEFAULT false NOT NULL,
    profile jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT accounts_pkey PRIMARY KEY (id);

CREATE INDEX idx_accounts_email_lower
    ON public.accounts USING btree (LOWER(email));

CREATE TABLE public.workspace_memberships (
    id text NOT NULL,
    account_id text NOT NULL,
    workspace_id text NOT NULL,
    user_id text NOT NULL,
    account_type text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_user_id_key UNIQUE (user_id);

CREATE INDEX idx_workspace_memberships_account_id
    ON public.workspace_memberships USING btree (account_id);

CREATE INDEX idx_workspace_memberships_workspace_id
    ON public.workspace_memberships USING btree (workspace_id);

CREATE TABLE public.external_members (
    id text NOT NULL,
    conversation_id text NOT NULL,
    host_workspace_id text NOT NULL,
    external_workspace_id text NOT NULL,
    account_id text NOT NULL,
    access_mode text NOT NULL,
    allowed_capabilities jsonb DEFAULT '[]'::jsonb NOT NULL,
    invited_by text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone
);

ALTER TABLE ONLY public.external_members
    ADD CONSTRAINT external_members_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.external_members
    ADD CONSTRAINT external_members_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_members
    ADD CONSTRAINT external_members_host_workspace_id_fkey FOREIGN KEY (host_workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_members
    ADD CONSTRAINT external_members_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_members
    ADD CONSTRAINT external_members_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);

CREATE INDEX idx_external_members_conversation_id
    ON public.external_members USING btree (conversation_id);

CREATE INDEX idx_external_members_host_workspace_id
    ON public.external_members USING btree (host_workspace_id);

CREATE INDEX idx_external_members_external_workspace_id
    ON public.external_members USING btree (host_workspace_id, external_workspace_id);

CREATE UNIQUE INDEX idx_external_members_active_unique
    ON public.external_members USING btree (conversation_id, account_id)
    WHERE (revoked_at IS NULL);

INSERT INTO public.accounts (
    id, principal_type, name, real_name, display_name, email, is_bot, deleted, profile, created_at, updated_at
)
SELECT
    u.id,
    u.principal_type,
    u.name,
    u.real_name,
    u.display_name,
    u.email,
    u.is_bot,
    u.deleted,
    u.profile,
    u.created_at,
    u.updated_at
FROM public.users u
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.workspace_memberships (
    id, account_id, workspace_id, user_id, account_type, created_at, updated_at
)
SELECT
    'WM_' || md5(u.id),
    u.id,
    u.workspace_id,
    u.id,
    u.account_type,
    u.created_at,
    u.updated_at
FROM public.users u
ON CONFLICT (user_id) DO NOTHING;
