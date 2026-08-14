import { describe, expect, it } from "vitest";
import { CLIPBOARD_CLEAR_SECONDS, copySecret, type ClipboardDeps } from "./clipboardSecret";

type Harness = {
  deps: ClipboardDeps;
  writes: string[];
  fire: () => void;
  delay: number | null;
};

function harness(over: Partial<ClipboardDeps> = {}, clipboard = ""): Harness {
  const writes: string[] = [];
  let current = clipboard;
  let pending: (() => void) | null = null;
  const h: Harness = {
    writes,
    delay: null,
    fire: () => pending?.(),
    deps: {
      clearEnabled: () => true,
      writeText: async (v) => {
        current = v;
        writes.push(v);
      },
      readText: async () => current,
      setTimer: (fn, ms) => {
        pending = fn;
        h.delay = ms;
        return 1;
      },
      ...over,
    },
  };
  return h;
}

const settle = (): Promise<void> => new Promise((r) => setTimeout(r, 0));

describe("copySecret", () => {
  it("writes the secret and schedules the scrub", async () => {
    const h = harness();
    const outcome = await copySecret("hunter2", h.deps);

    expect(outcome).toBe("copied-will-clear");
    expect(h.writes).toEqual(["hunter2"]);
    expect(h.delay).toBe(CLIPBOARD_CLEAR_SECONDS * 1000);
  });

  it("clears the clipboard when the scrub fires", async () => {
    const h = harness();
    await copySecret("hunter2", h.deps);
    h.fire();
    await settle();

    expect(h.writes).toEqual(["hunter2", ""]);
  });

  it("leaves a clipboard the user has since replaced", async () => {
    // Wiping something the user copied afterwards would be its own small betrayal.
    const h = harness();
    await copySecret("hunter2", h.deps);
    await h.deps.writeText("a shopping list");
    h.fire();
    await settle();

    expect(h.writes).toEqual(["hunter2", "a shopping list"]);
  });

  it("clears anyway when the clipboard cannot be read back", async () => {
    // The real build denies clipboard read permission, and the earlier inline version
    // swallowed that rejection and cleared nothing at all.
    const h = harness({
      readText: () => Promise.reject(new Error("PermissionClipboardRead: PermissionDeny")),
    });
    await copySecret("hunter2", h.deps);
    h.fire();
    await settle();

    expect(h.writes).toEqual(["hunter2", ""]);
  });

  it("honours the user turning the policy off", async () => {
    const h = harness({ clearEnabled: () => false });
    const outcome = await copySecret("hunter2", h.deps);

    expect(outcome).toBe("copied");
    expect(h.delay).toBeNull();
  });

  it("reports a refused clipboard instead of claiming a copy", async () => {
    const h = harness({ writeText: () => Promise.reject(new Error("denied")) });
    const outcome = await copySecret("hunter2", h.deps);

    expect(outcome).toBe("refused");
    expect(h.delay).toBeNull();
  });

  it("does not schedule a scrub for an empty value", async () => {
    const h = harness();
    const outcome = await copySecret("", h.deps);

    expect(outcome).toBe("refused");
    expect(h.writes).toEqual([]);
  });
});
