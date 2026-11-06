-- name: CreateRecurrence :one
INSERT INTO recurrence_rules (id,owner_id,title,description,priority,anchor_date,interval_days,next_run_date,reminder_enabled)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetRecurrence :one
SELECT * FROM recurrence_rules WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL;

-- name: ListRecurrences :many
SELECT * FROM recurrence_rules WHERE owner_id=$1 AND deleted_at IS NULL
AND (sqlc.narg(after_id)::uuid IS NULL OR id > sqlc.narg(after_id)::uuid)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: LockRecurrence :one
SELECT * FROM recurrence_rules WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL FOR UPDATE;

-- name: UpdateRecurrence :one
UPDATE recurrence_rules SET title=$3,description=$4,priority=$5,anchor_date=$6,
interval_days=$7,next_run_date=$8,paused=$9,deleted_at=$10,reminder_enabled=$12,version=version+1,updated_at=now()
WHERE id=$1 AND owner_id=$2 AND version=$11 AND deleted_at IS NULL RETURNING *;

-- name: LatestRecurrenceOccurrence :one
SELECT max(scheduled_date)::date AS last_date FROM recurrence_occurrences WHERE rule_id=$1 AND owner_id=$2;

-- name: LockDueRecurrences :many
SELECT * FROM recurrence_rules
WHERE NOT paused AND deleted_at IS NULL AND next_run_date <= sqlc.arg(today)::date
ORDER BY next_run_date,id
LIMIT sqlc.arg(batch_limit)
FOR UPDATE SKIP LOCKED;

-- name: InsertOccurrence :execrows
INSERT INTO recurrence_occurrences(rule_id,owner_id,scheduled_date)
VALUES ($1,$2,$3) ON CONFLICT (rule_id,scheduled_date) DO NOTHING;

-- name: InsertRecurringTask :exec
INSERT INTO tasks(id,owner_id,title,description,priority,plan_date,recurrence_id,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8);

-- name: AdvanceRecurrence :exec
UPDATE recurrence_rules SET next_run_date=$2,paused=$3,version=version+1,updated_at=now()
WHERE id=$1;
