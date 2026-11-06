CREATE TABLE recurrence_rules (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users(id),
    title TEXT NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    priority INTEGER NOT NULL CHECK (priority BETWEEN 1 AND 3),
    anchor_date DATE NOT NULL CHECK (anchor_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    interval_days INTEGER NOT NULL CHECK (interval_days BETWEEN 1 AND 365),
    next_run_date DATE NOT NULL CHECK (next_run_date BETWEEN anchor_date AND DATE '9999-12-31'),
    paused BOOLEAN NOT NULL DEFAULT false,
    deleted_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, id),
    CHECK ((next_run_date - anchor_date) % interval_days = 0)
);
CREATE INDEX recurrence_owner_id_idx ON recurrence_rules(owner_id, id) WHERE deleted_at IS NULL;
CREATE INDEX recurrence_due_idx ON recurrence_rules(next_run_date, id) WHERE NOT paused AND deleted_at IS NULL;

-- The ledger is the parent of generated tasks. Deleting a task never deletes its receipt.
CREATE TABLE recurrence_occurrences (
    rule_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    scheduled_date DATE NOT NULL CHECK (scheduled_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(rule_id, scheduled_date),
    UNIQUE(rule_id, owner_id, scheduled_date),
    FOREIGN KEY(owner_id, rule_id) REFERENCES recurrence_rules(owner_id, id) ON DELETE RESTRICT
);
ALTER TABLE tasks ADD COLUMN recurrence_id UUID;
ALTER TABLE tasks ADD CONSTRAINT tasks_recurrence_plan_check CHECK (recurrence_id IS NULL OR plan_date IS NOT NULL);
ALTER TABLE tasks ADD CONSTRAINT tasks_occurrence_fk
    FOREIGN KEY(recurrence_id, owner_id, plan_date)
    REFERENCES recurrence_occurrences(rule_id, owner_id, scheduled_date) ON DELETE RESTRICT;
CREATE UNIQUE INDEX tasks_recurrence_date_key ON tasks(recurrence_id, plan_date) WHERE recurrence_id IS NOT NULL;
