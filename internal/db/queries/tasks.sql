
-- name: GetTask :one
SELECT id, title, description, completed, priority, created_at, due_date, version, owner_id, project_id, plan_date, recurrence_id
FROM tasks
WHERE tasks.id = $1 AND ((tasks.project_id IS NULL AND tasks.owner_id = $2) OR EXISTS (SELECT 1 FROM projects p WHERE p.id=tasks.project_id AND (p.owner_id=$2 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=p.id AND m.user_id=$2))));

-- name: ListTasks :many
SELECT id, title, description, completed, priority, created_at, due_date, version, owner_id, project_id, plan_date, recurrence_id
FROM tasks
WHERE ((tasks.project_id IS NULL AND tasks.owner_id = sqlc.arg(owner_id)) OR EXISTS (SELECT 1 FROM projects p WHERE p.id=tasks.project_id AND (p.owner_id=sqlc.arg(owner_id) OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=p.id AND m.user_id=sqlc.arg(owner_id)))))
  AND (sqlc.narg(plan_date_filter)::date IS NULL OR (tasks.owner_id = sqlc.arg(owner_id) AND plan_date = sqlc.narg(plan_date_filter)))
  AND (NOT sqlc.arg(unassigned)::boolean OR project_id IS NULL)
  AND (sqlc.narg(project_filter)::uuid IS NULL OR project_id = sqlc.narg(project_filter))
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
    priority, created_at, due_date, owner_id, project_id, plan_date
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

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
WHERE tasks.id = $1 AND tasks.version = $7 AND ((tasks.project_id IS NULL AND tasks.owner_id = $8) OR EXISTS (SELECT 1 FROM projects p WHERE p.id=tasks.project_id AND (p.owner_id=$8 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=p.id AND m.user_id=$8))))
 AND (tasks.project_id IS NOT DISTINCT FROM $9::uuid OR tasks.owner_id=$8)
 AND ($9::uuid IS NULL OR EXISTS (SELECT 1 FROM projects p WHERE p.id=$9 AND (p.owner_id=$8 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=p.id AND m.user_id=$8))))
RETURNING id, title, description, completed,
          priority, created_at, due_date, version, owner_id, project_id, plan_date, recurrence_id;

-- name: DeleteTask :one
DELETE FROM tasks
WHERE tasks.id = $1 AND ((tasks.project_id IS NULL AND tasks.owner_id = $2) OR EXISTS (SELECT 1 FROM projects p WHERE p.id=tasks.project_id AND (p.owner_id=$2 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=p.id AND m.user_id=$2))))
RETURNING id;

-- name: LockTask :one
SELECT id, title, description, completed, priority, created_at, due_date, version, owner_id, project_id, plan_date, recurrence_id FROM tasks WHERE tasks.id=$1 AND ((tasks.project_id IS NULL AND tasks.owner_id = $2) OR EXISTS (SELECT 1 FROM projects p WHERE p.id=tasks.project_id AND (p.owner_id=$2 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=p.id AND m.user_id=$2)))) FOR UPDATE;
