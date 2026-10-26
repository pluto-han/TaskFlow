CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_normalized CHECK (
        email = lower(btrim(email)) AND octet_length(email) BETWEEN 3 AND 254
    ),
    CONSTRAINT users_email_key UNIQUE (email)
);
