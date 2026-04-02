DROP INDEX IF EXISTS public.idx_api_keys_principal_id;

ALTER TABLE public.api_keys
	DROP CONSTRAINT IF EXISTS api_keys_principal_id_fkey;

ALTER TABLE public.api_keys
	DROP COLUMN IF EXISTS principal_id;
