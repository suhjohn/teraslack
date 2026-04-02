# Identity Model

The canonical identity model is:

- `Account`: the global authentication identity.
- `User`: the workspace-local identity, access record, and persona.
- The old workspace-membership join model is removed. Any remaining mentions exist only in migration history and migration-focused tests.

## Canonical ownership

- Global auth fields live on `accounts`:
  - `email`
  - `principal_type`
  - `is_bot`
  - `deleted`
- Workspace-local access and persona fields live on `users`:
  - `workspace_id`
  - `account_id`
  - `account_type`
  - display/profile fields used inside a workspace

Former account-level persona columns are removed. Workspace-local persona lives only on `users`.

## Deleted semantics

- `accounts.deleted` is the global auth-level disable flag. Deleted accounts are not reused for human login/signup resolution.
- `users.deleted` is the workspace-local disable flag. A deleted user is excluded from normal workspace participation even if the linked account still exists globally.
- This split is intentional: global identity lifecycle is account-owned, while workspace membership/presence lifecycle is user-owned.

## Invite acceptance

- Workspace invites persist `accepted_by_account_id` as the canonical acceptance actor.
- The accepted workspace-local `User` is derived by resolving or creating the account's `User` row in the invited workspace at acceptance time.

## Invariants

- One `Account` can appear in many workspaces.
- Each workspace has at most one `User` row for a given `Account`.
- `users.id` stays stable during migration; existing user IDs remain the workspace-local actor IDs used by product tables.

## Migration direction

The migration path is:

1. Add `users.account_id`.
2. Backfill `users.account_id` from the former workspace-account join table.
3. Enforce uniqueness on `(account_id, workspace_id)`.
4. Move auth/session/oauth/invite flows to resolve `Account -> User in workspace`.
5. Remove the old join-based runtime logic.
6. Drop the old join storage and bridge fields once all runtime paths use `users.account_id`.

## Deploy order

Operational order for this migration:

1. Deploy the schema change that adds `users.account_id` and backfills it from the former workspace-account join table.
2. Verify the backfill completed and inspect for any duplicate `(account_id, workspace_id)` pairs before enforcing stricter constraints.
3. Deploy server code that resolves identity through `users.account_id` instead of join-based lookups.
4. Deploy API and frontend changes that remove the old join-shaped responses and routes.
5. After runtime traffic no longer depends on the legacy join, remove bridge columns and drop the old table.
