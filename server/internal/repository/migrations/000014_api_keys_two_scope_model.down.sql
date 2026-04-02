DROP INDEX IF EXISTS public.idx_api_keys_owner_account_id;
DROP INDEX IF EXISTS public.idx_api_keys_scope;

ALTER TABLE public.api_keys
    DROP CONSTRAINT IF EXISTS api_keys_owner_account_id_fkey,
    DROP CONSTRAINT IF EXISTS api_keys_workspace_ids_scope_check,
    DROP CONSTRAINT IF EXISTS api_keys_scope_shape_check,
    DROP CONSTRAINT IF EXISTS api_keys_scope_check;

UPDATE public.api_keys AS ak
SET
    workspace_id = COALESCE(ak.workspace_id, u.workspace_id, ''),
    principal_id = u.id
FROM public.users AS u
WHERE ak.scope = 'account'
  AND ak.owner_account_id = u.account_id
  AND (
      ak.workspace_ids IS NULL
      OR cardinality(ak.workspace_ids) = 0
      OR u.workspace_id = ANY(ak.workspace_ids)
  )
  AND ak.principal_id IS NULL;

ALTER TABLE public.api_keys
    ALTER COLUMN workspace_id SET NOT NULL;

ALTER TABLE public.api_keys
    DROP COLUMN IF EXISTS workspace_ids,
    DROP COLUMN IF EXISTS owner_account_id,
    DROP COLUMN IF EXISTS scope;
