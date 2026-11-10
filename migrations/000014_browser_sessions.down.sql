-- Browser credentials must not become valid Bearer credentials after rollback.
UPDATE sessions SET revoked_at = COALESCE(revoked_at, now())
WHERE credential_kind = 'browser';
ALTER TABLE sessions
    DROP CONSTRAINT sessions_csrf_secret_check,
    DROP CONSTRAINT sessions_credential_kind_check,
    DROP COLUMN csrf_secret,
    DROP COLUMN credential_kind;
