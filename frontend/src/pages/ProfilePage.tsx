import { Navigate } from "react-router";
import { useAuth } from "../auth/AuthProvider";

const date = (value: string | number) =>
  new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));

export function ProfilePage() {
  const auth = useAuth();
  if (!auth.user) return <Navigate to="/login" replace />;
  return (
    <section className="profile" aria-labelledby="profile-heading">
      <div className="profile-heading">
        <div>
          <span className="eyebrow">YOUR ACCOUNT</span>
          <h1 id="profile-heading">你的个人空间</h1>
          <p className="subtitle">很高兴再次见到你。</p>
        </div>
        <span className="pill">已登录</span>
      </div>
      {auth.error && (
        <div className="notice error" role="alert">
          {auth.error}
        </div>
      )}
      <div className="profile-card" aria-busy={auth.busy}>
        <div className="identity">
          <div className="avatar" aria-hidden="true">
            {auth.user.email[0]?.toUpperCase()}
          </div>
          <div>
            <h2>{auth.user.email}</h2>
            <p>TaskFlow 个人账号</p>
          </div>
        </div>
        <dl className="details">
          <div className="detail">
            <dt>邮箱地址</dt>
            <dd>{auth.user.email}</dd>
          </div>
          <div className="detail">
            <dt>加入时间</dt>
            <dd>{date(auth.user.created_at)}</dd>
          </div>
          <div className="detail">
            <dt>本次登录有效至</dt>
            <dd>{auth.expiresAt ? date(auth.expiresAt) : "—"}</dd>
          </div>
        </dl>
        <div className="actions">
          <p role="status">
            {auth.busy ? "正在处理，请稍候…" : "刷新页面后需要重新登录。"}
          </p>
          <div className="actions-buttons">
            <button
              type="button"
              className="button secondary"
              disabled={auth.busy}
              onClick={() => void auth.refresh()}
            >
              刷新资料
            </button>
            <button
              type="button"
              className="button primary"
              disabled={auth.busy}
              onClick={() => void auth.logout()}
            >
              退出登录
            </button>
          </div>
        </div>
      </div>
    </section>
  );
}
