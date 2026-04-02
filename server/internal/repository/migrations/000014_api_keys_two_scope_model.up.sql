ALTER TABLE public.api_keys
    ADD COLUMN scope text,
    ADD COLUMN owner_account_id text,
    ADD COLUMN workspace_ids text[] DEFAULT NULL;

UPDATE public.api_keys AS ak
SET
    scope = CASE
        WHEN ak.principal_id IS NULL THEN 'workspace_system'
        ELSE 'account'
    END,
    owner_account_id = u.account_id,
    workspace_id = CASE
        WHEN ak.principal_id IS NULL THEN ak.workspace_id
        ELSE NULL
    END
FROM public.users AS u
WHERE ak.principal_id = u.id;

UPDATE public.api_keys
SET scope = 'workspace_system'
WHERE scope IS NULL;

ALTER TABLE public.api_keys
    ALTER COLUMN scope SET NOT NULL,
    ALTER COLUMN workspace_id DROP NOT NULL;

ALTER TABLE public.api_keys
    ADD CONSTRAINT api_keys_scope_check CHECK (scope = ANY (ARRAY['account'::text, 'workspace_system'::text])),
    ADD CONSTRAINT api_keys_scope_shape_check CHECK (
        (scope = 'account' AND owner_account_id IS NOT NULL AND workspace_id IS NULL)
        OR
        (scope = 'workspace_system' AND owner_account_id IS NULL AND workspace_id IS NOT NULL)
    ),
    ADD CONSTRAINT api_keys_workspace_ids_scope_check CHECK (
        scope = 'account' OR workspace_ids IS NULL OR cardinality(workspace_ids) = 0
    ),
    ADD CONSTRAINT api_keys_owner_account_id_fkey FOREIGN KEY (owner_account_id) REFERENCES public.accounts(id);

CREATE INDEX idx_api_keys_scope ON public.api_keys USING btree (scope);
CREATE INDEX idx_api_keys_owner_account_id ON public.api_keys USING btree (owner_account_id);

