import { Fragment, type ReactNode, useEffect, useRef, useState } from "react";
import {
  Link,
  Navigate,
  Outlet,
  Route,
  Routes,
  useLocation,
} from "react-router";
import { AuthProvider, useAuth } from "./auth/AuthProvider";
import { AuthPage } from "./pages/AuthPage";
import { WorkspacePage } from "./pages/WorkspacePage";
import { InvitePage } from "./pages/InvitePage";
import { RecoveryPage } from "./pages/RecoveryPage";
import type { AccountLink } from "./auth/capability";
import { AccountSettings } from "./settings/AccountSettings";
import { SecuritySettings } from "./settings/SecuritySettings";
import { IntegrationsSettings } from "./settings/IntegrationsSettings";
import { ProjectSettingsPage } from "./pages/ProjectSettingsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { PublicLayout } from "./layouts/PublicLayout";
import { WorkspaceLayout } from "./layouts/WorkspaceLayout";

const pageTitles: Record<string, string> = {
  "/login": "Sign in",
  "/register": "Create account",
  "/forgot-password": "Recover account",
  "/reset-password": "Reset password",
  "/verify-email": "Verify email",
  "/invite": "Project invitation",
  "/tasks": "Tasks",
  "/tasks/today": "Today",
  "/tasks/unassigned": "Unassigned",
  "/tasks/recurring": "Recurring tasks",
  "/settings/account": "Account",
  "/settings/security": "Security",
  "/settings/integrations": "Integrations",
};

function SessionGate({ children }: { children: ReactNode }) {
  const auth = useAuth();
  if (auth.startup !== "ready")
    return (
      <PublicLayout>
        <section
          className="profile"
          aria-labelledby="session-heading"
          aria-busy={auth.startup === "loading"}
        >
          <h1 id="session-heading">
            {auth.startup === "loading"
              ? "Checking your session…"
              : "Unable to restore your session"}
          </h1>
          {auth.startup === "loading" ? (
            <p role="status">Loading…</p>
          ) : (
            <>
              <p className="notice error" role="alert">
                {auth.error}
              </p>
              <button
                type="button"
                className="button primary"
                onClick={() => void auth.retry()}
              >
                Try again
              </button>
            </>
          )}
        </section>
      </PublicLayout>
    );
  return <Fragment key={auth.user?.id ?? "signed-out"}>{children}</Fragment>;
}

export function App({
  invitationToken = "",
  accountLink,
}: {
  invitationToken?: string;
  accountLink?: AccountLink;
}) {
  const [capability, setCapability] = useState(accountLink);
  const [invitation, setInvitation] = useState(invitationToken);
  const { pathname, state } = useLocation();
  const previousPath = useRef(pathname);
  useEffect(() => {
    if (previousPath.current !== pathname) {
      previousPath.current = pathname;
      // Discard in-memory capabilities when leaving their route, without storage.
      setCapability(undefined);
    }
  }, [pathname]);
  useEffect(() => {
    const title =
      pageTitles[pathname] ??
      (pathname.startsWith("/projects/")
        ? pathname.endsWith("/settings")
          ? "Project settings"
          : "Tasks"
        : "Page not found");
    document.title = `${title} · TaskFlow`;
  }, [pathname]);
  return (
    <AuthProvider>
      <Routes>
        <Route element={<PublicLayout />}>
          <Route
            path="/forgot-password"
            element={
              <RecoveryPage key="request" mode="request" consumed={() => {}} />
            }
          />
          <Route
            path="/reset-password"
            element={
              <RecoveryPage
                key="reset"
                mode="reset"
                token={capability?.kind === "reset" ? capability.token : ""}
                consumed={() => setCapability(undefined)}
              />
            }
          />
          <Route
            path="/verify-email"
            element={
              <RecoveryPage
                key="verify"
                mode="verify"
                token={capability?.kind === "verify" ? capability.token : ""}
                consumed={() => setCapability(undefined)}
              />
            }
          />
        </Route>
        <Route
          element={
            <SessionGate>
              <Outlet />
            </SessionGate>
          }
        >
          <Route element={<PublicLayout />}>
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
          </Route>
          <Route element={<WorkspaceLayout />}>
            <Route path="/tasks" element={<WorkspacePage />} />
            <Route path="/tasks/today" element={<WorkspacePage />} />
            <Route path="/tasks/unassigned" element={<WorkspacePage />} />
            <Route path="/tasks/recurring" element={<WorkspacePage />} />
            <Route
              path="/projects/:projectId/tasks"
              element={<WorkspacePage />}
            />
            <Route
              path="/projects/:projectId/settings"
              element={<ProjectSettingsPage key={pathname} />}
            />
            <Route
              path="/profile"
              element={
                <Navigate to="/settings/account" replace state={state} />
              }
            />
            <Route path="/settings" element={<SettingsPage />}>
              <Route
                index
                element={<Navigate to="account" replace state={state} />}
              />
              <Route path="account" element={<AccountSettings />} />
              <Route path="security" element={<SecuritySettings />} />
              <Route path="integrations" element={<IntegrationsSettings />} />
            </Route>
          </Route>
          <Route element={<PublicLayout />}>
            <Route
              path="*"
              element={
                <section className="center">
                  <h1>Page not found</h1>

                  <Link className="button primary" to="/">
                    Back to home
                  </Link>
                </section>
              }
            />
          </Route>
        </Route>
      </Routes>
    </AuthProvider>
  );
}
