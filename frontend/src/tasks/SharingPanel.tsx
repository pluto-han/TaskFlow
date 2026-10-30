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
    <Modal title="成员与邀请" busy={busy} close={close}>
      <p className="hint">
        {project.name} · {owner ? "你是所有者" : "你是成员"}
        。成员可编辑和删除项目内的所有任务；移走任务仅限创建者。
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
              取消操作
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() => void confirm()}
            >
              确认{action.label}
            </button>
          </div>
        </section>
      ) : (
        <>
          {loading && <p role="status">正在加载成员…</p>}
          <ul className="member-list">
            {members.map((member) => (
              <li key={member.user_id}>
                <div>
                  <strong>
                    {member.role === "owner" ? "所有者" : "成员"}
                    {member.user_id === user?.id ? "（你）" : ""}
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
                          label: "转移所有权",
                          description: `转移给 ${member.user_id} 后，你将变为普通成员，未使用的邀请全部失效。`,
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
                      转移所有权
                    </button>
                    <button
                      className="text-button danger-text"
                      disabled={busy}
                      onClick={() =>
                        setAction({
                          label: "移除成员",
                          description:
                            "此成员立即失去后续项目访问权，其项目任务仍保留。",
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
                      移除成员
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
                  label: "退出项目",
                  description:
                    "退出后不能再访问此项目，即使任务由你创建。任务会保留在项目中。",
                  run: () =>
                    authorize((token) => sharingApi.leave(token, project.id)),
                })
              }
            >
              退出项目
            </button>
          )}
          {owner && (
            <>
              <hr />
              <h3>邀请已注册的朋友</h3>
              <p className="hint">
                请朋友从个人资料复制账号 ID。邀请仅此账号可用，有效期 24
                小时；重复邀请会使之前未使用的链接失效。
              </p>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  if (!busy) void invite();
                }}
              >
                <div className="field">
                  <label htmlFor="invite-user">接收者账号 ID</label>
                  <input
                    id="invite-user"
                    required
                    value={recipient}
                    onChange={(e) => setRecipient(e.target.value)}
                    disabled={busy}
                  />
                </div>
                <button className="button primary" disabled={busy}>
                  生成邀请链接
                </button>
              </form>
              {link && (
                <div className="notice invitation-link">
                  <label htmlFor="invite-link">
                    仅本次显示，请手工分享给接收者
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
                          setError("无法自动复制，请选中链接手工复制。"),
                        )
                    }
                  >
                    复制邀请链接
                  </button>
                  {copied && <span role="status">已复制</span>}
                </div>
              )}
              <h3>邀请记录</h3>
              {invitations.length === 0 && <p className="hint">还没有邀请。</p>}
              <ul className="member-list">
                {invitations.map((i) => (
                  <li key={i.id}>
                    <div>
                      <code>{i.invitee_user_id}</code>
                      <small>
                        {i.accepted_at
                          ? "已接受"
                          : i.revoked_at
                            ? "已撤销"
                            : `有效至 ${displayDate(i.expires_at)}`}
                      </small>
                    </div>
                    {!i.accepted_at && !i.revoked_at && (
                      <button
                        className="text-button danger-text"
                        disabled={busy}
                        onClick={() =>
                          setAction({
                            label: "撤销邀请",
                            description: "撤销后，该链接无法再用于加入项目。",
                            run: () =>
                              authorize((token) =>
                                sharingApi.revoke(token, project.id, i.id),
                              ),
                          })
                        }
                      >
                        撤销邀请
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
