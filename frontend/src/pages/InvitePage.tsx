import { useState } from "react";
import { Link } from "react-router";
import { useAuth } from "../auth/AuthProvider";
import { sharingApi } from "../api/sharing";
import { errorMessage } from "../api/client";
export function InvitePage({
  token,
  consumed,
}: {
  token: string;
  consumed(): void;
}) {
  const { user, authorize } = useAuth();
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [joined, setJoined] = useState(false);
  async function accept() {
    setBusy(true);
    setError("");
    try {
      await authorize((session) => sharingApi.accept(session, token));
      setJoined(true);
      consumed();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="profile">
      <span className="eyebrow">WORK TOGETHER</span>
      <h1>Project invitation</h1>
      {joined ? (
        <>
          <p role="status">
            You are part of the project now. You can view, edit, and delete its
            tasks.
          </p>
          <Link className="button primary" to="/tasks">
            Open workspace
          </Link>
        </>
      ) : !token ? (
        <p role="alert">
          This invitation is not available. Refreshing clears it from this tab;
          please open the original link again.
        </p>
      ) : !user ? (
        <>
          <p>
            Sign in with the account this invitation was sent to. It cannot be
            passed to another account.
          </p>
          <Link className="button primary" to="/login?next=invite">
            Sign in to accept
          </Link>
        </>
      ) : (
        <>
          <p>Signed in as: {user.email}</p>
          <p>
            Joining gives you access to view, edit, and delete tasks in this
            project.
          </p>
          {error && (
            <div className="notice error" role="alert">
              {error}
            </div>
          )}
          <button
            className="button primary"
            disabled={busy}
            onClick={() => void accept()}
          >
            {busy ? "Joining…" : "Accept invitation"}
          </button>
        </>
      )}
    </section>
  );
}
