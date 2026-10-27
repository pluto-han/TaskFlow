
-- name: GetTask :one
SELECT id, title, description, completed, priority, created_at, due_date, version, owner_id, project_id
FROM tasks
WHERE id = $1 AND owner_id = $2;

-- name: ListTasks :many
SELECT id, title, description, completed, priority, created_at, due_date, version, owner_id, project_id
FROM tasks
WHERE owner_id = sqlc.arg(owner_id)
  AND (sqlc.narg(completed_filter)::boolean IS NULL OR completed = sqlc.narg(completed_filter))
  AND (sqlc.narg(priority_filter)::integer IS NULL OR priority = sqlc.narg(priority_filter))
  AND (sqlc.narg(has_due_date)::boolean IS NULL OR (due_date IS NOT NULL) = sqlc.narg(has_due_date))
  AND (sqlc.narg(due_from)::timestamptz IS NULL OR due_date >= sqlc.narg(due_from))
  AND (sqlc.narg(due_before)::timestamptz IS NULL OR due_date < sqlc.narg(due_before))
  AND (sqlc.narg(after_created_at)::timestamptz IS NULL OR
       (created_at, id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.arg(after_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit)::integer;

-- name: CreateTask :exec
INSERT INTO tasks (
    id, title, description, completed,
    priority, created_at, due_date, owner_id, project_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: UpdateTask :one
UPDATE tasks
SET
    title = $2,
    description = $3,
    completed = $4,
    priority = $5,
    due_date = $6,
    version = version + 1,
    project_id = $9
WHERE id = $1 AND version = $7 AND owner_id = $8
RETURNING id, title, description, completed,
          priority, created_at, due_date, version, owner_id, project_id;

-- name: DeleteTask :one
DELETE FROM tasks
WHERE id = $1 AND owner_id = $2
RETURNING id;
