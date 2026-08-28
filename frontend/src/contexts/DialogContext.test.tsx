import React from "react";
import { describe, it, expect, vi } from "vitest";
import {
  render,
  screen,
  waitFor,
  renderHook,
  fireEvent,
} from "@testing-library/react";
import { DialogProvider, useDialogs } from "./DialogContext";
import { useConfirm } from "../components/useConfirm";

// LiveRegion.announce touches a module-level DOM node; stub it so the notice
// path doesn't depend on it in jsdom.
vi.mock("../components/LiveRegion", () => ({ announce: vi.fn() }));

describe("useDialogs", () => {
  it("throws when used outside a DialogProvider", () => {
    // Silence the expected error boundary console noise.
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() =>
      renderHook(() => useDialogs()),
    ).toThrow(/DialogProvider/);
    spy.mockRestore();
  });
});

describe("DialogProvider", () => {
  it("resolves confirm(true) when the confirm button is clicked", async () => {
    const onResult = vi.fn();

    function Trigger() {
      const { confirm } = useConfirm();
      return (
        <button
          onClick={async () => onResult(await confirm({ title: "Delete it?" }))}
        >
          go
        </button>
      );
    }

    render(
      <DialogProvider>
        <Trigger />
      </DialogProvider>,
    );

    fireEvent.click(screen.getByText("go"));
    // The dialog renders once at the provider root.
    expect(await screen.findByText("Delete it?")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onResult).toHaveBeenCalledWith(true));
    // Dialog is dismissed after resolving.
    expect(screen.queryByText("Delete it?")).not.toBeInTheDocument();
  });
});
