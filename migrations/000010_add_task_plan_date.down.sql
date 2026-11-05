-- Destructive: removes plan membership. Never use as a production app rollback.
DROP INDEX tasks_owner_plan_created_idx;
ALTER TABLE tasks DROP CONSTRAINT tasks_plan_date_range_check;
ALTER TABLE tasks DROP CONSTRAINT tasks_plan_private_check;
ALTER TABLE tasks DROP COLUMN plan_date;
