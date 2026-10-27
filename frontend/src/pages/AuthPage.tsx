import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { api, errorMessage } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { validate, type FieldErrors } from "../auth/validation";

export function AuthPage({ register = false }: { register?: boolean }) {
  const auth = useAuth();
  const navigate = useNavigate();
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
  if (auth.user) return <Navigate to="/profile" replace />;

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
        if (!controller.signal.aborted) navigate("/profile", { replace: true });
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
      <aside className="story" aria-label="TaskFlow 简介">
        <span className="eyebrow">A LITTLE ROOM TO FOCUS</span>
        <h2>
          给重要的事，
          <br />
          留一点空间。
        </h2>
        <p>
          从一个清晰的开始，
          <br />
          找到自己的节奏。
        </p>
        <div className="art" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
        <p className="story-bottom">少一点分心，多一点从容。</p>
      </aside>
      <section className="auth-card" aria-labelledby="auth-heading">
        <span className="eyebrow">YOUR PERSONAL SPACE</span>
        <h1 id="auth-heading">{register ? "开启你的空间" : "欢迎回来。"}</h1>
        <p className="subtitle">
          {register
            ? "创建一个账号，从这里开始。"
            : "登录 TaskFlow，回到你的个人空间。"}
        </p>
        {registered ? (
          <>
            <div className="notice" role="status">
              注册成功！现在可以使用新账号登录。
            </div>
            <Link className="button primary wide" to="/login">
              前往登录
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
                <label htmlFor="email">邮箱地址</label>
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
                <label htmlFor="password">密码</label>
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
                      使用 15–128 个字符，建议选择容易记住的长密码。
                    </p>
                  )
                )}
              </div>
              {register && (
                <div className="field">
                  <label htmlFor="confirm">确认密码</label>
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
                    ? "正在创建…"
                    : "正在登录…"
                  : register
                    ? "创建账号"
                    : "登录"}
                <span aria-hidden="true">→</span>
              </button>
            </form>
            <p className="auth-switch">
              {register ? "已经有账号？" : "还没有账号？"}{" "}
              <Link to={register ? "/login" : "/register"}>
                {register ? "去登录" : "创建账号"}
              </Link>
            </p>
          </>
        )}
        <p className="footnote">登录仅在当前页面有效，刷新后需重新登录。</p>
      </section>
    </div>
  );
}
