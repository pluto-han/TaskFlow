
-- name: GetTask :one
SELECT id, title, description, completed, priority, created_at, due_date, version, owner_id
FROM tasks
WHERE id = $1 AND owner_id = $2;

-- name: ListTasks :many
SELECT id, title, description, completed, priority, created_at, due_date, version, owner_id
FROM tasks
WHERE owner_id = $1
ORDER BY created_at DESC, id DESC;

-- name: CreateTask :exec
INSERT INTO tasks (
    id, title, description, completed,
    priority, created_at, due_date, owner_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateTask :one
UPDATE tasks
SET
    title = $2,
    description = $3,
    completed = $4,
    priority = $5,
    due_date = $6,
    version = version + 1
WHERE id = $1 AND version = $7 AND owner_id = $8
RETURNING id, title, description, completed,
          priority, created_at, due_date, version, owner_id;

-- name: DeleteTask :one
DELETE FROM tasks
WHERE id = $1 AND owner_id = $2
RETURNING id;
