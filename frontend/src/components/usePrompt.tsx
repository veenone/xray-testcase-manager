import { useDialogs } from "../contexts/DialogContext";

// usePrompt returns the app-wide async prompt dialog (WebView2 does not
// implement window.prompt — it silently returns null). The dialog is rendered
// once by DialogProvider (audit A2); this hook just exposes the trigger.

export interface PromptOptions {
  title: string;
  defaultValue?: string;
  placeholder?: string;
  submitLabel?: string;
  password?: boolean;
}

export function usePrompt(): {
  prompt: (opts: PromptOptions) => Promise<string | null>;
} {
  return { prompt: useDialogs().prompt };
}
