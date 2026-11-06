ALTER TABLE recurrence_rules ADD COLUMN reminder_enabled BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE notification_outbox (
 id UUID PRIMARY KEY,
 owner_id UUID NOT NULL,
 rule_id UUID NOT NULL,
 scheduled_date DATE NOT NULL,
 task_id TEXT NOT NULL,
 title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
 channel_id TEXT NOT NULL CHECK (channel_id ~ '^[1-9][0-9]{0,19}$'),
 binding_version BIGINT NOT NULL CHECK (binding_version > 0),
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','retrying','leased','sent','failed','expired','canceled')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 next_attempt_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 lease_token UUID,
 lease_until TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(rule_id,scheduled_date),
 FOREIGN KEY(rule_id,owner_id,scheduled_date) REFERENCES recurrence_occurrences(rule_id,owner_id,scheduled_date),
 CHECK ((status='leased' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR (status<>'leased' AND lease_token IS NULL AND lease_until IS NULL))
);
CREATE INDEX notification_ready_idx ON notification_outbox(next_attempt_at,id) WHERE status IN ('pending','retrying','leased');
CREATE INDEX notification_owner_idx ON notification_outbox(owner_id,created_at DESC,id DESC);
CREATE TABLE notification_cooldowns (
 scope TEXT PRIMARY KEY,
 until_at TIMESTAMPTZ NOT NULL
);
