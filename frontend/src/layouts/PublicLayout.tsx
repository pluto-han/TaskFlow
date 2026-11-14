import { useEffect, useRef, type ReactNode } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { useAuth } from "../auth/AuthProvider";

export function PublicLayout({
  navigation,
  children,
  workspace = false,
}: {
  navigation?: ReactNode;
  children?: ReactNode;
  workspace?: boolean;
}) {
  const { user } = useAuth();
  const main = useRef<HTMLElement>(null);
  const { pathname } = useLocation();
  useEffect(() => {
    main.current?.focus();
  }, [pathname]);
  return (
    <>
      <a className="skip-link" href="#main">
        Skip to main content
      </a>
      <div
        className={workspace ? "shell workspace-shell" : "shell public-shell"}
      >
        <header className="topbar">
          <Link className="brand" to="/tasks" aria-label="TaskFlow home">
            TaskFlow
          </Link>
          {navigation ??
            (user && (
              <nav className="app-nav" aria-label="Main navigation">
                <NavLink to="/tasks">Tasks</NavLink>
                <NavLink to="/settings/account">Settings</NavLink>
              </nav>
            ))}
        </header>
        <main id="main" className="main" ref={main} tabIndex={-1}>
          {children ?? <Outlet />}
        </main>
      </div>
    </>
  );
}
