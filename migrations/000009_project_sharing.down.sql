-- Refuses rollback if shared tasks no longer satisfy the previous owner rule.
-- Membership/invitation data is lost on rollback; never run without review.
ALTER TABLE tasks ADD CONSTRAINT tasks_project_owner_fk FOREIGN KEY(owner_id,project_id) REFERENCES projects(owner_id,id) ON DELETE RESTRICT;
ALTER TABLE tasks DROP CONSTRAINT tasks_project_fk;
DROP TRIGGER project_owner_check ON projects;
DROP TRIGGER membership_owner_check ON project_memberships;
DROP FUNCTION check_project_owner_membership();
DROP TRIGGER membership_project_lock ON project_memberships;
DROP FUNCTION lock_membership_project();
DROP TABLE project_invitations;
DROP TABLE project_memberships;
