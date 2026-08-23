import { useEffect, useRef } from "react";

// LiveRegion + announce() give the app a screen-reader channel for transient
// status that isn't a dialog (audit X4): loading finishing, bulk-operation
// results, connection-test outcomes, errors. Before this there were zero
// aria-live regions, so async results were silent to assistive tech.
//
// Mount <LiveRegion /> once at the app root, then call announce(msg) from
// anywhere. Errors should pass assertive=true so they interrupt.

let politeEl: HTMLElement | null = null;
let assertiveEl: HTMLElement | null = null;

export function announce(message: string, assertive = false): void {
  const el = assertive ? assertiveEl : politeEl;
  if (!el || !message) return;
  // Clearing first and setting on the next tick guarantees the region "changes"
  // even when the same message repeats, so it is re-announced each time.
  el.textContent = "";
  window.setTimeout(() => {
    if (el) el.textContent = message;
  }, 40);
}

export function LiveRegion() {
  const politeRef = useRef<HTMLDivElement>(null);
  const assertiveRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    politeEl = politeRef.current;
    assertiveEl = assertiveRef.current;
    return () => {
      politeEl = null;
      assertiveEl = null;
    };
  }, []);

  return (
    <>
      <div
        ref={politeRef}
        className="sr-only"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      />
      <div
        ref={assertiveRef}
        className="sr-only"
        role="alert"
        aria-live="assertive"
        aria-atomic="true"
      />
    </>
  );
}
