import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "../auth/AuthProvider";
import { sharingApi } from "../api/sharing";
import { ApiError, errorMessage } from "../api/client";
import type { Project, Member, Invitation } from "../api/types";
import { displayDate } from "../tasks/dates";

type Action = {
  label: string;
  description: string;
  exit?: boolean;
  run(): Promise<void>;
};
export function ProjectSettings({
  project,
  close,
  changed,
}: {
  project: Project;
  close(): void;
  changed(): void;
}) {
  const { user, authorize } = useAuth();
  const [members, setMembers] = useState<Member[]>([]),
    [invitations, setInvitations] = useState<Invitation[]>([]);
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [loading, setLoading] = useState(true);
  const [recipient, setRecipient] = useState(""),
    [link, setLink] = useState(""),
    [copied, setCopied] = useState(false);
  const [action, setAction] = useState<Action | null>(null);
  const request = useRef<AbortController | null>(null);
  const owner = project.role === "owner";
  const load = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    try {
      const result = await authorize((credentials) =>
        sharingApi.members(credentials, project.id, controller.signal),
      );
      if (controller.signal.aborted) return;
      setMembers(result);
      if (owner) {
        const invites = await authorize((credentials) =>
          sharingApi.invitations(credentials, project.id, controller.signal),
        );
        if (!controller.signal.aborted) setInvitations(invites);
      }
    } catch (err) {
      if (!controller.signal.aborted) {
        if (err instanceof ApiError && [403, 404].includes(err.status ?? 0)) {
          changed();
          close();
        } else setError(errorMessage(err));
      }
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [authorize, owner, project.id, changed, close]);
  useEffect(() => {
    // Fetch current membership; roles are never taken from the session credentials.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
    return () => request.current?.abort();
  }, [load]);
  async function invite() {
    setBusy(true);
    setError("");
    setLink("");
    setCopied(false);
    try {
      const result = await authorize((credentials) =>
        sharingApi.invite(credentials, project.id, recipient.trim()),
      );
      setLink(`${window.location.origin}/invite#token=${result.token}`);
      setRecipient("");
      await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  async function confirm() {
    if (!action || busy) return;
    setBusy(true);
    setError("");
    try {
      await action.run();
      setLink("");
      setAction(null);
      changed();
      if (action.exit) close();
      else await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="project-members" aria-labelledby="members-heading">
      <h2 id="members-heading">Members & invitations</h2>
      {error && (
        <div className="notice error" role="alert">
          {error}
          {!action && (
            <button
              className="text-button"
              disabled={busy}
              onClick={() => void load()}
            >
              Retry
            </button>
          )}
        </div>
      )}
      {action ? (
        <section className="notice">
          <h3>{action.label}</h3>
          <p>{action.description}</p>
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => {
                setAction(null);
                setError("");
              }}
            >
              Cancel action
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() => void confirm()}
            >
              Confirm {action.label.toLowerCase()}
            </button>
          </div>
        </section>
      ) : (
        <>
          {loading && <p role="status">Loading members…</p>}
          <ul className="member-list">
            {members.map((member) => (
              <li key={member.user_id}>
                <div>
                  <strong>
                    {member.role === "owner" ? "Owner" : "Member"}
                    {member.user_id === user?.id ? " (you)" : ""}
                  </strong>
                  <code>{member.user_id}</code>
                </div>
                {owner && member.role === "member" && (
                  <div className="row-actions">
                    <button
                      className="text-button"
                      disabled={busy}
                      onClick={() =>
                        setAction({
                          label: "Transfer ownership",
                          exit: true,
                          description: `Transfer ownership to ${member.user_id}? You will become a member, and all unused invitations will expire.`,
                          run: () =>
                            authorize((credentials) =>
                              sharingApi.transfer(
                                credentials,
                                project.id,
                                member.user_id,
                              ),
                            ),
                        })
                      }
                    >
                      Transfer ownership
                    </button>
                    <button
                      className="text-button danger-text"
                      disabled={busy}
                      onClick={() =>
                        setAction({
                          label: "Remove member",
                          description:
                            "This member will lose access to the project. Their tasks will stay in place.",
                          run: () =>
                            authorize((credentials) =>
                              sharingApi.remove(
                                credentials,
                                project.id,
                                member.user_id,
                              ),
                            ),
                        })
                      }
                    >
                      Remove member
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
          {!owner && (
            <button
              className="button secondary"
              disabled={busy}
              onClick={() =>
                setAction({
                  label: "Leave project",
                  exit: true,
                  description:
                    "You will lose access to this project, including tasks you created. Your tasks will stay in the project.",
                  run: () =>
                    authorize((credentials) =>
                      sharingApi.leave(credentials, project.id),
                    ),
                })
              }
            >
              Leave project
            </button>
          )}
          {owner && (
            <>
              <hr />
              <h3>Invite member</h3>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  if (!busy) void invite();
                }}
              >
                <div className="field">
                  <label htmlFor="invite-user">Recipient account ID</label>
                  <input
                    id="invite-user"
                    required
                    value={recipient}
                    onChange={(e) => setRecipient(e.target.value)}
                    disabled={busy}
                  />
                </div>
                <button className="button primary" disabled={busy}>
                  Create invitation link
                </button>
              </form>
              {link && (
                <div className="notice invitation-link">
                  <label htmlFor="invite-link">Invitation link</label>
                  <input
                    id="invite-link"
                    value={link}
                    readOnly
                    onFocus={(e) => e.target.select()}
                  />
                  <button
                    className="text-button"
                    onClick={() =>
                      void navigator.clipboard
                        .writeText(link)
                        .then(() => setCopied(true))
                        .catch(() =>
                          setError(
                            "We could not copy the link. Please select and copy it manually.",
                          ),
                        )
                    }
                  >
                    Copy invitation link
                  </button>
                  {copied && <span role="status">Copied</span>}
                </div>
              )}
              <h3>Invitations</h3>
              {invitations.length === 0 && (
                <p className="hint">No invitations yet.</p>
              )}
              <ul className="member-list">
                {invitations.map((i) => (
                  <li key={i.id}>
                    <div>
                      <code>{i.invitee_user_id}</code>
                      <small>
                        {i.accepted_at
                          ? "Accepted"
                          : i.revoked_at
                            ? "Revoked"
                            : `Expires ${displayDate(i.expires_at)}`}
                      </small>
                    </div>
                    {!i.accepted_at && !i.revoked_at && (
                      <button
                        className="text-button danger-text"
                        disabled={busy}
                        onClick={() =>
                          setAction({
                            label: "Revoke invitation",
                            description:
                              "This link will no longer let anyone join the project.",
                            run: () =>
                              authorize((credentials) =>
                                sharingApi.revoke(
                                  credentials,
                                  project.id,
                                  i.id,
                                ),
                              ),
                          })
                        }
                      >
                        Revoke invitation
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </section>
  );
}
