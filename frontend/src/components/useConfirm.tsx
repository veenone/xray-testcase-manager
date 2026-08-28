import { useDialogs } from "../contexts/DialogContext";

// useConfirm returns the app-wide async confirm dialog (an in-app, themed
// replacement for window.confirm, which WebView2 renders out-of-theme). The
// dialog itself is rendered once by DialogProvider (audit A2); this hook just
// exposes the trigger.

export interface ConfirmOptions {
  title: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  // danger styles the confirm button as destructive (the default for deletes).
  danger?: boolean;
}

export function useConfirm(): {
  confirm: (opts: ConfirmOptions) => Promise<boolean>;
} {
  return { confirm: useDialogs().confirm };
}
