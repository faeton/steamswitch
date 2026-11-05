export type ViewportDropdownLayout = {
  placement: "above" | "below";
  maxHeight: number;
};

const VIEWPORT_MARGIN = 8;
const MAX_VIEWPORT_HEIGHT_RATIO = 0.6;

export function computeViewportDropdownLayout(
  trigger: Pick<DOMRect, "top" | "bottom">,
  options: { viewportHeight: number; menuHeight: number },
): ViewportDropdownLayout {
  const heightCap = Math.floor(options.viewportHeight * MAX_VIEWPORT_HEIGHT_RATIO);
  const desiredHeight = Math.min(options.menuHeight, heightCap);
  const availableAbove = Math.max(0, Math.floor(trigger.top - VIEWPORT_MARGIN));
  const availableBelow = Math.max(0, Math.floor(options.viewportHeight - trigger.bottom - VIEWPORT_MARGIN));
  const placement = desiredHeight <= availableBelow || availableBelow >= availableAbove ? "below" : "above";

  return {
    placement,
    maxHeight: Math.min(heightCap, placement === "above" ? availableAbove : availableBelow),
  };
}

/** True when a menu opening rightwards from its trigger would run past the edge it has to fit in. */
export function dropdownNeedsRightAlign(
  trigger: Pick<DOMRect, "left">,
  menuWidth: number,
  rightEdge: number,
): boolean {
  return trigger.left + menuWidth > rightEdge - VIEWPORT_MARGIN;
}

/** The right edge of whatever would scroll or clip this menu. */
function clippingRightEdge(node: HTMLElement): number {
  let edge = window.innerWidth || document.documentElement.clientWidth;
  for (let el = node.parentElement; el; el = el.parentElement) {
    const overflow = getComputedStyle(el);
    if (overflow.overflowX !== "visible" || overflow.overflowY !== "visible") {
      edge = Math.min(edge, el.getBoundingClientRect().right);
    }
  }
  return edge;
}

export function applyViewportDropdownLayout(
  style: Pick<CSSStyleDeclaration, "setProperty">,
  layout: ViewportDropdownLayout,
): void {
  style.setProperty("top", layout.placement === "below" ? "100%" : "auto", "important");
  style.setProperty("bottom", layout.placement === "above" ? "100%" : "auto", "important");
  style.setProperty("max-height", `${layout.maxHeight}px`);
}

export function viewportDropdown(node: HTMLElement) {
  let frame = 0;

  function measure(): void {
    frame = 0;
    const trigger = node.parentElement?.querySelector<HTMLElement>(".dropdown-toggle");
    if (!trigger) return;

    const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
    const layout = computeViewportDropdownLayout(trigger.getBoundingClientRect(), {
      viewportHeight,
      menuHeight: Math.max(node.scrollHeight, node.getBoundingClientRect().height),
    });

    applyViewportDropdownLayout(node.style, layout);

    const rightAlign = dropdownNeedsRightAlign(
      trigger.getBoundingClientRect(),
      node.getBoundingClientRect().width,
      clippingRightEdge(node),
    );
    node.style.setProperty("left", rightAlign ? "auto" : "0", "important");
    node.style.setProperty("right", rightAlign ? "0" : "auto", "important");
  }

  function schedule(): void {
    if (frame) cancelAnimationFrame(frame);
    frame = requestAnimationFrame(measure);
  }

  /**
   * Escape closes the open list and goes no further: the pages listen for Escape on window to
   * go back, so it used to leave Settings with the list still open. Every dropdown using this
   * action toggles from its `.dropdown-toggle`, so clicking that is the one way to close it.
   */
  function onKeydown(e: KeyboardEvent): void {
    if (e.key !== "Escape") return;
    const trigger = node.parentElement?.querySelector<HTMLElement>(".dropdown-toggle");
    if (!trigger) return;
    e.preventDefault();
    e.stopPropagation();
    trigger.click();
    trigger.focus();
  }

  measure();
  window.addEventListener("resize", schedule);
  window.addEventListener("scroll", schedule, true);
  window.addEventListener("keydown", onKeydown, true);

  return {
    destroy(): void {
      if (frame) cancelAnimationFrame(frame);
      window.removeEventListener("resize", schedule);
      window.removeEventListener("scroll", schedule, true);
      window.removeEventListener("keydown", onKeydown, true);
    },
  };
}
