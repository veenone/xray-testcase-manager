import { driver } from "driver.js";
import "driver.js/dist/driver.css";
import "./tour.css";
import { SetTourSeenVersion } from "../api";
import { TOUR_STEPS, TOUR_VERSION } from "./steps";

interface Options {
  /** Called before the first step, to put the app in a state the steps assume. */
  onBeforeStart: () => void;
  /** Called once the tour ends, whether completed or skipped. */
  onFinish: () => void;
}

// useTour builds the driver instance and returns a start function. It is not
// stateful React, just a factory, so it can be called from a menu handler or an
// effect without re-render concerns.
export function useTour({ onBeforeStart, onFinish }: Options) {
  function markSeen() {
    // Best-effort: a failed write means the tour is offered again next launch,
    // which is a far better failure than blocking the user inside it.
    SetTourSeenVersion(TOUR_VERSION).catch(() => {});
    onFinish();
  }

  function start() {
    onBeforeStart();
    // The DOM has to settle after onBeforeStart switches views, or the first
    // selectors resolve against the outgoing view.
    requestAnimationFrame(() => {
      const steps = TOUR_STEPS.map((s) => ({
        element: `[data-tour="${s.target}"]`,
        popover: {
          title: s.title,
          description: s.body,
          side: s.side ?? "bottom",
        },
      })).filter((s) => document.querySelector(s.element) !== null);

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
