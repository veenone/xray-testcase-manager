import { useEffect, useRef } from "react";
import type { KeyboardEvent, ReactNode } from "react";

// Modal is the shared dialog primitive (audit X1/X2/X3). Every dialog in the app
// used to hand-roll its own `.modal-overlay` + `.modal` pair, which meant none of
// them trapped focus, restored focus on close, exposed a dialog role/name, or
// closed on Escape. This wrapper does all of that once so callers only supply the
// dialog's contents and a close handler.
//
// Migration is a mechanical swap: replace
//   <div className="modal-overlay" onClick={close}>
//     <div className="modal X" onClick={(e) => e.stopPropagation()}> … </div>
//   </div>
// with
//   <Modal onClose={close} className="modal X" labelledBy={titleId}> … </Modal>
// and give the dialog's <h2> an id matching `labelledBy`.

interface ModalProps {
  onClose: () => void;
  children: ReactNode;
  // Inner dialog container class(es). Defaults to "modal"; pass the modal's
  // existing specific classes (e.g. "modal bulk-modal") to preserve styling.
  className?: string;
  role?: "dialog" | "alertdialog";
  // id of the heading that names the dialog (preferred). Falls back to `label`.
  labelledBy?: string;
  label?: string;
  overlayClassName?: string;
  closeOnOverlayClick?: boolean;
  closeOnEsc?: boolean;
}

// Elements that can receive keyboard focus, for the Tab trap and initial focus.
const FOCUSABLE =
  'a[href], area[href], input:not([disabled]), select:not([disabled]), ' +
  'textarea:not([disabled]), button:not([disabled]), iframe, object, embed, ' +
  '[tabindex]:not([tabindex="-1"]), [contenteditable="true"]';

export function Modal({
  onClose,
  children,
  className = "modal",
  role = "dialog",
  labelledBy,
  label,
  overlayClassName = "modal-overlay",
  closeOnOverlayClick = true,
  closeOnEsc = true,
}: ModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  // The element focused before the dialog opened, restored on unmount so the
  // user lands back where they were instead of at the top of the page.
  const restoreRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    restoreRef.current = document.activeElement as HTMLElement | null;
    const node = dialogRef.current;
    if (node) {
      const first = node.querySelector<HTMLElement>(FOCUSABLE);
      (first ?? node).focus();
    }
    return () => {
      // Guard: the trigger may have unmounted while the dialog was open.
      restoreRef.current?.focus?.();
    };
  }, []);

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (closeOnEsc && e.key === "Escape") {
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== "Tab") return;
    const node = dialogRef.current;
    if (!node) return;
    const items = Array.from(
      node.querySelectorAll<HTMLElement>(FOCUSABLE),
    ).filter((el) => el.offsetParent !== null);
    if (items.length === 0) {
      e.preventDefault();
      node.focus();
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === node)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  }

  return (
    <div
      className={overlayClassName}
      onClick={closeOnOverlayClick ? onClose : undefined}
    >
      <div
        ref={dialogRef}
        className={className}
        role={role}
        aria-modal="true"
        aria-labelledby={labelledBy}
        aria-label={labelledBy ? undefined : label}
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        {children}
      </div>
    </div>
  );
}
