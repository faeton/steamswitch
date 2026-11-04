export type TileKeyIntent = "activate" | "detail" | "menu" | null;

type TileKey = Pick<KeyboardEvent, "key" | "shiftKey" | "target" | "currentTarget">;

/**
 * What a keydown on the row means.
 *
 * Only keys aimed at the row itself count. Enter on a control inside it bubbles here: treating
 * that as "activate" both swallowed the control's own click (preventDefault) and started a
 * switch, which closes Steam, from a key the user pressed to open a menu.
 *
 * Enter switches, as a deliberate key. Space does not: a single click now leaves the row focused
 * without switching, so a stray Space afterwards would otherwise close Steam. It shows the
 * account's details, which is what "select this" means in a list, and is harmless.
 */
export function tileKeyIntent(e: TileKey): TileKeyIntent {
  if (e.target !== e.currentTarget) return null;
  if (e.key === "Enter") return "activate";
  if (e.key === " ") return "detail";
  // The keyboard equivalent of right-click, for users who never reach for a mouse.
  if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) return "menu";
  return null;
}

export type RowClickIntent = "detail" | "switch" | null;

/**
 * What a `click` event on the row means.
 *
 * `detail` is the browser's click count. It is 0 when the click did not come from a pointer:
 * Enter on the focused row, a screen reader's "press", or the controller's activate button. Those
 * are deliberate, and they have no way to double-click. A pointer's click only ever selects; the
 * switch is on `dblclick`, so one stray click cannot close Steam.
 */
export function rowClickIntent(e: Pick<MouseEvent, "detail" | "shiftKey">): RowClickIntent {
  if (e.shiftKey) return "detail";
  return e.detail === 0 ? "switch" : null;
}

/** A Shift-double-click is two detail clicks, not a switch. */
export function rowDoubleClickSwitches(e: Pick<MouseEvent, "shiftKey">): boolean {
  return !e.shiftKey;
}
