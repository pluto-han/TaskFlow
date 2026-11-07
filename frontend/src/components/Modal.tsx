import { useEffect, useRef, type ReactNode } from "react";

export function Modal({
  title,
  busy = false,
  close,
  children,
}: {
  title: string;
  busy?: boolean;
  close(): void;
  children: ReactNode;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const previous = useRef<HTMLElement | null>(null);
  useEffect(() => {
    previous.current = document.activeElement as HTMLElement;
    const node = dialog.current!;
    node.showModal();
    return () => {
      node.close();
      if (previous.current?.isConnected) previous.current.focus();
    };
  }, []);
  return (
    <dialog
      ref={dialog}
      className="modal"
      aria-labelledby="modal-title"
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) close();
      }}
    >
      <header className="modal-heading">
        <h2 id="modal-title">{title}</h2>
        <button
          className="icon-button"
          type="button"
          onClick={close}
          disabled={busy}
          aria-label="Close dialog"
        >
          ×
        </button>
      </header>
      {children}
    </dialog>
  );
}
