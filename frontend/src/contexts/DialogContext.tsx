import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import type { ReactNode } from "react";
import { Modal } from "../components/Modal";
import { announce } from "../components/LiveRegion";
import type { NoticeOptions } from "../components/useNotice";
import type { ConfirmOptions } from "../components/useConfirm";
import type { PromptOptions } from "../components/usePrompt";

// DialogProvider hoists the app's async dialog system (notice / confirm / prompt)
// to a single root, replacing the ~20 per-component instances that each rendered
// their own dialog root (audit A2). WebView2 renders the native window.alert /
// confirm / prompt out-of-theme (or, for prompt, not at all), so these are the
// in-app themed replacements. Only one dialog is ever open at a time, so a
// single root instance is correct.
interface DialogApi {
  notice: (opts: NoticeOptions) => Promise<void>;
  confirm: (opts: ConfirmOptions) => Promise<boolean>;
  prompt: (opts: PromptOptions) => Promise<string | null>;
}

const DialogContext = createContext<DialogApi | null>(null);

export function useDialogs(): DialogApi {
  const ctx = useContext(DialogContext);
  if (!ctx) {
    throw new Error("useDialogs must be used within a DialogProvider");
  }
  return ctx;
}

interface NoticeState {
  opts: NoticeOptions;
  resolve: () => void;
}
interface ConfirmState {
  opts: ConfirmOptions;
  resolve: (value: boolean) => void;
}
interface PromptState {
  opts: PromptOptions;
  resolve: (value: string | null) => void;
}

export function DialogProvider({ children }: { children: ReactNode }) {
  const [noticeState, setNoticeState] = useState<NoticeState | null>(null);
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null);
  const [promptState, setPromptState] = useState<PromptState | null>(null);

  const notice = useCallback(
    (opts: NoticeOptions) =>
      new Promise<void>((resolve) => {
        // Announce to assistive tech immediately — errors interrupt (assertive).
        announce(
          `${opts.title}${opts.message ? ". " + opts.message : ""}`,
          opts.tone === "error",
        );
        setNoticeState({ opts, resolve });
      }),
    [],
  );

  const confirm = useCallback(
    (opts: ConfirmOptions) =>
      new Promise<boolean>((resolve) => setConfirmState({ opts, resolve })),
    [],
  );

  const prompt = useCallback(
    (opts: PromptOptions) =>
      new Promise<string | null>((resolve) =>
        setPromptState({ opts, resolve }),
      ),
    [],
  );

  const api = useMemo<DialogApi>(
    () => ({ notice, confirm, prompt }),
    [notice, confirm, prompt],
  );

  return (
    <DialogContext.Provider value={api}>
      {children}
      {noticeState && (
        <NoticeModal
          {...noticeState.opts}
          onClose={() => {
            noticeState.resolve();
            setNoticeState(null);
          }}
        />
      )}
      {confirmState && (
        <ConfirmModal
          {...confirmState.opts}
          onConfirm={() => {
            confirmState.resolve(true);
            setConfirmState(null);
          }}
          onCancel={() => {
            confirmState.resolve(false);
            setConfirmState(null);
          }}
        />
      )}
      {promptState && (
        <PromptModal
          {...promptState.opts}
          onSubmit={(v) => {
            promptState.resolve(v);
            setPromptState(null);
          }}
          onCancel={() => {
            promptState.resolve(null);
            setPromptState(null);
          }}
        />
      )}
    </DialogContext.Provider>
  );
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
        <div
          className={`bulk-body confirm-message${tone === "error" ? " error-text" : ""}`}
        >
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

function PromptModal({
  title,
  defaultValue,
  placeholder,
  submitLabel,
  password,
  onSubmit,
  onCancel,
}: PromptOptions & {
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(defaultValue ?? "");
  return (
    <Modal
      onClose={onCancel}
      className="modal prompt-modal"
      labelledBy="prompt-modal-title"
    >
      <div className="pending-head">
        <h2 id="prompt-modal-title">{title}</h2>
        <button className="btn btn-ghost" onClick={onCancel} title="Cancel">
          ✕
        </button>
      </div>
      <div className="bulk-body">
        <input
          className="detail-input"
          autoFocus
          type={password ? "password" : "text"}
          value={value}
          placeholder={placeholder}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") onSubmit(value);
            if (e.key === "Escape") onCancel();
          }}
        />
      </div>
      <div className="pending-actions">
        <button className="btn" onClick={onCancel}>
          Cancel
        </button>
        <button className="btn btn-primary" onClick={() => onSubmit(value)}>
          {submitLabel ?? "OK"}
        </button>
      </div>
    </Modal>
  );
}
