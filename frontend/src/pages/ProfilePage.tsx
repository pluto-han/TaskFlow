import { useState } from "react";
import { DiscordSettings } from "./DiscordSettings";
import { Navigate } from "react-router";
import { useAuth } from "../auth/AuthProvider";

const date = (value: string | number) =>
  new Intl.DateTimeFormat("en-GB", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));

export function ProfilePage() {
  const auth = useAuth();
  const [copyMessage, setCopyMessage] = useState("");
  if (!auth.user) return <Navigate to="/login" replace />;
  return (
    <section className="profile" aria-labelledby="profile-heading">
      <div className="profile-heading">
        <div>
          <span className="eyebrow">YOUR ACCOUNT</span>
          <h1 id="profile-heading">A space of your own</h1>
          <p className="subtitle">Good to have you here again.</p>
        </div>
        <span className="pill">Signed in</span>
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
            <p>Your TaskFlow account</p>
          </div>
        </div>
        <dl className="details">
          <div className="detail">
            <dt>Email address</dt>
            <dd>{auth.user.email}</dd>
          </div>
          <div className="detail">
            <dt>Account ID</dt>
            <dd>
              <code>{auth.user.id}</code>{" "}
              <button
                className="text-button"
                onClick={() =>
                  void navigator.clipboard
                    .writeText(auth.user!.id)
                    .then(() => setCopyMessage("Account ID copied"))
                    .catch(() =>
                      setCopyMessage("Please select and copy your account ID."),
                    )
                }
              >
                Copy account ID
              </button>
              <span aria-live="polite">{copyMessage}</span>
            </dd>
          </div>
          <div className="detail">
            <dt>Here since</dt>
            <dd>{date(auth.user.created_at)}</dd>
          </div>
          <div className="detail">
            <dt>Signed in until</dt>
            <dd>{auth.expiresAt ? date(auth.expiresAt) : "—"}</dd>
          </div>
        </dl>
        <div className="actions">
          <p role="status">
            {auth.busy
              ? "Just a moment…"
              : "Your sign-in is kept across refreshes and tabs."}
          </p>
          <div className="actions-buttons">
            <button
              type="button"
              className="button secondary"
              disabled={auth.busy}
              onClick={() => void auth.refresh()}
            >
              Refresh profile
            </button>
            <button
              type="button"
              className="button primary"
              disabled={auth.busy}
              onClick={() => void auth.logout()}
            >
              Sign out
            </button>
          </div>
        </div>
      </div>
      <DiscordSettings key={auth.user.id} />
    </section>
  );
}
