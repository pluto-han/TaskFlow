ALTER TABLE sessions
    ADD COLUMN credential_kind TEXT NOT NULL DEFAULT 'bearer',
    ADD COLUMN csrf_secret BYTEA,
    ADD CONSTRAINT sessions_credential_kind_check
        CHECK (credential_kind IN ('bearer', 'browser')),
    ADD CONSTRAINT sessions_csrf_secret_check CHECK (
        (credential_kind = 'bearer' AND csrf_secret IS NULL)
        OR (credential_kind = 'browser' AND csrf_secret IS NOT NULL
            AND octet_length(csrf_secret) = 32)
    );
