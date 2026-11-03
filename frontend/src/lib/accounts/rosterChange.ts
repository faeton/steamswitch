/** Whether two reads of the switcher list name the same accounts, ignoring order. */
export function sameRoster(before: readonly string[], after: readonly string[]): boolean {
  if (before.length !== after.length) return false;
  const seen = new Set(before);
  return after.every((id) => seen.has(id));
}
