-- Re-add the UNIQUE constraint on tokens.token column.
ALTER TABLE tokens ADD CONSTRAINT tokens_token_key UNIQUE (token);
