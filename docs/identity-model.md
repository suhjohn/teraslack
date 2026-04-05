# Identity Model

The canonical identity model is account-first:

- `Account` is the global authentication identity.
- `WorkspaceMembership` is the canonical workspace access record keyed by `(workspace_id, account_id)`.
- `WorkspaceProfile` is the workspace-local presentation record keyed by `(workspace_id, account_id)`.
- `User` is the workspace-local directory and persona surface linked to an account.

## Canonical ownership

- Global auth fields live on `accounts`.
- Workspace access, role, status, guest scope, and membership policy live on `workspace_memberships`.
- Workspace-local persona fields live on `workspace_profiles`.
- Workspace-local directory and product-facing actor fields live on `users`.

## Conversations and messages

- Conversations are owned by exactly one subject: either an `account` or a `workspace`.
- `conversations.owner_type` is the canonical ownership discriminator.
- `conversations.owner_account_id` is used when the conversation is account-owned.
- `conversations.owner_workspace_id` is used when the conversation is workspace-owned.
- Messages are authored canonically by `author_account_id`.
- Workspace-owned messages may also carry `author_workspace_membership_id` so rendering can use the workspace-local persona record.
- `messages.user_id` remains the workspace-local actor reference used by product-facing message surfaces.

## Deleted semantics

- `accounts.deleted` is the global auth-level disable flag.
- `users.deleted` is the workspace-local directory disable flag.
- Workspace memberships carry the live access state that new authorization should use.

## Design Rules

1. Authentication resolves to `Account` first.
2. Workspace authorization resolves through `WorkspaceMembership`.
3. Workspace-local presentation resolves through `User` and `WorkspaceProfile`.
4. Conversation membership, posting policy, read state, and management are account-keyed.
5. Product-facing surfaces may still expose workspace-local actor records where the UI needs them.
