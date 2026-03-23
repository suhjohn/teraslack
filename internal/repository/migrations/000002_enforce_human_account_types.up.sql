UPDATE public.users
SET account_type = 'member'
WHERE principal_type = 'human' AND account_type = '';

ALTER TABLE public.users
    ADD CONSTRAINT users_account_type_by_principal_check CHECK (
        (principal_type = 'human' AND account_type = ANY (ARRAY['primary_admin'::text, 'admin'::text, 'member'::text])) OR
        (principal_type <> 'human' AND account_type = ''::text)
    );
