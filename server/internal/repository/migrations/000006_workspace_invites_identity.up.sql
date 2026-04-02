ALTER TABLE ONLY public.workspace_invites
    ADD COLUMN accepted_by_account_id text,
    ADD COLUMN accepted_by_membership_id text;

UPDATE public.workspace_invites wi
SET accepted_by_account_id = wm.account_id,
    accepted_by_membership_id = wm.id
FROM public.workspace_memberships wm
WHERE wi.accepted_by_user_id = wm.user_id;

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_accepted_by_account_id_fkey
        FOREIGN KEY (accepted_by_account_id) REFERENCES public.accounts(id);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_accepted_by_membership_id_fkey
        FOREIGN KEY (accepted_by_membership_id) REFERENCES public.workspace_memberships(id);

CREATE INDEX idx_workspace_invites_accepted_by_account_id
    ON public.workspace_invites USING btree (accepted_by_account_id);

CREATE INDEX idx_workspace_invites_accepted_by_membership_id
    ON public.workspace_invites USING btree (accepted_by_membership_id);
