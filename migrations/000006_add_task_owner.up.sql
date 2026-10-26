-- Requires an empty tasks table. Existing data needs an explicitly approved
-- cleanup or a separate ownership/backfill plan; never assign it implicitly.
ALTER TABLE tasks ADD COLUMN owner_id UUID NOT NULL REFERENCES users(id);
CREATE INDEX tasks_owner_created_idx ON tasks(owner_id, created_at DESC, id DESC);
