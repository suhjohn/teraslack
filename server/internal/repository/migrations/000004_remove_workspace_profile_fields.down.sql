ALTER TABLE public.workspaces
ADD COLUMN profile_fields jsonb DEFAULT '[]'::jsonb NOT NULL;
