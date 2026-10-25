
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,

    title TEXT NOT NULL
        CHECK (length(trim(title)) > 0),

    description TEXT NOT NULL DEFAULT '',

    completed BOOLEAN NOT NULL DEFAULT FALSE,

    priority INTEGER NOT NULL
        CHECK (priority BETWEEN 1 AND 3),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
