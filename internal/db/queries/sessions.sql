-- name: CreateSession :one
INSERT INTO sessions (id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, now() + interval '24 hours')
RETURNING id, user_id, expires_at;

-- name: GetActiveSession :one
SELECT s.id, s.user_id, s.expires_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()
  AND s.credential_kind = 'bearer';

-- name: CreateBrowserSession :one
INSERT INTO sessions (id, user_id, token_hash, expires_at, credential_kind, csrf_secret)
VALUES ($1, $2, $3, now() + interval '24 hours', 'browser', $4)
RETURNING id, user_id, expires_at;

-- name: GetActiveBrowserSession :one
SELECT s.id, s.user_id, s.expires_at, s.csrf_secret
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()
  AND s.credential_kind = 'browser';

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > clock_timestamp();

-- name: LockActiveSession :one
SELECT id FROM sessions
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > clock_timestamp()
FOR UPDATE;

-- name: RevokeAllSessions :exec
UPDATE sessions SET revoked_at = clock_timestamp()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: ListActiveSessions :many
SELECT id, created_at, expires_at, credential_kind
FROM sessions
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL AND expires_at > now()
  AND (NOT sqlc.arg(has_cursor)::boolean OR (created_at, id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT $1;
