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
      if (active.current)
        setNotificationsError("We could not load your reminders just now.");
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
      <h2 id="discord-heading">Discord channel</h2>
      <p className="hint">
        Recurring task reminders will appear in this channel, where anyone with
        access can see them. Only connect a channel you have permission to
        manage.
      </p>
      <p className="hint">
        When reminders and delivery are enabled, messages will mention the
        Discord account that connected this channel. If someone connected it for
        you, reconnect it yourself to receive the mentions.
      </p>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      {!binding && !error && <p role="status">Checking your connection…</p>}
      {binding && (
        <dl className="details">
          <div className="detail">
            <dt>Status</dt>
            <dd>
              {binding.bound ? "Connected" : "Not connected"}
              {!binding.enabled && " · Not set up yet"}
            </dd>
          </div>
          {binding.bound && (
            <>
              <div className="detail">
                <dt>Channel</dt>
                <dd>
                  #{binding.channel_name} <code>{binding.channel_id}</code>
                </dd>
              </div>
              <div className="detail">
                <dt>Server ID</dt>
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
          Ask the server administrator to configure the Discord Application ID
          and Public Key, then restart the service. Setup instructions are in{" "}
          <code>docs/lesson-33-discord-setup.md</code>.
        </p>
      )}
      {code && (
        <div
          className="notice"
          role="region"
          aria-label="One-time connection code"
        >
          <p>
            Run this command in a regular text channel on your chosen server:
          </p>
          <code className="binding-command">
            /taskflow bind code:{code.code}
          </code>
          <p className="hint">
            Valid for 10 minutes and one use only. Keep it private: do not share
            it, take screenshots, or post it as a regular chat message. A new
            code replaces the old one.
          </p>
          <button
            className="text-button"
            onClick={() =>
              void navigator.clipboard
                .writeText(`/taskflow bind code:${code.code}`)
                .then(() =>
                  setCopy(
                    "Command copied. Paste it into Discord’s slash command field.",
                  ),
                )
                .catch(() => setCopy("Please copy the command manually."))
            }
          >
            Copy connection command
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
          Refresh connection
        </button>
        <button
          className="button primary"
          disabled={busy || !binding?.enabled}
          onClick={() => void issue()}
        >
          {code
            ? "Generate a new code"
            : binding?.bound
              ? "Change channel"
              : "Generate connection code"}
        </button>
        {binding && (binding.bound || code) && (
          <button
            className="text-button danger-text"
            disabled={busy}
            onClick={() => setConfirm(true)}
          >
            Disconnect and revoke codes
          </button>
        )}
      </div>
      <div className="notification-history">
        <h3>Recent reminders</h3>
        <button
          className="text-button"
          onClick={() => void loadNotifications()}
        >
          Refresh reminders
        </button>
        {notificationsError && <p role="status">{notificationsError}</p>}
        {!notificationsError && notifications.length === 0 && (
          <p className="hint">No reminders yet.</p>
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
                          pending: "Waiting to send",
                          retrying: "Waiting to retry",
                          leased: "Sending",
                          sent: "Sent",
                          failed: "Delivery failed",
                          expired: "Expired",
                          canceled: "Canceled",
                        } as const
                      )[item.status]
                    }
                  </span>
                </div>
                {item.last_error && (
                  <p className="hint">
                    {(
                      {
                        network: "Network error",
                        rate_limited: "Discord is limiting requests",
                        discord_unavailable:
                          "Discord is temporarily unavailable",
                        discord_token_invalid:
                          "The bot token is invalid. Please check the configuration.",
                        discord_permission:
                          "The bot does not have access to this channel",
                        discord_channel_missing: "Channel unavailable",
                        discord_rejected: "Discord declined the request",
                        attempts_exhausted: "No retry attempts left",
                      } as Record<string, string>
                    )[item.last_error] || "Delivery did not complete"}
                  </p>
                )}
              </div>
            </li>
          ))}
        </ul>
      </div>
      {confirm && (
        <Modal
          title="Disconnect Discord"
          busy={busy}
          close={() => setConfirm(false)}
        >
          <p>
            Disconnect this channel and revoke unused connection codes? Your
            tasks and history will stay.
          </p>
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
              Cancel
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() => void unbind()}
            >
              Confirm disconnect
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
