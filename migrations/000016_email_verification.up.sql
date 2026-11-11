ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

CREATE TABLE email_tokens (
    token_hash BYTEA PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL CHECK (purpose = 'verify_email'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);
CREATE INDEX email_tokens_user_created_idx ON email_tokens(user_id, created_at DESC);
CREATE INDEX email_tokens_created_idx ON email_tokens(created_at);
CREATE INDEX email_tokens_expiry_idx ON email_tokens(expires_at);

CREATE TABLE email_outbox (
    id UUID PRIMARY KEY,
    token_hash BYTEA NOT NULL REFERENCES email_tokens(token_hash) ON DELETE CASCADE,
    recipient TEXT,
    link TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    expires_at TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','leased','sent','failed')),
    lease_id UUID,
    lease_until TIMESTAMPTZ,
    CHECK ((recipient IS NULL) = (link IS NULL)),
    CHECK (status NOT IN ('sent','failed') OR link IS NULL)
);
CREATE INDEX email_outbox_token_idx ON email_outbox(token_hash);
CREATE INDEX email_outbox_ready_idx ON email_outbox(next_attempt_at) WHERE status IN ('pending','leased');
CREATE INDEX email_outbox_expiry_idx ON email_outbox(expires_at);
