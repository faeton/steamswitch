import { get } from "svelte/store";
import { afterEach, describe, expect, it } from "vitest";
import { activeModal, dismissModal, openConfirm } from "./modal";

describe("openConfirm", () => {
  afterEach(() => dismissModal(false));

  it("marks a destroying confirmation so its button is drawn as one", () => {
    void openConfirm({ title: "Delete vault entry", destructive: true });
    const m = get(activeModal);
    expect(m?.kind).toBe("confirm");
    expect(m && m.kind === "confirm" && m.destructive).toBe(true);
  });

  it("leaves an ordinary confirmation in the accent colour", () => {
    void openConfirm({ title: "Add another Steam login" });
    const m = get(activeModal);
    expect(m && m.kind === "confirm" && m.destructive).toBe(false);
  });
});
