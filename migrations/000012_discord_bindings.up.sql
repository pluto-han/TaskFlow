CREATE TABLE discord_bindings (
 owner_id UUID PRIMARY KEY REFERENCES users(id),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version > 0),
 guild_id TEXT,
 channel_id TEXT,
 channel_name TEXT,
 discord_user_id TEXT,
 bound_at TIMESTAMPTZ,
 code_hash BYTEA UNIQUE CHECK(code_hash IS NULL OR octet_length(code_hash)=32),
 code_expires_at TIMESTAMPTZ,
 rate_started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 rate_count INTEGER NOT NULL DEFAULT 0 CHECK(rate_count BETWEEN 0 AND 3),
 CHECK ((code_hash IS NULL) = (code_expires_at IS NULL)),
 CHECK (
  (guild_id IS NULL AND channel_id IS NULL AND channel_name IS NULL AND discord_user_id IS NULL AND bound_at IS NULL)
  OR
  (guild_id ~ '^[1-9][0-9]{0,19}$' AND channel_id ~ '^[1-9][0-9]{0,19}$'
   AND discord_user_id ~ '^[1-9][0-9]{0,19}$' AND channel_name IS NOT NULL
   AND char_length(channel_name) BETWEEN 1 AND 100 AND bound_at IS NOT NULL
   AND guild_id IS NOT NULL AND channel_id IS NOT NULL AND discord_user_id IS NOT NULL)
 )
);
-- Successful interaction receipts survive unbinding; never store the payload/token/code.
CREATE TABLE discord_interactions (
 interaction_id TEXT PRIMARY KEY CHECK(interaction_id ~ '^[1-9][0-9]{0,19}$'),
 owner_id UUID NOT NULL REFERENCES discord_bindings(owner_id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX discord_interactions_created_idx ON discord_interactions(created_at);
