import { useCallback, useState } from "react";
import type { ReactNode } from "react";

import { Modal } from "./Modal";
import { announce } from "./LiveRegion";

// WebView2 renders window.alert() as a bare, out-of-theme dialog. useNotice is
// an in-app, themed replacement: an async modal that resolves when dismissed.
// Pairs with useConfirm for one consistent dialog system across the app.

export interface NoticeOptions {
  title: string;
  message?: string;
  tone?: "info" | "error";
}

interface State {
  opts: NoticeOptions;
  resolve: () => void;
}

export function useNotice(): {
  notice: (opts: NoticeOptions) => Promise<void>;
  noticeUI: ReactNode;
} {
  const [state, setState] = useState<State | null>(null);

  const notice = useCallback(
    (opts: NoticeOptions) =>
      new Promise<void>((resolve) => {
        // Announce to assistive tech immediately — errors interrupt (assertive).
        announce(
          `${opts.title}${opts.message ? ". " + opts.message : ""}`,
          opts.tone === "error",
        );
        setState({ opts, resolve });
      }),
    [],
  );

  const close = () => {
    if (state) state.resolve();
    setState(null);
  };

  const noticeUI = state ? (
    <NoticeModal {...state.opts} onClose={close} />
  ) : null;

  return { notice, noticeUI };
}

function NoticeModal({
  title,
  message,
  tone = "info",
  onClose,
}: NoticeOptions & { onClose: () => void }) {
  return (
    <Modal
      onClose={onClose}
      className="modal confirm-modal"
      role="alertdialog"
      labelledBy="notice-title"
    >
      <div className="pending-head">
        <h2 id="notice-title">{title}</h2>
        <button
          className="btn btn-ghost"
          onClick={onClose}
          title="Close"
          aria-label="Close"
        >
          ✕
        </button>
      </div>
      {message && (
        <div className={`bulk-body confirm-message${tone === "error" ? " error-text" : ""}`}>
          {message}
        </div>
      )}
      <div className="pending-actions">
        <button className="btn btn-primary" onClick={onClose} autoFocus>
          OK
        </button>
      </div>
    </Modal>
  );
}
