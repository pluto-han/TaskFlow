import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { accountApi } from "../api/account";
import { ApiError, errorMessage } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { validate, type FieldErrors } from "../auth/validation";
import { PasswordFields } from "../components/PasswordFields";

export function RecoveryPage({
  mode,
  token = "",
  consumed,
}: {
  mode: "request" | "reset" | "verify";
  token?: string;
  consumed(): void;
}) {
  const auth = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errors, setErrors] = useState<FieldErrors>({});
  const [error, setError] = useState("");
  const [invalid, setInvalid] = useState(false);
  const [disabled, setDisabled] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const pending = useRef<AbortController | null>(null);
  const locked = useRef(false);
  const form = useRef<HTMLFormElement>(null);
  const result = useRef<HTMLDivElement>(null);
  useEffect(() => () => pending.current?.abort(), []);
  useEffect(() => {
    if (done || invalid) result.current?.focus();
  }, [done, invalid]);
  const missing = mode !== "request" && !token && !done;
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (locked.current) return;
    const fields: FieldErrors =
      mode === "request"
        ? { email: validate(email, "placeholder").email }
        : mode === "reset"
          ? validate("valid@example.com", password, confirm)
          : {};
    setErrors(fields);
    setError("");
    const first = Object.keys(fields).find(
      (key) => fields[key as keyof FieldErrors],
    );
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
      if (mode === "request")
        await accountApi.requestReset(email.trim(), controller.signal);
      else if (mode === "reset")
        await accountApi.reset(token, password, controller.signal);
      else await accountApi.confirmEmail(token, controller.signal);
      if (controller.signal.aborted) return;
      setPassword("");
      setConfirm("");
      setDone(true);
      consumed();
      // Recheck a currently signed-in account; never assume the link belongs to it.
      if (auth.user && mode !== "request") void auth.refresh();
    } catch (err) {
      if (!controller.signal.aborted) {
        setError(errorMessage(err));
        if (err instanceof ApiError) {
          setDisabled(err.code === "EMAIL_DISABLED");
          setInvalid(
            ["RESET_TOKEN_INVALID", "EMAIL_TOKEN_INVALID"].includes(err.code),
          );
        }
      }
    } finally {
      if (!controller.signal.aborted) {
        locked.current = false;
        setBusy(false);
      }
    }
  }
  const title = done
    ? mode === "request"
      ? "Check your email"
      : mode === "reset"
        ? "Password updated"
        : "Email verified"
    : missing || invalid
      ? "This link is no longer available"
      : mode === "request"
        ? "Forgot your password?"
        : mode === "reset"
          ? "Choose a new password"
          : "Verify your email";
  return (
    <div className="auth-layout recovery-layout">
      <aside className="story recovery-story" aria-label="Account recovery">
        <span className="eyebrow">YOUR ACCOUNT</span>
        <h2>
          A secure way
          <br />
          back to your tasks.
        </h2>
        <p>
          Keep your account details up to date, so your work is always within
          reach.
        </p>
        <div className="art" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
        <p className="story-bottom">
          Your tasks stay right where you left them.
        </p>
      </aside>
      <section className="auth-card" aria-labelledby="recovery-heading">
        <Link to="/login" className="recovery-back">
          ← Back to sign in
        </Link>
        <span className="eyebrow">
          {mode === "verify" ? "EMAIL VERIFICATION" : "ACCOUNT RECOVERY"}
        </span>
        <h1 id="recovery-heading">{title}</h1>
        {done ? (
          <div ref={result} tabIndex={-1} className="recovery-result">
            <span className="recovery-check" aria-hidden="true">
              ✓
            </span>
            <p role="status">
              {mode === "request"
                ? "If this address belongs to an account, a reset link may arrive shortly. Check your inbox and spam folder. Links are valid for 30 minutes."
                : mode === "reset"
                  ? "Your password has been updated and previous sessions have ended. Sign in with your new password to continue."
                  : "Your email address is verified. You can return to your account."}
            </p>
            <Link
              className="button primary wide"
              to={mode === "verify" ? "/profile" : "/login"}
            >
              {mode === "verify" ? "Go to profile" : "Continue to sign in"}
            </Link>
            {mode === "request" && (
              <Link
                className="recovery-secondary"
                to="/forgot-password"
                onClick={() => {
                  setDone(false);
                  setError("");
                }}
              >
                Try another email address
              </Link>
            )}
          </div>
        ) : missing || invalid ? (
          <div ref={result} tabIndex={-1} className="recovery-result">
            <p className="subtitle">
              {mode === "reset"
                ? "It may have expired or already been used. Request a new link to set your password."
                : "Open the original email again, or request a new verification link from your profile."}
            </p>
            <Link
              className="button primary wide"
              to={mode === "reset" ? "/forgot-password" : "/profile"}
            >
              {mode === "reset" ? "Request a new link" : "Go to profile"}
            </Link>
          </div>
        ) : (
          <>
            <p className="subtitle">
              {mode === "request"
                ? "Enter the email address you used to create your account."
                : mode === "reset"
                  ? "Set a new password for your account. You will need to sign in again on all devices."
                  : "Confirm this email address for the account that requested this link. No password is required."}
            </p>
            {error && (
              <div className="notice error" role="alert">
                {error}
              </div>
            )}
            <form
              ref={form}
              onSubmit={(event) => void submit(event)}
              noValidate
              aria-busy={busy}
            >
              {mode === "request" && (
                <div className="field">
                  <label htmlFor="recovery-email">Email address</label>
                  <input
                    id="recovery-email"
                    name="email"
                    type="email"
                    autoComplete="email"
                    autoCapitalize="none"
                    spellCheck={false}
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    disabled={busy || disabled}
                    required
                    aria-invalid={!!errors.email}
                    aria-describedby={
                      errors.email ? "recovery-email-error" : undefined
                    }
                  />
                  {errors.email && (
                    <p id="recovery-email-error" className="field-error">
                      {errors.email}
                    </p>
                  )}
                </div>
              )}
              {mode === "reset" && (
                <PasswordFields
                  prefix="reset"
                  password={password}
                  confirm={confirm}
                  onPassword={setPassword}
                  onConfirm={setConfirm}
                  errors={errors}
                  busy={busy || disabled}
                />
              )}
              <button
                className="button primary wide"
                disabled={busy || disabled}
                type="submit"
              >
                {busy && <span className="spinner" aria-hidden="true" />}
                {busy
                  ? "Please wait…"
                  : mode === "request"
                    ? "Send reset link"
                    : mode === "reset"
                      ? "Save new password"
                      : "Verify email address"}
              </button>
            </form>
            <p className="footnote">
              {mode === "request"
                ? "For your privacy, the response is the same whether an account exists or not."
                : "Opening this page does not change your account. Confirmation happens only when you submit."}
            </p>
          </>
        )}
      </section>
    </div>
  );
}
