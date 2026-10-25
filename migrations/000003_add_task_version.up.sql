
ALTER TABLE tasks
ADD COLUMN version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE tasks
ADD CONSTRAINT tasks_version_positive
CHECK (version > 0);
