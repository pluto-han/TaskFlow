import { useCallback, useEffect, useRef, useState } from "react";
import { Modal } from "../components/Modal";
import { useAuth } from "../auth/AuthProvider";
import { sharingApi } from "../api/sharing";
import { errorMessage } from "../api/client";
import type { Project, Member, Invitation } from "../api/types";
import { displayDate } from "./dates";

type Action = { label: string; description: string; run(): Promise<void> };
export function SharingPanel({
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
      const result = await authorize((token) =>
        sharingApi.members(token, project.id, controller.signal),
      );
      if (controller.signal.aborted) return;
      setMembers(result);
      if (owner) {
        const invites = await authorize((token) =>
          sharingApi.invitations(token, project.id, controller.signal),
        );
        if (!controller.signal.aborted) setInvitations(invites);
      }
    } catch (err) {
      if (!controller.signal.aborted) setError(errorMessage(err));
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [authorize, owner, project.id]);
  useEffect(() => {
    // Fetch current membership; roles are never taken from the session token.
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
      const result = await authorize((token) =>
        sharingApi.invite(token, project.id, recipient.trim()),
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
      close();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="Members & invitations" busy={busy} close={close}>
      <p className="hint">
        {project.name} · {owner ? "You own this project" : "You are a member"}.
        Members can edit and delete any task in the project. Only a task’s
        creator can move it out.
      </p>
      {error && (
        <div className="notice error" role="alert">
          {error}
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
                          description: `Transfer ownership to ${member.user_id}? You will become a member, and all unused invitations will expire.`,
                          run: () =>
                            authorize((token) =>
                              sharingApi.transfer(
                                token,
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
                            authorize((token) =>
                              sharingApi.remove(
                                token,
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
                  description:
                    "You will lose access to this project, including tasks you created. Your tasks will stay in the project.",
                  run: () =>
                    authorize((token) => sharingApi.leave(token, project.id)),
                })
              }
            >
              Leave project
            </button>
          )}
          {owner && (
            <>
              <hr />
              <h3>Make room for a friend</h3>
              <p className="hint">
                Ask your friend to copy their account ID from their profile.
                This invitation is only for that account and lasts 24 hours.
                Sending another invitation replaces any unused link for them.
              </p>
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
                  <label htmlFor="invite-link">
                    Shown once. Share this link privately with the recipient.
                  </label>
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
                              authorize((token) =>
                                sharingApi.revoke(token, project.id, i.id),
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
    </Modal>
  );
}
