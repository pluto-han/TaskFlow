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
      <p>确定删除「{name}」？此操作无法撤销。</p>
      {project && (
        <p className="hint">项目内有任务时无法删除，请先移走或删除任务。</p>
      )}
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      <div className="modal-actions">
        <button className="button secondary" disabled={busy} onClick={close}>
          取消
        </button>
        <button
          className="button danger"
          disabled={busy}
          onClick={() => void confirm()}
        >
          {busy ? "正在删除…" : "确认删除"}
        </button>
      </div>
    </Modal>
  );
}
