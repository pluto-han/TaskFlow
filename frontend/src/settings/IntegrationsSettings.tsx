import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "../auth/AuthProvider";
import { discordApi } from "../api/discord";
import { errorMessage } from "../api/client";
import type { components } from "../api/schema";
import { Modal } from "../components/Modal";
type Binding = components["schemas"]["DiscordBinding"];
type Code = components["schemas"]["DiscordBindingCode"];
type Notification = components["schemas"]["NotificationEvent"];
export function IntegrationsSettings() {
  const { authorize } = useAuth();
  const [historyOpen, setHistoryOpen] = useState(false);
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [notificationsError, setNotificationsError] = useState("");
  const [notificationsLoading, setNotificationsLoading] = useState(false);
  const [binding, setBinding] = useState<Binding | null>(null);
  const [code, setCode] = useState<Code | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [copy, setCopy] = useState("");
  const request = useRef<AbortController | null>(null);
  const active = useRef(true);
  const historyRequest = useRef<AbortController | null>(null);
  const load = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    try {
      const result = await authorize((credentials) =>
        discordApi.status(credentials, controller.signal),
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
      historyRequest.current?.abort();
    };
  }, [load]);
  const loadNotifications = useCallback(async () => {
    historyRequest.current?.abort();
    const controller = new AbortController();
    historyRequest.current = controller;
    setNotificationsLoading(true);
    try {
      const data = await authorize((credentials) =>
        discordApi.notifications(credentials, controller.signal),
      );
      if (active.current && !controller.signal.aborted) {
        setNotifications(data);
        setNotificationsError("");
      }
    } catch {
      if (active.current && !controller.signal.aborted)
        setNotificationsError("We could not load your reminders just now.");
    } finally {
      if (active.current && !controller.signal.aborted)
        setNotificationsLoading(false);
    }
  }, [authorize]);
  useEffect(() => {
    // Load the signed-in user's notification history.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (historyOpen) void loadNotifications();
    return () => historyRequest.current?.abort();
  }, [loadNotifications, historyOpen]);
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
      const result = await authorize((credentials) =>
        discordApi.issue(credentials),
      );
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
      await authorize((credentials) =>
        discordApi.unbind(credentials, binding.version),
      );
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
      className="settings-content discord-settings"
      aria-labelledby="discord-heading"
      aria-busy={busy}
    >
      <h1 id="discord-heading">Integrations</h1>
      <h2 className="section-title">Discord</h2>

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
      {code && (
        <div
          className="notice"
          role="region"
          aria-label="One-time connection code"
        >
          <code className="binding-command">
            /taskflow bind code:{code.code}
          </code>
          <p className="hint">
            Expires {new Date(code.expires_at).toLocaleTimeString()}
          </p>
          <button
            className="text-button"
            onClick={() =>
              void navigator.clipboard
                .writeText(`/taskflow bind code:${code.code}`)
                .then(() => setCopy("Command copied"))
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
      <button
        className="button secondary"
        aria-expanded={historyOpen}
        onClick={() => setHistoryOpen(!historyOpen)}
      >
        Recent reminders
      </button>
      {historyOpen && (
        <div className="notification-history">
          <button
            className="text-button"
            disabled={notificationsLoading}
            onClick={() => void loadNotifications()}
          >
            Refresh reminders
          </button>
          {notificationsLoading && <p role="status">Loading reminders…</p>}
          {notificationsError && <p role="alert">{notificationsError}</p>}
          {!notificationsLoading &&
            !notificationsError &&
            notifications.length === 0 && (
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
                            "The bot credentials is invalid. Please check the configuration.",
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
      )}
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
