CREATE TABLE project_memberships (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id),
    role TEXT NOT NULL DEFAULT 'member' CHECK (role = 'member'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_memberships_user_idx ON project_memberships(user_id, project_id);
CREATE TABLE project_invitations (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    invitee_user_id UUID NOT NULL REFERENCES users(id),
    created_by UUID NOT NULL REFERENCES users(id),
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    CHECK (NOT (revoked_at IS NOT NULL AND accepted_at IS NOT NULL))
);
CREATE INDEX project_invitations_project_idx ON project_invitations(project_id, created_at DESC);
-- Owner remains authoritative in projects.owner_id, never a duplicate member.
CREATE FUNCTION check_project_owner_membership() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME = 'projects' THEN target := NEW.id; ELSE target := NEW.project_id; END IF;
    IF EXISTS (SELECT 1 FROM projects p JOIN project_memberships m
               ON m.project_id=p.id AND m.user_id=p.owner_id WHERE p.id=target) THEN
        RAISE EXCEPTION 'Owner cannot also be a member' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER membership_owner_check
AFTER INSERT OR UPDATE ON project_memberships DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_project_owner_membership();
CREATE CONSTRAINT TRIGGER project_owner_check
AFTER INSERT OR UPDATE ON projects DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION check_project_owner_membership();
ALTER TABLE tasks DROP CONSTRAINT tasks_project_owner_fk;
ALTER TABLE tasks ADD CONSTRAINT tasks_project_fk FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE RESTRICT;

-- Serialize even direct membership writes with project ownership changes.
CREATE FUNCTION lock_membership_project() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM id FROM projects WHERE id=NEW.project_id FOR UPDATE;
    RETURN NEW;
END;
$$;
CREATE TRIGGER membership_project_lock BEFORE INSERT OR UPDATE ON project_memberships
FOR EACH ROW EXECUTE FUNCTION lock_membership_project();
