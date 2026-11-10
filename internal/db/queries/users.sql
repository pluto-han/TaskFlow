-- name: CreateUser :one
INSERT INTO users (id, email, password_hash)
VALUES ($1, $2, $3)
RETURNING id, email, password_hash, created_at, credentials_version;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, created_at, credentials_version FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, password_hash, created_at, credentials_version FROM users WHERE id = $1;

-- name: LockUserCredentials :one
SELECT credentials_version FROM users WHERE id = $1 FOR UPDATE;

-- name: UpdatePassword :execrows
UPDATE users SET password_hash = sqlc.arg(password_hash), credentials_version = credentials_version + 1
WHERE id = sqlc.arg(id) AND credentials_version = sqlc.arg(expected_version);
