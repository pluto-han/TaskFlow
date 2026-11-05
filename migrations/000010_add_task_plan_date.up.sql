-- Additive migration: existing tasks remain ordinary tasks (NULL plan_date).
ALTER TABLE tasks ADD COLUMN plan_date DATE;
ALTER TABLE tasks ADD CONSTRAINT tasks_plan_private_check
    CHECK (plan_date IS NULL OR project_id IS NULL);
ALTER TABLE tasks ADD CONSTRAINT tasks_plan_date_range_check
    CHECK (plan_date IS NULL OR plan_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31');
CREATE INDEX tasks_owner_plan_created_idx
    ON tasks(owner_id, plan_date, created_at DESC, id DESC)
    WHERE plan_date IS NOT NULL;
