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
    <nav className="app-nav" aria-label="Main navigation">
      <Link to="/tasks">Tasks</Link>
      <Link to="/profile">Profile</Link>
    </nav>
  ) : (
    <span className="nav-label">Make room for what matters</span>
  );
}

export function App({ invitationToken = "" }: { invitationToken?: string }) {
  const [invitation, setInvitation] = useState(invitationToken);
  const { pathname } = useLocation();
  const main = useRef<HTMLElement>(null);
  useEffect(() => {
    document.title = `${pathname === "/tasks" ? "Tasks" : pathname === "/register" ? "Create account" : pathname === "/profile" ? "Your space" : "Sign in"} · TaskFlow`;
    main.current?.focus();
  }, [pathname]);
  return (
    <AuthProvider>
      <a className="skip-link" href="#main">
        Skip to main content
      </a>
      <div className="shell">
        <header className="topbar">
          <Link className="brand" to="/" aria-label="TaskFlow home">
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
                  <h1>Nothing here just yet</h1>
                  <p>
                    This page could not be found. Find your way back to your
                    space.
                  </p>
                  <Link className="button primary" to="/">
                    Back to home
                  </Link>
                </section>
              }
            />
          </Routes>
        </main>
        <footer className="footer">
          <span>TaskFlow · A little order. A gentler pace.</span>
          <span>A little focus. A little further.</span>
        </footer>
      </div>
    </AuthProvider>
  );
}
