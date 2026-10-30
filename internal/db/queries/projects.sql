-- name: CreateProject :one
INSERT INTO projects(id, owner_id, name, description)
VALUES ($1, $2, $3, $4)
RETURNING id, owner_id, name, description, created_at, updated_at;

-- name: GetProject :one
SELECT id, owner_id, name, description, created_at, updated_at
FROM projects WHERE id = $1 AND (owner_id = $2 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=projects.id AND m.user_id=$2));

-- name: ListProjects :many
SELECT id, owner_id, name, description, created_at, updated_at
FROM projects WHERE owner_id = $1 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=projects.id AND m.user_id=$1)
ORDER BY created_at DESC, id DESC;

-- name: UpdateProject :one
UPDATE projects SET
    name = COALESCE(sqlc.narg(name)::text, name),
    description = COALESCE(sqlc.narg(description)::text, description),
    updated_at = now()
WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id)
RETURNING id, owner_id, name, description, created_at, updated_at;

-- name: DeleteProject :one
DELETE FROM projects WHERE id = $1 AND owner_id = $2 RETURNING id;
