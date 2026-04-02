ALTER TABLE public.api_keys
	ADD COLUMN IF NOT EXISTS principal_id text;

UPDATE public.api_keys AS ak
SET principal_id = u.id
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
	ADD CONSTRAINT api_keys_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.users(id);

CREATE INDEX idx_api_keys_principal_id ON public.api_keys USING btree (principal_id);
