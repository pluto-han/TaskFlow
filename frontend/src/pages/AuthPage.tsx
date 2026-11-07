import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate, useLocation } from "react-router";
import { api, errorMessage } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { validate, type FieldErrors } from "../auth/validation";

export function AuthPage({ register = false }: { register?: boolean }) {
  const auth = useAuth();
  const navigate = useNavigate();
  const nextInvite =
    new URLSearchParams(useLocation().search).get("next") === "invite";
  const destination = nextInvite ? "/invite" : "/profile";
  const suffix = nextInvite ? "?next=invite" : "";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errors, setErrors] = useState<FieldErrors>({});
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [registered, setRegistered] = useState(false);
  const form = useRef<HTMLFormElement>(null);
  const pending = useRef<AbortController | null>(null);
  const locked = useRef(false);
  useEffect(() => () => pending.current?.abort(), []);
  if (auth.user) return <Navigate to={destination} replace />;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (locked.current) return;
    const fields = validate(email, password, register ? confirm : undefined);
    setErrors(fields);
    setError("");
    const first = Object.keys(fields)[0];
    if (first) {
      form.current
        ?.querySelector<HTMLInputElement>(`[name="${first}"]`)
        ?.focus();
      return;
    }
    locked.current = true;
    setSubmitting(true);
    const controller = new AbortController();
    pending.current = controller;
    try {
      if (register) {
        await api.register(
          { email: email.trim(), password },
          controller.signal,
        );
        if (!controller.signal.aborted) {
          setRegistered(true);
          setPassword("");
          setConfirm("");
        }
      } else {
        await auth.login({ email: email.trim(), password }, controller.signal);
        if (!controller.signal.aborted)
          navigate(destination, { replace: true });
      }
    } catch (err) {
      if (!controller.signal.aborted) setError(errorMessage(err));
    } finally {
      if (!controller.signal.aborted) {
        locked.current = false;
        setSubmitting(false);
      }
    }
  }

  return (
    <div className="auth-layout">
      <aside className="story" aria-label="About TaskFlow">
        <span className="eyebrow">A LITTLE ROOM TO FOCUS</span>
        <h2>
          Make room
          <br />
          for what matters.
        </h2>
        <p>
          Begin with a little clarity.
          <br />
          Find a rhythm of your own.
        </p>
        <div className="art" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
        <p className="story-bottom">Less noise. More room to breathe.</p>
      </aside>
      <section className="auth-card" aria-labelledby="auth-heading">
        <span className="eyebrow">YOUR PERSONAL SPACE</span>
        <h1 id="auth-heading">
          {register ? "Make yourself a little space" : "Welcome back."}
        </h1>
        <p className="subtitle">
          {register
            ? "A fresh start, and a little space of your own."
            : "Step back into your space."}
        </p>
        {registered ? (
          <>
            <div className="notice" role="status">
              Your space is ready. Sign in to make it your own.
            </div>
            <Link className="button primary wide" to={"/login" + suffix}>
              Continue to sign in
            </Link>
          </>
        ) : (
          <>
            {auth.notice && (
              <div className="notice" role="status">
                {auth.notice}
              </div>
            )}
            {error && (
              <div className="notice error" role="alert">
                {error}
              </div>
            )}
            <form
              ref={form}
              onSubmit={(event) => void submit(event)}
              noValidate
              aria-busy={submitting}
            >
              <div className="field">
                <label htmlFor="email">Email address</label>
                <input
                  id="email"
                  name="email"
                  type="email"
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="you@example.com"
                  required
                  disabled={submitting}
                  aria-invalid={!!errors.email}
                  aria-describedby={errors.email ? "email-error" : undefined}
                />
                {errors.email && (
                  <p className="field-error" id="email-error">
                    {errors.email}
                  </p>
                )}
              </div>
              <div className="field">
                <label htmlFor="password">Password</label>
                <input
                  id="password"
                  name="password"
                  type="password"
                  autoComplete={register ? "new-password" : "current-password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  disabled={submitting}
                  aria-invalid={!!errors.password}
                  aria-describedby={
                    errors.password
                      ? "password-error"
                      : register
                        ? "password-hint"
                        : undefined
                  }
                />
                {errors.password ? (
                  <p className="field-error" id="password-error">
                    {errors.password}
                  </p>
                ) : (
                  register && (
                    <p className="hint" id="password-hint">
                      Use 15–128 characters. A few memorable words can make a
                      good passphrase.
                    </p>
                  )
                )}
              </div>
              {register && (
                <div className="field">
                  <label htmlFor="confirm">Confirm password</label>
                  <input
                    id="confirm"
                    name="confirm"
                    type="password"
                    autoComplete="new-password"
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    required
                    disabled={submitting}
                    aria-invalid={!!errors.confirm}
                    aria-describedby={
                      errors.confirm ? "confirm-error" : undefined
                    }
                  />
                  {errors.confirm && (
                    <p className="field-error" id="confirm-error">
                      {errors.confirm}
                    </p>
                  )}
                </div>
              )}
              <button
                className="button primary wide"
                disabled={submitting}
                type="submit"
              >
                {submitting && <span className="spinner" aria-hidden="true" />}
                {submitting
                  ? register
                    ? "Creating your space…"
                    : "Signing in…"
                  : register
                    ? "Create account"
                    : "Sign in"}
                <span aria-hidden="true">→</span>
              </button>
            </form>
            <p className="auth-switch">
              {register ? "Already have an account?" : "New here?"}{" "}
              <Link to={(register ? "/login" : "/register") + suffix}>
                {register ? "Sign in" : "Create account"}
              </Link>
            </p>
          </>
        )}
        <p className="footnote">
          Your session stays in this tab. Refreshing the page will sign you out.
        </p>
      </section>
    </div>
  );
}
