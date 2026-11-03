export type TileKeyIntent = "activate" | "menu" | null;

type TileKey = Pick<KeyboardEvent, "key" | "shiftKey" | "target" | "currentTarget">;

/**
 * What a keydown on the tile means.
 *
 * Only keys aimed at the tile itself count. The tile contains its own ⋯ button, and Enter
 * on that button bubbles here: treating it as "activate" both swallowed the button's click
 * (preventDefault) and started a switch, which closes Steam — from a key the user pressed
 * to open a menu.
 */
export function tileKeyIntent(e: TileKey): TileKeyIntent {
  if (e.target !== e.currentTarget) return null;
  if (e.key === "Enter" || e.key === " ") return "activate";
  // The keyboard equivalent of right-click, for users who never reach for a mouse.
  if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) return "menu";
  return null;
}
