import { useCallback, useState } from "react";
import type { ReactNode } from "react";

import { Modal } from "./Modal";

// WebView2 (the Wails runtime on Windows) renders window.confirm() as a bare,
// out-of-theme dialog. useConfirm is an in-app, themed replacement: an async
// modal that resolves to true (confirmed) or false (cancelled). Used for
// destructive actions so every delete asks first with a consistent UI.

export interface ConfirmOptions {
  title: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  // danger styles the confirm button as destructive (the default for deletes).
  danger?: boolean;
}

interface State {
  opts: ConfirmOptions;
  resolve: (value: boolean) => void;
}

export function useConfirm(): {
  confirm: (opts: ConfirmOptions) => Promise<boolean>;
  confirmUI: ReactNode;
} {
  const [state, setState] = useState<State | null>(null);

  const confirm = useCallback(
    (opts: ConfirmOptions) =>
      new Promise<boolean>((resolve) => setState({ opts, resolve })),
    [],
  );

  const close = (value: boolean) => {
    if (state) state.resolve(value);
    setState(null);
  };

  const confirmUI = state ? (
    <ConfirmModal
      {...state.opts}
      onConfirm={() => close(true)}
      onCancel={() => close(false)}
    />
  ) : null;

  return { confirm, confirmUI };
}

function ConfirmModal({
  title,
  message,
  confirmLabel,
  cancelLabel,
  danger = true,
  onConfirm,
  onCancel,
}: ConfirmOptions & {
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <Modal
      onClose={onCancel}
      className="modal confirm-modal"
      role="alertdialog"
      labelledBy="confirm-title"
    >
      <div className="pending-head">
        <h2 id="confirm-title">{title}</h2>
        <button
          className="btn btn-ghost"
          onClick={onCancel}
          title="Cancel"
          aria-label="Cancel"
        >
          ✕
        </button>
      </div>
      {message && <div className="bulk-body confirm-message">{message}</div>}
      <div className="pending-actions">
        <button className="btn" onClick={onCancel} autoFocus>
          {cancelLabel ?? "Cancel"}
        </button>
        <button
          className={`btn ${danger ? "btn-danger" : "btn-primary"}`}
          onClick={onConfirm}
        >
          {confirmLabel ?? "Delete"}
        </button>
      </div>
    </Modal>
  );
}
