import { afterEach, describe, expect, it, vi } from "vitest";
import { modalFocus } from "./modalFocus";

/** The action only needs a listener slot and a tabindex; no DOM library in this project. */
function fakeDialog() {
  let keydown: ((e: KeyboardEvent) => void) | undefined;
  const node = {
    tabIndex: 0,
    hasAttribute: () => true,
    addEventListener: (type: string, fn: (e: KeyboardEvent) => void) => {
      if (type === "keydown") keydown = fn;
    },
    removeEventListener: () => {},
    querySelector: () => null,
    querySelectorAll: () => [],
    contains: () => true,
    focus: () => {},
  };
  return { node: node as unknown as HTMLElement, press: (e: KeyboardEvent) => keydown?.(e) };
}

describe("modalFocus", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("spends Escape on the dialog, so it cannot reach the page behind it", () => {
    vi.stubGlobal("HTMLElement", class {});
    vi.stubGlobal("document", { activeElement: null });
    vi.stubGlobal("requestAnimationFrame", () => 0);
    vi.stubGlobal("queueMicrotask", () => {});

    const { node, press } = fakeDialog();
    const onEscape = vi.fn();
    modalFocus(node, { onEscape });

    const stopPropagation = vi.fn();
    press({ key: "Escape", preventDefault: () => {}, stopPropagation } as unknown as KeyboardEvent);

    expect(onEscape).toHaveBeenCalledOnce();
    expect(stopPropagation).toHaveBeenCalledOnce();
  });
});
