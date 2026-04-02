ALTER TABLE public.workspace_invites
	ADD COLUMN accepted_by_membership_id text;

UPDATE public.workspace_invites wi
SET accepted_by_membership_id = wm.id
FROM public.workspace_memberships wm
WHERE wi.accepted_by_account_id = wm.account_id
  AND wi.workspace_id = wm.workspace_id
  AND wi.accepted_at IS NOT NULL;

ALTER TABLE public.workspace_invites
	ADD CONSTRAINT workspace_invites_accepted_by_membership_id_fkey
		FOREIGN KEY (accepted_by_membership_id) REFERENCES public.workspace_memberships(id);

CREATE INDEX idx_workspace_invites_accepted_by_membership_id
	ON public.workspace_invites USING btree (accepted_by_membership_id);
