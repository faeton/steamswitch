import { derived, get, writable, type Readable, type Writable } from "svelte/store";
import { pushToast } from "../stores/toast";
import { formatWailsError } from "./formatWailsError";
import { t } from "../stores/i18n";

export interface ToggleController extends Readable<{ value: boolean; loading: boolean }> {
  value: Writable<boolean>;
  loading: Writable<boolean>;
  init: () => Promise<void>;
  toggle: () => Promise<void>;
}

/**
 * Creates a reactive toggle for a boolean application setting.
 *
 * Success is silent (REDESIGN.md §3). A checkbox that fires "Saved X" every time it is
 * clicked teaches people to dismiss toasts without reading them, which is exactly the habit
 * that makes the ones that matter — a failed switch, an active kit — get missed. The control
 * moving is the confirmation.
 *
 * Failure still speaks, because the control will have moved either way and would otherwise be
 * showing a state that is not on disk.
 *
 * @param getter  Function that fetches the current state from the backend.
 * @param setter  Function that persists the new state to the backend.
 * @param label   Kept for callers that pass a label; no longer used for a success toast.
 * @param guard   Optional guard function — if it returns false, the toggle is blocked.
 */
export function createToggle(
  getter: () => Promise<boolean>,
  setter: (value: boolean) => Promise<void>,
  label?: string,
  guard?: () => boolean,
): ToggleController {
  const value = writable(false);
  const loading = writable(false);

  const store = derived([value, loading], ([$value, $loading]) => ({
    value: $value,
    loading: $loading,
  }));

  const toggle = async () => {
    if (get(loading) || (guard && !guard())) return;
    const next = !get(value);
    loading.set(true);
    try {
      await setter(next);
      value.set(next);
    } catch (e) {
      pushToast({
        type: "error",
        message: formatWailsError(e),
        duration: 8000,
      });
    } finally {
      loading.set(false);
    }
  };

  return {
    subscribe: store.subscribe,
    value,
    loading,
    init: async () => {
      try {
        value.set(await getter());
      } catch {
        value.set(false);
      }
    },
    toggle,
  };
}
