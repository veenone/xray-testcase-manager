import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useReducer,
} from "react";
import type { ReactNode } from "react";

// ModalContext collapses App.tsx's ~16 modal-visibility booleans into one
// reducer (spec §5.5). Only one of these overlay modals is ever open at a time
// — they are all root-level blocking overlays, and the one case that opens a
// sibling (the bridge wizard → connections) closes itself first — so a single
// `openModal: ModalId | null` faithfully replaces the booleans.
//
// The New Test panel (showNewTest) is deliberately NOT here: it shares the
// detail slot with TestDetail rather than being an overlay, so it can be open
// alongside a modal. It lives in NavContext. `editingProfile` (the form's
// edit-target) stays App-local — it is meaningful only with the form modal and
// has no bearing on the visibility invariant this reducer models.

export type ModalId =
  | "form"
  | "profiles"
  | "connections"
  | "bridge"
  | "pending"
  | "bulkEdit"
  | "bulkTransition"
  | "bulkAllocate"
  | "bulkMove"
  | "bulkPreconditions"
  | "bulkRequirements"
  | "bulkReview"
  | "diagnostics"
  | "about"
  | "syncHistory"
  | "import";

type ModalAction =
  | { type: "OPEN"; id: ModalId }
  | { type: "CLOSE" };

function modalReducer(state: ModalId | null, action: ModalAction): ModalId | null {
  switch (action.type) {
    case "OPEN":
      return action.id;
    case "CLOSE":
      return null;
    default:
      return state;
  }
}

interface ModalApi {
  current: ModalId | null;
  isOpen: (id: ModalId) => boolean;
  openModal: (id: ModalId) => void;
  closeModal: () => void;
}

const ModalContext = createContext<ModalApi | null>(null);

export function useModal(): ModalApi {
  const ctx = useContext(ModalContext);
  if (!ctx) {
    throw new Error("useModal must be used within a ModalProvider");
  }
  return ctx;
}

export function ModalProvider({ children }: { children: ReactNode }) {
  const [current, dispatch] = useReducer(modalReducer, null);

  const isOpen = useCallback((id: ModalId) => current === id, [current]);
  const openModal = useCallback(
    (id: ModalId) => dispatch({ type: "OPEN", id }),
    [],
  );
  const closeModal = useCallback(() => dispatch({ type: "CLOSE" }), []);

  const api = useMemo<ModalApi>(
    () => ({ current, isOpen, openModal, closeModal }),
    [current, isOpen, openModal, closeModal],
  );

  return <ModalContext.Provider value={api}>{children}</ModalContext.Provider>;
}
