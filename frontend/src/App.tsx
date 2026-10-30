import { useEffect, useRef, useState } from "react";
import { Link, Navigate, Route, Routes, useLocation } from "react-router";
import { AuthProvider, useAuth } from "./auth/AuthProvider";
import { AuthPage } from "./pages/AuthPage";
import { WorkspacePage } from "./pages/WorkspacePage";
import { InvitePage } from "./pages/InvitePage";
import { ProfilePage } from "./pages/ProfilePage";

function Navigation() {
  const { user } = useAuth();
  return user ? (
    <nav className="app-nav" aria-label="主导航">
      <Link to="/tasks">任务</Link>
      <Link to="/profile">个人资料</Link>
    </nav>
  ) : (
    <span className="nav-label">为专注留出空间</span>
  );
}

export function App({ invitationToken = "" }: { invitationToken?: string }) {
  const [invitation, setInvitation] = useState(invitationToken);
  const { pathname } = useLocation();
  const main = useRef<HTMLElement>(null);
  useEffect(() => {
    document.title = `${pathname === "/tasks" ? "任务" : pathname === "/register" ? "注册" : pathname === "/profile" ? "个人空间" : "登录"} · TaskFlow`;
    main.current?.focus();
  }, [pathname]);
  return (
    <AuthProvider>
      <a className="skip-link" href="#main">
        跳到主要内容
      </a>
      <div className="shell">
        <header className="topbar">
          <Link className="brand" to="/" aria-label="TaskFlow 首页">
            <span className="brand-mark" aria-hidden="true">
              ↗
            </span>
            TaskFlow
          </Link>
          <Navigation />
        </header>
        <main id="main" className="main" ref={main} tabIndex={-1}>
          <Routes>
            <Route path="/" element={<Navigate to="/tasks" replace />} />
            <Route path="/login" element={<AuthPage key="login" />} />
            <Route
              path="/register"
              element={<AuthPage key="register" register />}
            />
            <Route
              path="/invite"
              element={
                <InvitePage
                  token={invitation}
                  consumed={() => setInvitation("")}
                />
              }
            />
            <Route path="/tasks" element={<WorkspacePage />} />
            <Route path="/profile" element={<ProfilePage />} />
            <Route
              path="*"
              element={
                <section className="center">
                  <h1>这里还没有内容</h1>
                  <p>这个页面不存在，回到个人空间继续吧。</p>
                  <Link className="button primary" to="/">
                    返回首页
                  </Link>
                </section>
              }
            />
          </Routes>
        </main>
        <footer className="footer">
          <span>TaskFlow · 有条不紊，从容向前</span>
          <span>一点专注，一点进步。</span>
        </footer>
      </div>
    </AuthProvider>
  );
}
