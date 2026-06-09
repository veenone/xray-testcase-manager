// Shared date formatting. All app timestamps render in the user's LOCAL
// timezone.
//
// Jira Data Center emits timestamps like "2026-06-09T15:18:59.928+0100" — a
// numeric offset with no colon, which is NOT valid ISO 8601. WebView2's Date
// parser rejects it, so a naive `new Date(s)` returns Invalid Date and the raw
// "…+0100" string used to leak into the UI. parseJiraDate normalises a trailing
// ±HHMM offset to ±HH:MM first so the value parses, then the formatters below
// read it back in local time.

export function parseJiraDate(s: string): Date {
  // Insert a colon into a trailing numeric timezone offset (+0100 -> +01:00).
  return new Date(s.replace(/([+-]\d{2})(\d{2})$/, "$1:$2"));
}

const pad = (n: number) => String(n).padStart(2, "0");

// formatDateTime — dd/mm/YYYY HH:mm in local time. "—" for empty, the raw
// string only if genuinely unparseable.
export function formatDateTime(s?: string): string {
  if (!s) return "—";
  const d = parseJiraDate(s);
  if (isNaN(d.getTime())) return s;
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()} ${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;
}

// formatDate — local date only (locale-formatted).
export function formatDate(s?: string): string {
  if (!s) return "—";
  const d = parseJiraDate(s);
  return isNaN(d.getTime()) ? s : d.toLocaleDateString();
}

// formatDateTimeLong — local date + time (locale-formatted), for logs / history.
export function formatDateTimeLong(s?: string): string {
  if (!s) return "—";
  const d = parseJiraDate(s);
  return isNaN(d.getTime()) ? s : d.toLocaleString();
}
