ALTER TABLE public.conversations
    ADD COLUMN owner_type text NOT NULL DEFAULT 'workspace',
    ADD COLUMN owner_account_id text,
    ADD COLUMN owner_workspace_id text;

UPDATE public.conversations
SET owner_type = 'workspace',
    owner_workspace_id = workspace_id,
    owner_account_id = NULL
WHERE owner_workspace_id IS NULL;

ALTER TABLE public.conversations
    ADD CONSTRAINT conversations_owner_type_check
        CHECK (owner_type = ANY (ARRAY['account'::text, 'workspace'::text]));

ALTER TABLE public.conversations
    ADD CONSTRAINT conversations_exactly_one_owner_check
        CHECK (
            (owner_type = 'workspace' AND owner_workspace_id IS NOT NULL AND owner_account_id IS NULL) OR
            (owner_type = 'account' AND owner_account_id IS NOT NULL AND owner_workspace_id IS NULL)
        );

ALTER TABLE public.conversations
    ADD CONSTRAINT conversations_owner_account_id_fkey
        FOREIGN KEY (owner_account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

CREATE INDEX idx_conversations_owner_type
    ON public.conversations USING btree (owner_type);

CREATE INDEX idx_conversations_owner_account_id
    ON public.conversations USING btree (owner_account_id);

CREATE INDEX idx_conversations_owner_workspace_id
    ON public.conversations USING btree (owner_workspace_id);

CREATE TABLE public.workspace_memberships (
    id text NOT NULL,
    workspace_id text NOT NULL,
    account_id text NOT NULL,
    role text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    membership_kind text DEFAULT 'full'::text NOT NULL,
    guest_scope text DEFAULT 'workspace_full'::text NOT NULL,
    created_by_account_id text,
    updated_by_account_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspace_memberships_pkey PRIMARY KEY (id),
    CONSTRAINT workspace_memberships_workspace_account_unique UNIQUE (workspace_id, account_id),
    CONSTRAINT workspace_memberships_role_check CHECK (role <> ''),
    CONSTRAINT workspace_memberships_status_check CHECK (status = ANY (ARRAY['active'::text, 'invited'::text, 'disabled'::text])),
    CONSTRAINT workspace_memberships_kind_check CHECK (membership_kind = ANY (ARRAY['full'::text, 'guest'::text])),
    CONSTRAINT workspace_memberships_guest_scope_check CHECK (guest_scope = ANY (ARRAY['single_conversation'::text, 'conversation_allowlist'::text, 'workspace_full'::text])),
    CONSTRAINT workspace_memberships_kind_scope_consistency_check CHECK (
        (membership_kind = 'full' AND guest_scope = 'workspace_full') OR
        (membership_kind = 'guest' AND guest_scope <> 'workspace_full')
    )
);

ALTER TABLE public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_workspace_id_fkey
    FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_account_id_fkey
    FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_created_by_account_id_fkey
    FOREIGN KEY (created_by_account_id) REFERENCES public.accounts(id);

ALTER TABLE public.workspace_memberships
    ADD CONSTRAINT workspace_memberships_updated_by_account_id_fkey
    FOREIGN KEY (updated_by_account_id) REFERENCES public.accounts(id);

CREATE INDEX idx_workspace_memberships_workspace_id
    ON public.workspace_memberships USING btree (workspace_id);

CREATE INDEX idx_workspace_memberships_account_id
    ON public.workspace_memberships USING btree (account_id);

INSERT INTO public.workspace_memberships (
    id,
    workspace_id,
    account_id,
    role,
    status,
    membership_kind,
    guest_scope,
    created_by_account_id,
    updated_by_account_id,
    created_at,
    updated_at
)
SELECT
    'WM_' || md5(u.workspace_id || ':' || u.account_id),
    u.workspace_id,
    u.account_id,
    CASE
        WHEN u.account_type = '' THEN 'member'
        ELSE u.account_type
    END,
    CASE
        WHEN u.deleted THEN 'disabled'
        ELSE 'active'
    END,
    'full',
    'workspace_full',
    u.account_id,
    u.account_id,
    u.created_at,
    u.updated_at
FROM public.users u
WHERE COALESCE(u.account_id, '') <> ''
ON CONFLICT (workspace_id, account_id) DO NOTHING;

INSERT INTO public.workspace_memberships (
    id,
    workspace_id,
    account_id,
    role,
    status,
    membership_kind,
    guest_scope,
    created_by_account_id,
    updated_by_account_id,
    created_at,
    updated_at
)
SELECT
    'WM_' || md5(em.host_workspace_id || ':' || em.account_id),
    em.host_workspace_id,
    em.account_id,
    'guest',
    CASE
        WHEN em.revoked_at IS NOT NULL THEN 'disabled'
        WHEN em.expires_at IS NOT NULL AND em.expires_at < NOW() THEN 'disabled'
        ELSE 'active'
    END,
    'guest',
    'single_conversation',
    COALESCE(inviter.account_id, em.account_id),
    COALESCE(inviter.account_id, em.account_id),
    em.created_at,
    em.created_at
FROM public.external_members em
LEFT JOIN public.users inviter
  ON inviter.id = em.invited_by
WHERE COALESCE(em.account_id, '') <> ''
ON CONFLICT (workspace_id, account_id) DO NOTHING;

CREATE TABLE public.workspace_profiles (
    workspace_id text NOT NULL,
    account_id text NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    real_name text DEFAULT ''::text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    profile jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspace_profiles_pkey PRIMARY KEY (workspace_id, account_id),
    CONSTRAINT workspace_profiles_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE,
    CONSTRAINT workspace_profiles_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE
);

INSERT INTO public.workspace_profiles (
    workspace_id,
    account_id,
    name,
    real_name,
    display_name,
    profile,
    created_at,
    updated_at
)
SELECT
    u.workspace_id,
    u.account_id,
    u.name,
    u.real_name,
    u.display_name,
    u.profile,
    u.created_at,
    u.updated_at
FROM public.users u
WHERE COALESCE(u.account_id, '') <> ''
ON CONFLICT (workspace_id, account_id) DO UPDATE SET
    name = EXCLUDED.name,
    real_name = EXCLUDED.real_name,
    display_name = EXCLUDED.display_name,
    profile = EXCLUDED.profile,
    updated_at = EXCLUDED.updated_at;

CREATE TABLE public.workspace_membership_conversation_access (
    workspace_membership_id text NOT NULL,
    conversation_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspace_membership_conversation_access_pkey PRIMARY KEY (workspace_membership_id, conversation_id),
    CONSTRAINT workspace_membership_conversation_access_membership_id_fkey FOREIGN KEY (workspace_membership_id) REFERENCES public.workspace_memberships(id) ON DELETE CASCADE,
    CONSTRAINT workspace_membership_conversation_access_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE
);

INSERT INTO public.workspace_membership_conversation_access (
    workspace_membership_id,
    conversation_id,
    created_at
)
SELECT DISTINCT
    wm.id,
    em.conversation_id,
    em.created_at
FROM public.external_members em
JOIN public.workspace_memberships wm
  ON wm.workspace_id = em.host_workspace_id
 AND wm.account_id = em.account_id
WHERE COALESCE(em.account_id, '') <> ''
ON CONFLICT (workspace_membership_id, conversation_id) DO NOTHING;

CREATE TABLE public.conversation_members_v2 (
    conversation_id text NOT NULL,
    account_id text NOT NULL,
    membership_role text DEFAULT 'member'::text NOT NULL,
    added_by_account_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversation_members_v2_pkey PRIMARY KEY (conversation_id, account_id),
    CONSTRAINT conversation_members_v2_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE,
    CONSTRAINT conversation_members_v2_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE,
    CONSTRAINT conversation_members_v2_added_by_account_id_fkey FOREIGN KEY (added_by_account_id) REFERENCES public.accounts(id) ON DELETE SET NULL
);

INSERT INTO public.conversation_members_v2 (
    conversation_id,
    account_id,
    membership_role,
    added_by_account_id,
    created_at
)
SELECT
    cm.conversation_id,
    u.account_id,
    'member',
    COALESCE(creator.account_id, u.account_id),
    cm.joined_at
FROM public.conversation_members cm
JOIN public.users u
  ON u.id = cm.user_id
JOIN public.conversations c
  ON c.id = cm.conversation_id
LEFT JOIN public.users creator
  ON creator.id = c.creator_id
WHERE COALESCE(u.account_id, '') <> ''
ON CONFLICT (conversation_id, account_id) DO NOTHING;

INSERT INTO public.conversation_members_v2 (
    conversation_id,
    account_id,
    membership_role,
    added_by_account_id,
    created_at
)
SELECT
    em.conversation_id,
    em.account_id,
    'guest',
    COALESCE(inviter.account_id, em.account_id),
    em.created_at
FROM public.external_members em
LEFT JOIN public.users inviter
  ON inviter.id = em.invited_by
WHERE COALESCE(em.account_id, '') <> ''
ON CONFLICT (conversation_id, account_id) DO NOTHING;

CREATE TABLE public.conversation_reads_v2 (
    conversation_id text NOT NULL,
    account_id text NOT NULL,
    last_read_ts text NOT NULL,
    last_read_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversation_reads_v2_pkey PRIMARY KEY (conversation_id, account_id),
    CONSTRAINT conversation_reads_v2_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE,
    CONSTRAINT conversation_reads_v2_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE
);

INSERT INTO public.conversation_reads_v2 (
    conversation_id,
    account_id,
    last_read_ts,
    last_read_at
)
SELECT
    cr.conversation_id,
    u.account_id,
    cr.last_read_ts,
    cr.last_read_at
FROM public.conversation_reads cr
JOIN public.users u
  ON u.id = cr.user_id
WHERE COALESCE(u.account_id, '') <> ''
ON CONFLICT (conversation_id, account_id) DO UPDATE SET
    last_read_ts = EXCLUDED.last_read_ts,
    last_read_at = EXCLUDED.last_read_at;

CREATE TABLE public.conversation_manager_assignments_v2 (
    conversation_id text NOT NULL,
    account_id text NOT NULL,
    assigned_by_account_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversation_manager_assignments_v2_pkey PRIMARY KEY (conversation_id, account_id),
    CONSTRAINT conversation_manager_assignments_v2_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE,
    CONSTRAINT conversation_manager_assignments_v2_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE,
    CONSTRAINT conversation_manager_assignments_v2_assigned_by_account_id_fkey FOREIGN KEY (assigned_by_account_id) REFERENCES public.accounts(id) ON DELETE SET NULL
);

INSERT INTO public.conversation_manager_assignments_v2 (
    conversation_id,
    account_id,
    assigned_by_account_id,
    created_at
)
SELECT
    cma.conversation_id,
    u.account_id,
    COALESCE(assigner.account_id, u.account_id),
    cma.created_at
FROM public.conversation_manager_assignments cma
JOIN public.users u
  ON u.id = cma.user_id
LEFT JOIN public.users assigner
  ON assigner.id = cma.assigned_by
WHERE COALESCE(u.account_id, '') <> ''
ON CONFLICT (conversation_id, account_id) DO NOTHING;

CREATE TABLE public.conversation_posting_policy_allowed_accounts_v2 (
    conversation_id text NOT NULL,
    account_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversation_posting_policy_allowed_accounts_v2_pkey PRIMARY KEY (conversation_id, account_id),
    CONSTRAINT conversation_posting_policy_allowed_accounts_v2_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE,
    CONSTRAINT conversation_posting_policy_allowed_accounts_v2_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE
);

INSERT INTO public.conversation_posting_policy_allowed_accounts_v2 (
    conversation_id,
    account_id,
    created_at
)
SELECT DISTINCT
    cpp.conversation_id,
    u.account_id,
    cpp.updated_at
FROM public.conversation_posting_policies cpp
CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(cpp.policy_json -> 'allowed_user_ids', '[]'::jsonb)) AS allowed_user_id(user_id)
JOIN public.users u
  ON u.id = allowed_user_id.user_id
WHERE COALESCE(u.account_id, '') <> ''
ON CONFLICT (conversation_id, account_id) DO NOTHING;

ALTER TABLE public.messages
    ADD COLUMN author_account_id text,
    ADD COLUMN author_workspace_membership_id text;

UPDATE public.messages m
SET author_account_id = u.account_id
FROM public.users u
WHERE u.id = m.user_id
  AND COALESCE(u.account_id, '') <> ''
  AND m.author_account_id IS NULL;

UPDATE public.messages m
SET author_workspace_membership_id = wm.id
FROM public.conversations c,
     public.users u,
     public.workspace_memberships wm
WHERE c.id = m.channel_id
  AND u.id = m.user_id
  AND wm.workspace_id = c.workspace_id
  AND wm.account_id = u.account_id
  AND COALESCE(u.account_id, '') <> ''
  AND m.author_workspace_membership_id IS NULL;

ALTER TABLE public.messages
    ADD CONSTRAINT messages_author_account_id_fkey
        FOREIGN KEY (author_account_id) REFERENCES public.accounts(id) ON DELETE SET NULL;

ALTER TABLE public.messages
    ADD CONSTRAINT messages_author_workspace_membership_id_fkey
        FOREIGN KEY (author_workspace_membership_id) REFERENCES public.workspace_memberships(id) ON DELETE SET NULL;

CREATE INDEX idx_messages_author_account_id
    ON public.messages USING btree (author_account_id);

CREATE INDEX idx_messages_author_workspace_membership_id
    ON public.messages USING btree (author_workspace_membership_id);
