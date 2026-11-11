-- Destructive to reset capabilities: do not run as an application rollback.
DELETE FROM email_tokens WHERE purpose='reset_password';
DROP TABLE email_reset_requests;
ALTER TABLE email_tokens DROP CONSTRAINT email_tokens_reset_version_check;
ALTER TABLE email_tokens DROP COLUMN credentials_version;
ALTER TABLE email_tokens DROP CONSTRAINT email_tokens_purpose_check;
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_purpose_check CHECK (purpose='verify_email');
