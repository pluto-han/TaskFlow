import { useState } from "react";
import { Modal } from "../components/Modal";
import { errorMessage } from "../api/client";

export function DeleteDialog({
  title,
  name,
  project = false,
  close,
  remove,
}: {
  title: string;
  name: string;
  project?: boolean;
  close(): void;
  remove(): Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function confirm() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await remove();
      close();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title={title} busy={busy} close={close}>
      <p>Delete “{name}”? This cannot be undone.</p>
      {project && (
        <p className="hint">
          Move or delete the tasks in this project before deleting it.
        </p>
      )}
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      <div className="modal-actions">
        <button className="button secondary" disabled={busy} onClick={close}>
          Cancel
        </button>
        <button
          className="button danger"
          disabled={busy}
          onClick={() => void confirm()}
        >
          {busy ? "Deleting…" : "Confirm delete"}
        </button>
      </div>
    </Modal>
  );
}
