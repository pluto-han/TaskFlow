-- Complements the owner/created_at/id index for selective deadline ranges.
CREATE INDEX tasks_owner_due_idx ON tasks(owner_id, due_date)
WHERE due_date IS NOT NULL;
