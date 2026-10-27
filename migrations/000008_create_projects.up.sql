CREATE TABLE projects (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 100),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT projects_owner_id_key UNIQUE(owner_id, id)
);
CREATE INDEX projects_owner_created_idx ON projects(owner_id, created_at DESC, id DESC);
ALTER TABLE tasks ADD COLUMN project_id UUID;
-- A task can reference only a project owned by the same authenticated user.
-- MATCH SIMPLE allows a NULL project_id while owner_id remains non-null.
ALTER TABLE tasks ADD CONSTRAINT tasks_project_owner_fk
    FOREIGN KEY(owner_id, project_id) REFERENCES projects(owner_id, id) ON DELETE RESTRICT;
CREATE INDEX tasks_owner_project_idx ON tasks(owner_id, project_id) WHERE project_id IS NOT NULL;
