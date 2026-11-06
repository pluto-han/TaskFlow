-- Destructive: pending and historical notification events are lost.
DROP TABLE notification_cooldowns;
DROP TABLE notification_outbox;
ALTER TABLE recurrence_rules DROP COLUMN reminder_enabled;
