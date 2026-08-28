import { useDialogs } from "../contexts/DialogContext";

// useNotice returns the app-wide async notice dialog (an in-app, themed
// replacement for window.alert, which WebView2 renders out-of-theme). The dialog
// is rendered once by DialogProvider (audit A2); this hook just exposes the
// trigger. It also announces to assistive tech via LiveRegion (handled in the
// provider).

export interface NoticeOptions {
  title: string;
  message?: string;
  tone?: "info" | "error";
}

export function useNotice(): {
  notice: (opts: NoticeOptions) => Promise<void>;
} {
  return { notice: useDialogs().notice };
}
