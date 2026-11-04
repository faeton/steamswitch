import { writable, type Readable } from "svelte/store";
import { Events } from "@wailsio/runtime";
import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
import { asSignInStatus, NO_SIGN_IN, type SignInStatus } from "../lib/steam/signIn";

const internal = writable<SignInStatus>(NO_SIGN_IN);

/** Steam's latest answer on the last launch. Lives here so it survives a page change. */
export const signInStatus: Readable<SignInStatus> = { subscribe: internal.subscribe };

/** Read the current answer directly, in case an event went by while nobody was listening. */
export async function refreshSignInStatus(): Promise<SignInStatus> {
  try {
    const status = asSignInStatus(await SteamService.GetSignInStatus());
    internal.set(status);
    return status;
  } catch {
    return NO_SIGN_IN;
  }
}

export function initSignInStatus(): () => void {
  void refreshSignInStatus();
  return Events.On("steam-signin-status", (ev) => internal.set(asSignInStatus(ev.data)));
}
