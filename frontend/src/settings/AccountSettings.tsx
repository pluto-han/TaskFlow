import { useCallback, useEffect, useRef, useState } from "react";
import { accountApi, type EmailStatus } from "../api/account";
import { ApiError, errorMessage } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { displayDate } from "../tasks/dates";

export function AccountSettings() {
  const auth = useAuth();
  const { authorize } = auth;
  const [copyMessage, setCopyMessage] = useState("");
  const [status, setStatus] = useState<EmailStatus | null>(null);
  const [emailError, setEmailError] = useState("");
  const [sent, setSent] = useState<number | null>(null);
  const [cooling, setCooling] = useState(false);
  const [sending, setSending] = useState(false);
  const emailRequest = useRef<AbortController | null>(null);
  const actionRequest = useRef<AbortController | null>(null);
  const locked = useRef(false);
  const loadEmail = useCallback(async () => {
    emailRequest.current?.abort();
    const controller = new AbortController();
    emailRequest.current = controller;
    try {
      const value = await authorize((credentials) =>
        accountApi.status(credentials, controller.signal),
      );
      if (!controller.signal.aborted) {
        setStatus(value);
        setEmailError("");
      }
    } catch (err) {
      if (!controller.signal.aborted) setEmailError(errorMessage(err));
    }
  }, [authorize]);
  useEffect(() => {
    // Synchronize email verification without loading unrelated settings.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadEmail();
    return () => {
      emailRequest.current?.abort();
      actionRequest.current?.abort();
    };
  }, [loadEmail]);
  useEffect(() => {
    if (sent === null) return;
    const timer = window.setTimeout(
      () => setCooling(false),
      Math.max(0, 60_000 - (Date.now() - sent)),
    );
    return () => window.clearTimeout(timer);
  }, [sent]);
  async function send() {
    if (locked.current) return;
    locked.current = true;
    setSending(true);
    setEmailError("");
    const controller = new AbortController();
    actionRequest.current = controller;
    try {
      await authorize((credentials) =>
        accountApi.verify(credentials, controller.signal),
      );
      if (!controller.signal.aborted) {
        setSent(Date.now());
        setCooling(true);
      }
    } catch (err) {
      if (!controller.signal.aborted) {
        setEmailError(errorMessage(err));
        if (err instanceof ApiError && err.code === "EMAIL_DISABLED")
          setStatus((old) => (old ? { ...old, enabled: false } : old));
      }
    } finally {
      if (!controller.signal.aborted) {
        setSending(false);
        locked.current = false;
      }
    }
  }
  if (!auth.user) return null;
  return (
    <section className="settings-content" aria-labelledby="account-heading">
      <h1 id="account-heading">Account</h1>
      {auth.error && (
        <p className="notice error" role="alert">
          {auth.error}
        </p>
      )}
      <div className="settings-fields">
        <div className="settings-row">
          <h2>Email address</h2>
          <div>
            {auth.user.email}{" "}
            <span className="badge">
              {status
                ? status.verified_at
                  ? "Verified"
                  : "Not verified"
                : "Checking…"}
            </span>
            {emailError && (
              <div className="notice error" role="alert">
                {emailError}
                {!status && (
                  <button
                    className="text-button"
                    onClick={() => void loadEmail()}
                  >
                    Try again
                  </button>
                )}
              </div>
            )}
            {status?.verified_at ? null : status ? (
              <div className="security-row">
                {status.enabled && (
                  <button
                    className="button secondary"
                    disabled={sending || cooling}
                    onClick={() => void send()}
                  >
                    {sending ? (
                      <>Requesting…</>
                    ) : cooling ? (
                      "Email requested"
                    ) : sent !== null ? (
                      "Resend verification email"
                    ) : (
                      "Send verification email"
                    )}
                  </button>
                )}
              </div>
            ) : (
              !emailError && (
                <p className="security-description" role="status">
                  Checking email verification…
                </p>
              )
            )}
            {status && !status.enabled && (
              <p className="hint">Email delivery unavailable</p>
            )}
            {status && (
              <button
                className="text-button security-refresh"
                onClick={() => void loadEmail()}
              >
                Refresh email status
              </button>
            )}
          </div>
        </div>
        <div className="settings-row">
          <h2>Member since</h2>
          <p>{displayDate(auth.user.created_at)}</p>
        </div>
        <div className="settings-row">
          <h2>Account ID</h2>
          <code>{auth.user.id}</code>
          <button
            className="button secondary"
            onClick={() =>
              void navigator.clipboard
                .writeText(auth.user!.id)
                .then(() => setCopyMessage("Account ID copied"))
                .catch(() =>
                  setCopyMessage("Copy failed. Select the account ID to copy."),
                )
            }
          >
            Copy account ID
          </button>
        </div>
        <p role="status">{copyMessage}</p>
        <div className="settings-row">
          <h2>Sign out</h2>
          <div className="actions-buttons">
            <button
              className="button secondary"
              disabled={auth.busy}
              onClick={() => void auth.refresh()}
            >
              Refresh profile
            </button>
            <button
              className="button danger"
              disabled={auth.busy}
              onClick={() => void auth.logout()}
            >
              Sign out
            </button>
          </div>
        </div>
      </div>
    </section>
  );
}
