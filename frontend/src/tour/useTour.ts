import { driver } from "driver.js";
import "driver.js/dist/driver.css";
import "./tour.css";
import { SetTourSeenVersion } from "../api";
import { TOURS, TOUR_VERSION } from "./steps";

interface Options {
  /** Called once the tour ends, whether completed or skipped. */
  onFinish: () => void;
}

// useTour builds the driver instance and returns a start function. It is not
// stateful React, just a factory, so it can be called from a menu handler or an
// effect without re-render concerns. `start` takes the view id whose tour to
// run, so the same handler serves every view's tour.
export function useTour({ onFinish }: Options) {
  function markSeen() {
    // Best-effort: a failed write means the tour is offered again next launch,
    // which is a far better failure than blocking the user inside it.
    SetTourSeenVersion(TOUR_VERSION).catch(() => {});
    onFinish();
  }

  // start runs the tour for `viewId`. onBeforeStart (optional) puts the app in
  // the state the steps assume — e.g. switching to Browse for the first-run
  // tour; on-demand launches run against the current view and pass nothing.
  function start(viewId: string, onBeforeStart?: () => void) {
    onBeforeStart?.();
    // The DOM has to settle after any onBeforeStart view switch, or the first
    // selectors resolve against the outgoing view.
    requestAnimationFrame(() => {
      const steps = (TOURS[viewId] ?? [])
        .map((s) => ({
          element: `[data-tour="${s.target}"]`,
          popover: {
            title: s.title,
            description: s.body,
            side: s.side ?? "bottom",
          },
        }))
        .filter((s) => document.querySelector(s.element) !== null);

      if (steps.length === 0) return;

      driver({
        showProgress: true,
        allowClose: true,
        nextBtnText: "Next",
        prevBtnText: "Back",
        doneBtnText: "Done",
        onDestroyed: markSeen,
        steps,
      }).drive();
    });
  }

  return { start };
}
