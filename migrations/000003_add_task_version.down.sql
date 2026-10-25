
ALTER TABLE tasks
DROP CONSTRAINT tasks_version_positive;

ALTER TABLE tasks
DROP COLUMN version;
