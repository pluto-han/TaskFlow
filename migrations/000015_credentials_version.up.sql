ALTER TABLE users ADD COLUMN credentials_version BIGINT NOT NULL DEFAULT 0
    CHECK (credentials_version >= 0);

CREATE INDEX sessions_user_created_id_idx ON sessions (user_id, created_at DESC, id DESC)
    WHERE revoked_at IS NULL;
