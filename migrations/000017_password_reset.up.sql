ALTER TABLE email_tokens DROP CONSTRAINT email_tokens_purpose_check;
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_purpose_check CHECK (purpose IN ('verify_email','reset_password'));
ALTER TABLE email_tokens ADD COLUMN credentials_version BIGINT;
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_reset_version_check
    CHECK ((purpose='verify_email' AND credentials_version IS NULL)
        OR (purpose='reset_password' AND credentials_version IS NOT NULL AND credentials_version>=0));

-- Includes unknown targets, so throttling cannot disclose account existence.
-- Digests still represent personal data; retain for one rolling day only.
CREATE TABLE email_reset_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_hash BYTEA NOT NULL CHECK (octet_length(target_hash)=32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX email_reset_requests_target_idx ON email_reset_requests(target_hash,created_at DESC);
CREATE INDEX email_reset_requests_created_idx ON email_reset_requests(created_at);
