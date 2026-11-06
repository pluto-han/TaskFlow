-- Destructive: never use as an automatic application rollback.
DROP INDEX tasks_recurrence_date_key;
ALTER TABLE tasks DROP CONSTRAINT tasks_occurrence_fk;
ALTER TABLE tasks DROP CONSTRAINT tasks_recurrence_plan_check;
ALTER TABLE tasks DROP COLUMN recurrence_id;
DROP TABLE recurrence_occurrences;
DROP TABLE recurrence_rules;
