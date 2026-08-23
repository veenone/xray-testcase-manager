import { useCallback, useState } from "react";
import type { ReactNode } from "react";
import { Modal } from "./Modal";

// WebView2 (the Wails runtime on Windows) does not implement window.prompt() —
// it silently returns null. usePrompt is a drop-in async replacement: an
// in-app modal that resolves to the entered string, or null on cancel.

export interface PromptOptions {
  title: string;
  defaultValue?: string;
  placeholder?: string;
  submitLabel?: string;
  password?: boolean;
}

interface State {
  opts: PromptOptions;
  resolve: (value: string | null) => void;
}

export function usePrompt(): {
  prompt: (opts: PromptOptions) => Promise<string | null>;
  promptUI: ReactNode;
} {
  const [state, setState] = useState<State | null>(null);

  const prompt = useCallback(
    (opts: PromptOptions) =>
      new Promise<string | null>((resolve) => setState({ opts, resolve })),
    [],
  );

  const close = (value: string | null) => {
    if (state) state.resolve(value);
    setState(null);
  };

  const promptUI = state ? (
    <PromptModal
      {...state.opts}
      onSubmit={(v) => close(v)}
      onCancel={() => close(null)}
    />
  ) : null;

  return { prompt, promptUI };
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
    <Modal onClose={onCancel} className="modal prompt-modal" labelledBy="prompt-modal-title">
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
