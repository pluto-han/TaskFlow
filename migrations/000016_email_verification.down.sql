DROP TABLE email_outbox;
DROP TABLE email_tokens;
ALTER TABLE users DROP COLUMN email_verified_at;
