ALTER TABLE ONLY public.accounts
	ADD COLUMN IF NOT EXISTS name text DEFAULT ''::text NOT NULL,
	ADD COLUMN IF NOT EXISTS real_name text DEFAULT ''::text NOT NULL,
	ADD COLUMN IF NOT EXISTS display_name text DEFAULT ''::text NOT NULL,
	ADD COLUMN IF NOT EXISTS profile jsonb DEFAULT '{}'::jsonb NOT NULL;
