import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "../auth/AuthProvider";
import { discordApi } from "../api/discord";
import { errorMessage } from "../api/client";
import type { components } from "../api/schema";
import { Modal } from "../components/Modal";
type Binding = components["schemas"]["DiscordBinding"];
type Code = components["schemas"]["DiscordBindingCode"];
type Notification = components["schemas"]["NotificationEvent"];
export function DiscordSettings() {
  const { authorize } = useAuth();
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [notificationsError, setNotificationsError] = useState("");
  const [binding, setBinding] = useState<Binding | null>(null);
  const [code, setCode] = useState<Code | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [copy, setCopy] = useState("");
  const request = useRef<AbortController | null>(null);
  const active = useRef(true);
  const load = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    try {
      const result = await authorize((token) =>
        discordApi.status(token, controller.signal),
      );
      if (controller.signal.aborted) return;
      setBinding(result);
      setError("");
      if (result.bound) setCode(null);
    } catch (err) {
      if (!controller.signal.aborted) setError(errorMessage(err));
    }
  }, [authorize]);
  useEffect(() => {
    active.current = true;
    // Synchronize private account settings with the server.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
    return () => {
      active.current = false;
      request.current?.abort();
    };
  }, [load]);
  const loadNotifications = useCallback(async () => {
    try {
      const data = await authorize((token) => discordApi.notifications(token));
      if (active.current) {
        setNotifications(data);
        setNotificationsError("");
      }
    } catch {
      if (active.current) setNotificationsError("暂时无法读取提醒记录。");
    }
  }, [authorize]);
  useEffect(() => {
    // Load the signed-in user's notification history.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadNotifications();
  }, [loadNotifications]);
  useEffect(() => {
    if (!code) return;
    const timer = setTimeout(
      () => setCode(null),
      Math.max(0, Date.parse(code.expires_at) - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [code]);
  async function issue() {
    if (busy) return;
    setBusy(true);
    setError("");
    setCode(null);
    setCopy("");
    try {
      const result = await authorize((token) => discordApi.issue(token));
      if (active.current) setCode(result);
    } catch (err) {
      if (active.current) setError(errorMessage(err));
    } finally {
      if (active.current) setBusy(false);
    }
  }
  async function unbind() {
    if (!binding || busy) return;
    setBusy(true);
    setError("");
    try {
      await authorize((token) => discordApi.unbind(token, binding.version));
      if (!active.current) return;
      setCode(null);
      setConfirm(false);
      await load();
    } catch (err) {
      if (active.current) setError(errorMessage(err));
    } finally {
      if (active.current) setBusy(false);
    }
  }
  return (
    <section
      className="profile-card discord-settings"
      aria-labelledby="discord-heading"
      aria-busy={busy}
    >
      <h2 id="discord-heading">Discord 频道</h2>
      <p className="hint">
        以后启用循环提醒后，任务信息将发送到这个频道，所有能查看频道的成员都可能看到。请只绑定你有权管理的频道。
      </p>
      <p className="hint">
        循环规则和发送服务均开启后，提醒会 @ 完成频道绑定的 Discord 用户。如果是他人代你绑定，请由你重新绑定频道。
      </p>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      {!binding && !error && <p role="status">正在读取绑定状态…</p>}
      {binding && (
        <dl className="details">
          <div className="detail">
            <dt>状态</dt>
            <dd>
              {binding.bound ? "已绑定" : "未绑定"}
              {!binding.enabled && " · 服务尚未配置"}
            </dd>
          </div>
          {binding.bound && (
            <>
              <div className="detail">
                <dt>频道</dt>
                <dd>
                  #{binding.channel_name} <code>{binding.channel_id}</code>
                </dd>
              </div>
              <div className="detail">
                <dt>服务器 ID</dt>
                <dd>
                  <code>{binding.guild_id}</code>
                </dd>
              </div>
            </>
          )}
        </dl>
      )}
      {binding && !binding.enabled && (
        <p className="hint">
          请先配置 Discord Application ID 和公钥，然后重启后端。具体步骤见项目的
          docs/lesson-33-discord-setup.md。
        </p>
      )}
      {code && (
        <div className="notice" role="region" aria-label="一次性绑定码">
          <p>在目标服务器的普通文字频道执行：</p>
          <code className="binding-command">
            /taskflow bind code:{code.code}
          </code>
          <p className="hint">
            10
            分钟内有效，只能使用一次。请勿分享、截图或粘贴到普通聊天消息；重新生成会撤销旧码。
          </p>
          <button
            className="text-button"
            onClick={() =>
              void navigator.clipboard
                .writeText(`/taskflow bind code:${code.code}`)
                .then(() =>
                  setCopy("命令已复制，请粘贴到 Discord 的斜杠命令框。"),
                )
                .catch(() => setCopy("请手工复制命令。"))
            }
          >
            复制绑定命令
          </button>
          <span role="status">{copy}</span>
        </div>
      )}
      <div className="actions-buttons">
        <button
          className="button secondary"
          disabled={busy}
          onClick={() => void load()}
        >
          刷新绑定状态
        </button>
        <button
          className="button primary"
          disabled={busy || !binding?.enabled}
          onClick={() => void issue()}
        >
          {code
            ? "重新生成绑定码"
            : binding?.bound
              ? "更换绑定频道"
              : "生成绑定码"}
        </button>
        {binding && (binding.bound || code) && (
          <button
            className="text-button danger-text"
            disabled={busy}
            onClick={() => setConfirm(true)}
          >
            解绑并撤销绑定码
          </button>
        )}
      </div>
      <div className="notification-history">
        <h3>最近提醒</h3>
        <button
          className="text-button"
          onClick={() => void loadNotifications()}
        >
          刷新提醒记录
        </button>
        {notificationsError && <p role="status">{notificationsError}</p>}
        {!notificationsError && notifications.length === 0 && (
          <p className="hint">暂无提醒记录。</p>
        )}
        <ul className="task-list">
          {notifications.map((item) => (
            <li key={item.id} className="task-row">
              <div className="task-content">
                <h4>{item.title}</h4>
                <div className="task-meta">
                  <span>{item.scheduled_date}</span>
                  <span>
                    {
                      (
                        {
                          pending: "等待发送",
                          retrying: "等待重试",
                          leased: "发送中",
                          sent: "已发送",
                          failed: "发送失败",
                          expired: "已过期",
                          canceled: "已取消",
                        } as const
                      )[item.status]
                    }
                  </span>
                </div>
                {item.last_error && (
                  <p className="hint">
                    {(
                      {
                        network: "网络异常",
                        rate_limited: "Discord 暂时限流",
                        discord_unavailable: "Discord 暂时不可用",
                        discord_token_invalid: "Bot Token 无效，请检查配置",
                        discord_permission: "Bot 没有频道权限",
                        discord_channel_missing: "频道不可用",
                        discord_rejected: "Discord 拒绝了请求",
                        attempts_exhausted: "重试次数已用完",
                      } as Record<string, string>
                    )[item.last_error] || "发送未完成"}
                  </p>
                )}
              </div>
            </li>
          ))}
        </ul>
      </div>
      {confirm && (
        <Modal title="解绑 Discord" busy={busy} close={() => setConfirm(false)}>
          <p>解绑当前频道并撤销未使用的绑定码？任务与历史会保留。</p>
          {error && (
            <div role="alert" className="notice error">
              {error}
            </div>
          )}
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setConfirm(false)}
            >
              取消
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() => void unbind()}
            >
              确认解绑
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
