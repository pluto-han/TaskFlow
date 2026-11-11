import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { Link } from "react-router";
import {
  accountApi,
  type EmailStatus,
  type SessionPage,
  type SessionInfo,
} from "../api/account";
import { ApiError, errorMessage } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { validate, type FieldErrors } from "../auth/validation";
import { Modal } from "../components/Modal";
import { PasswordFields } from "../components/PasswordFields";

const date = (value: string) =>
  new Intl.DateTimeFormat("en-GB", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
function SecurityIcon({ kind }: { kind: "mail" | "lock" | "sessions" }) {
  return (
    <span className="security-icon" aria-hidden="true">
      <svg
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        {kind === "mail" ? (
          <>
            <rect x="3" y="5" width="18" height="14" rx="3" />
            <path d="m4 7 8 6 8-6" />
          </>
        ) : kind === "lock" ? (
          <>
            <rect x="5" y="10" width="14" height="11" rx="3" />
            <path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3" />
          </>
        ) : (
          <>
            <rect x="3" y="4" width="18" height="13" rx="2" />
            <path d="M8 21h8m-4-4v4" />
          </>
        )}
      </svg>
    </span>
  );
}
function PasswordChange() {
  const auth = useAuth();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errors, setErrors] = useState<FieldErrors>({});
  const [currentError, setCurrentError] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const pending = useRef<AbortController | null>(null);
  const locked = useRef(false);
  const form = useRef<HTMLFormElement>(null);
  useEffect(() => () => pending.current?.abort(), []);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (locked.current) return;
    const fields = validate("valid@example.com", password, confirm);
    setErrors(fields);
    setCurrentError(current ? "" : "Enter your current password.");
    setError("");
    const first = !current ? "current" : Object.keys(fields)[0];
    if (first) {
      form.current
        ?.querySelector<HTMLInputElement>(`[name="${first}"]`)
        ?.focus();
      return;
    }
    locked.current = true;
    setBusy(true);
    const controller = new AbortController();
    pending.current = controller;
    try {
      await auth.authorize((credentials) =>
        accountApi.changePassword(
          credentials,
          { current_password: current, new_password: password },
          controller.signal,
        ),
      );
      if (!controller.signal.aborted)
        auth.endSession(
          "Your password has been changed. Sign in again with your new password.",
        );
    } catch (err) {
      if (!controller.signal.aborted) {
        if (
          err instanceof ApiError &&
          err.code === "CURRENT_PASSWORD_INCORRECT"
        ) {
          setCurrentError(errorMessage(err));
          form.current
            ?.querySelector<HTMLInputElement>('[name="current"]')
            ?.focus();
        } else setError(errorMessage(err));
      }
    } finally {
      if (!controller.signal.aborted) {
        setBusy(false);
        locked.current = false;
      }
    }
  }
  return (
    <details className="password-disclosure">
      <summary>Change password</summary>
      <form
        ref={form}
        onSubmit={(event) => void submit(event)}
        noValidate
        aria-busy={busy}
      >
        <p className="security-warning">
          Changing your password signs you out of all sessions, including this
          one.
        </p>
        {error && (
          <p className="notice error" role="alert">
            {error}
          </p>
        )}
        <div className="field">
          <label htmlFor="change-current">Current password</label>
          <input
            id="change-current"
            name="current"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            required
            disabled={busy}
            aria-invalid={!!currentError}
            aria-describedby={currentError ? "change-current-error" : undefined}
          />
          {currentError && (
            <p className="field-error" id="change-current-error">
              {currentError}
            </p>
          )}
        </div>
        <PasswordFields
          prefix="change"
          password={password}
          confirm={confirm}
          onPassword={setPassword}
          onConfirm={setConfirm}
          errors={errors}
          busy={busy}
        />
        <div className="security-form-actions">
          <Link to="/forgot-password">Forgot your password?</Link>
          <button className="button primary" type="submit" disabled={busy}>
            {busy ? (
              <>
                <span className="spinner" aria-hidden="true" />
                Updating…
              </>
            ) : (
              "Update password"
            )}
          </button>
        </div>
      </form>
    </details>
  );
}
export function AccountSettings() {
  const auth = useAuth();
  const { authorize } = auth;
  const [status, setStatus] = useState<EmailStatus | null>(null);
  const [emailError, setEmailError] = useState("");
  const [sent, setSent] = useState<number | null>(null);
  const [cooling, setCooling] = useState(false);
  const [sending, setSending] = useState(false);
  const [page, setPage] = useState<SessionPage>({ items: [], next_cursor: "" });
  const [loading, setLoading] = useState(true);
  const [sessionsError, setSessionsError] = useState("");
  const [notice, setNotice] = useState("");
  const [selected, setSelected] = useState<SessionInfo | null>(null);
  const [revoking, setRevoking] = useState(false);
  const emailRequest = useRef<AbortController | null>(null);
  const listRequest = useRef<AbortController | null>(null);
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
  const loadSessions = useCallback(
    async (cursor = "") => {
      listRequest.current?.abort();
      const controller = new AbortController();
      listRequest.current = controller;
      setLoading(true);
      setSessionsError("");
      try {
        const value = await authorize((credentials) =>
          accountApi.sessions(credentials, cursor, controller.signal),
        );
        if (!controller.signal.aborted)
          setPage((old) =>
            cursor
              ? {
                  items: [...old.items, ...value.items],
                  next_cursor: value.next_cursor,
                }
              : value,
          );
      } catch (err) {
        if (!controller.signal.aborted) setSessionsError(errorMessage(err));
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    },
    [authorize],
  );
  useEffect(() => {
    // Synchronize private panels with the server on mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void Promise.all([loadEmail(), loadSessions()]);
    return () => {
      emailRequest.current?.abort();
      listRequest.current?.abort();
      actionRequest.current?.abort();
    };
  }, [loadEmail, loadSessions]);
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
  async function revoke() {
    if (!selected || locked.current) return;
    locked.current = true;
    setRevoking(true);
    setSessionsError("");
    const controller = new AbortController();
    actionRequest.current = controller;
    try {
      await authorize((credentials) =>
        accountApi.revoke(credentials, selected.id, controller.signal),
      );
      if (controller.signal.aborted) return;
      if (selected.current)
        auth.endSession("This session has ended. Sign in again to continue.");
      else {
        setSelected(null);
        setNotice("Session signed out.");
        await loadSessions();
      }
    } catch (err) {
      if (!controller.signal.aborted) setSessionsError(errorMessage(err));
    } finally {
      if (!controller.signal.aborted) {
        setRevoking(false);
        locked.current = false;
      }
    }
  }
  return (
    <section
      className="account-security profile-card"
      aria-labelledby="security-heading"
    >
      <header className="security-heading">
        <div>
          <span className="eyebrow">ACCOUNT SETTINGS</span>
          <h2 id="security-heading">Sign-in & security</h2>
          <p>Keep your account protected and your access up to date.</p>
        </div>
      </header>
      <section
        className="security-section"
        aria-labelledby="email-security-heading"
      >
        <div className="security-section-heading">
          <SecurityIcon kind="mail" />
          <div>
            <h3 id="email-security-heading">Email address</h3>
            <p>{auth.user?.email}</p>
          </div>
          <span
            className={`security-badge ${status?.verified_at ? "verified" : ""}`}
          >
            {status
              ? status.verified_at
                ? "Verified"
                : "Not verified"
              : "Checking…"}
          </span>
        </div>
        {emailError && (
          <div className="notice error" role="alert">
            {emailError}
            {!status && (
              <button className="text-button" onClick={() => void loadEmail()}>
                Try again
              </button>
            )}
          </div>
        )}
        {status?.verified_at ? (
          <p className="security-description">
            Verified on {date(status.verified_at)}.{" "}
            {status.enabled
              ? "Your email address is confirmed."
              : "Email delivery is currently unavailable."}
          </p>
        ) : status ? (
          <div className="security-row">
            <p className="security-description">
              {!status.enabled
                ? "Email delivery is not available on this instance. Your existing sign-in still works."
                : sent !== null
                  ? "Verification requested. Check your inbox and spam folder; the link is valid for 24 hours."
                  : "Confirm that this email address belongs to you."}
            </p>
            {status.enabled && (
              <button
                className="button secondary"
                disabled={sending || cooling || revoking}
                onClick={() => void send()}
              >
                {sending ? (
                  <>
                    <span className="spinner" aria-hidden="true" />
                    Requesting…
                  </>
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
        {status && (
          <button
            className="text-button security-refresh"
            onClick={() => void loadEmail()}
          >
            Refresh email status
          </button>
        )}
      </section>
      <section
        className="security-section"
        aria-labelledby="password-security-heading"
      >
        <div className="security-section-heading">
          <SecurityIcon kind="lock" />
          <div>
            <h3 id="password-security-heading">Password</h3>
            <p>Use a unique password for your account.</p>
          </div>
        </div>
        <PasswordChange />
      </section>
      <section className="security-section" aria-labelledby="sessions-heading">
        <div className="security-section-heading">
          <SecurityIcon kind="sessions" />
          <div>
            <h3 id="sessions-heading">Active sessions</h3>
            <p>Review where your account is signed in.</p>
          </div>
          <button
            className="text-button"
            disabled={loading || revoking}
            onClick={() => void loadSessions()}
          >
            Refresh sessions
          </button>
        </div>
        <p className="security-description">
          Each sign-in creates a session. Tabs in the same browser share one.
          Device and location details are not collected.
        </p>
        {notice && (
          <p className="notice" role="status">
            {notice}
          </p>
        )}
        {sessionsError && !selected && (
          <p className="notice error" role="alert">
            {sessionsError}
          </p>
        )}
        {loading && (
          <p className="security-description" role="status">
            Loading sessions…
          </p>
        )}
        <ul className="session-list" aria-label="Active sessions">
          {page.items.map((item) => (
            <li key={item.id}>
              <div className="session-description">
                <strong>
                  {item.current
                    ? "This browser"
                    : item.credential_kind === "browser"
                      ? "Browser session"
                      : "API session"}
                </strong>
                {item.current && (
                  <span className="security-badge verified">
                    Current session
                  </span>
                )}
                <p>Signed in {date(item.created_at)}</p>
                <p>Expires {date(item.expires_at)}</p>
              </div>
              <button
                className="button secondary session-revoke"
                disabled={revoking || sending}
                onClick={() => {
                  setSelected(item);
                  setSessionsError("");
                }}
                aria-label={
                  item.current
                    ? "End current session"
                    : `End session signed in ${date(item.created_at)}`
                }
              >
                Sign out
              </button>
            </li>
          ))}
        </ul>
        {!loading && !sessionsError && page.items.length === 0 && (
          <p className="security-description">
            No active sessions found. Refresh to check again.
          </p>
        )}
        {page.next_cursor && (
          <button
            className="button secondary"
            disabled={loading || revoking}
            onClick={() => void loadSessions(page.next_cursor)}
          >
            Load more sessions
          </button>
        )}
      </section>
      {selected && (
        <Modal
          title={
            selected.current ? "End this session?" : "Sign out this session?"
          }
          busy={revoking}
          close={() => setSelected(null)}
        >
          <p>
            {selected.current
              ? "You will be signed out of this browser. Your tasks will remain saved."
              : "This session will lose access to your account. Your current browser and saved tasks will stay available."}
          </p>
          {sessionsError && (
            <p className="notice error" role="alert">
              {sessionsError}
            </p>
          )}
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={revoking}
              onClick={() => setSelected(null)}
            >
              Cancel
            </button>
            <button
              className="button danger"
              disabled={revoking}
              onClick={() => void revoke()}
            >
              {revoking ? "Signing out…" : "Confirm sign out"}
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
