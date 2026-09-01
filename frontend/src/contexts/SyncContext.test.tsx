import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { SyncProvider, useSync } from "./SyncContext";
import * as api from "../api";
import type { SyncProgress } from "../api";

// Capture the EventsOn handlers so tests can push progress frames the way the
// Go backend would. The capturing implementation is installed in beforeEach
// (the mock factory is hoisted above `handlers`, so it can't reference it).
const handlers: Record<string, (p: SyncProgress) => void> = {};
vi.mock("../api", () => ({ EventsOn: vi.fn(() => () => {}) }));

const frame = (over: Partial<SyncProgress> = {}): SyncProgress => ({
  phase: "tests",
  fetched: 1,
  total: 10,
  done: false,
  ...over,
});

function wrapper({ children }: { children: React.ReactNode }) {
  return <SyncProvider>{children}</SyncProvider>;
}

beforeEach(() => {
  for (const k of Object.keys(handlers)) delete handlers[k];
  (api.EventsOn as ReturnType<typeof vi.fn>).mockReset();
  (api.EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
    (event: string, cb: (p: SyncProgress) => void) => {
      handlers[event] = cb;
      return () => {};
    },
  );
});

describe("useSync", () => {
  it("throws when used outside a SyncProvider", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useSync())).toThrow(/SyncProvider/);
    spy.mockRestore();
  });
});

describe("SyncProvider", () => {
  it("starts idle: everything permitted", () => {
    const { result } = renderHook(() => useSync(), { wrapper });
    expect(result.current.status).toBe("idle");
    expect(result.current.canSync).toBe(true);
    expect(result.current.canCommit).toBe(true);
    expect(result.current.canSwitchProfile).toBe(true);
    expect(result.current.pulling).toBe(false);
  });

  it("beginSync locks sync + commit and raises the button flag; endSync releases", () => {
    const { result } = renderHook(() => useSync(), { wrapper });
    act(() => result.current.beginSync({ clearError: true }));
    expect(result.current.status).toBe("syncing");
    expect(result.current.pulling).toBe(true);
    expect(result.current.canSync).toBe(false);
    expect(result.current.canCommit).toBe(false);
    expect(result.current.canSwitchProfile).toBe(false);
    act(() => result.current.endSync());
    expect(result.current.status).toBe("idle");
    expect(result.current.canSwitchProfile).toBe(true);
  });

  it("beginCommit locks sync/commit but NOT profile switching", () => {
    const { result } = renderHook(() => useSync(), { wrapper });
    act(() => result.current.beginCommit());
    expect(result.current.status).toBe("committing");
    expect(result.current.canCommit).toBe(false);
    expect(result.current.canSwitchProfile).toBe(true);
    act(() => result.current.endCommit());
    expect(result.current.status).toBe("idle");
  });

  it("routes sync:progress frames through the reducer (terminal frame releases the button, keeps the lock for the tail)", () => {
    const { result } = renderHook(() => useSync(), { wrapper });
    act(() => result.current.beginSync({ clearError: true }));

    act(() => handlers["sync:progress"](frame({ phase: "folders", fetched: 4 })));
    expect(result.current.progress).toEqual(frame({ phase: "folders", fetched: 4 }));

    act(() => handlers["sync:progress"](frame({ done: true })));
    expect(result.current.pulling).toBe(false); // button re-enabled
    expect(result.current.progress).toBeNull();
    expect(result.current.status).toBe("syncing"); // tail still locks the picker
    expect(result.current.canSwitchProfile).toBe(false);
  });

  it("routes spellcheck:progress to the bar without touching the sync lifecycle", () => {
    const { result } = renderHook(() => useSync(), { wrapper });
    act(() =>
      handlers["spellcheck:progress"](frame({ phase: "spellcheck", fetched: 7 })),
    );
    expect(result.current.status).toBe("idle");
    expect(result.current.progress).toEqual(
      frame({ phase: "spellcheck", fetched: 7 }),
    );
    // A spellcheck bar still locks the profile switcher (preserved quirk).
    expect(result.current.canSwitchProfile).toBe(false);
  });
});
