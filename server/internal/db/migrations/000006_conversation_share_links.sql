alter table conversation_invites
  add column if not exists encrypted_token text;

update conversation_invites
set revoked_at = now()
where revoked_at is null;

create unique index if not exists conversation_invites_one_active_per_conversation
  on conversation_invites (conversation_id)
  where revoked_at is null;
