/**
 * The per-account menu, on a diet (REDESIGN.md §3).
 *
 * At most eight rows on the top level; everything the inherited menu carried that is not a
 * daily action moves under `Advanced ▸`. The commands themselves are unchanged — this is a
 * restructuring of `MenuItemDef` trees, not a removal of capability.
 */
import type { MenuItemDef } from "../../stores/contextMenu";
import type { SteamAccountRow } from "./types";
import { canMarkShared, roleOf, type AccountRoleMap } from "./accountRoles";
import { buildTagsSectionMenuItem, type TagDefRow } from "../accountTagsContext";
import * as SteamService from "../../../bindings/steamswitch/internal/steam/steamservice.js";
import * as BasicService from "../../../bindings/steamswitch/internal/basic/basicservice.js";

type Translate = (key: string, vars?: Record<string, string | number>) => string;

export type AccountMenuDeps = {
  account: SteamAccountRow;
  label: string;
  roles: AccountRoleMap;
  /** True while a switch or recovery is in flight; mutating rows are disabled. */
  blocked: boolean;
  /**
   * Every tag defined for Steam, for the "Tags ▸ Add" list. Loaded by the page, because the
   * menu builder is synchronous and the definitions live behind a service call. An empty
   * array still gives a working menu — you can create a tag by typing a new name.
   */
  tagDefs: TagDefRow[];
  t: Translate;
  onSwitch: () => void;
  /** Switch with an explicit persona state; must go through the same gate as `onSwitch`. */
  onSwitchAs: (personaState: number) => void;
  onRolesChanged: (next: AccountRoleMap) => void;
  /** Open a Tools route, used by the rows that were demoted out of the main menu. */
  onNavigate: (page: string) => void;
  onAccountsChanged: () => void;
  onError: (message: string, err: unknown) => void;
  onToast: (message: string) => void;
};

async function applyRoleChange(
  deps: AccountMenuDeps,
  call: () => Promise<{ homeSteamId64: string; sharedIds: string[] }>,
  failKey: string,
): Promise<void> {
  try {
    const next = await call();
    deps.onRolesChanged({
      homeSteamId64: next.homeSteamId64 ?? "",
      sharedIds: next.sharedIds ?? [],
    });
  } catch (e) {
    deps.onError(deps.t(failKey), e);
  }
}

export function buildAccountMenu(deps: AccountMenuDeps): MenuItemDef[] {
  const { account, roles, blocked, t } = deps;
  const id = account.steamId64;
  const role = roleOf(roles, id);
  const isCurrent = account.currentSession === true;

  const personaStates = [
    { state: 1, label: t("Online") },
    { state: 7, label: t("Invisible") },
    { state: 0, label: t("Offline") },
    { state: 2, label: t("Busy") },
    { state: 3, label: t("Away") },
    { state: 4, label: t("Snooze") },
    { state: 5, label: t("LookingToTrade") },
    { state: 6, label: t("LookingToPlay") },
  ];

  return [
    {
      label: t("Button_Login"),
      disabled: blocked || isCurrent,
      action: deps.onSwitch,
    },
    { type: "separator" },
    {
      label: t("Menu_SetAsHome"),
      // Marking the current Home as Home again is a no-op; shared accounts are ineligible.
      disabled: blocked || role === "home" || role === "shared",
      action: () =>
        void applyRoleChange(deps, () => SteamService.SetHomeAccount(id), "Toast_Roles_SetHomeFail"),
    },
    {
      label: role === "shared" ? t("Menu_UnmarkShared") : t("Menu_MarkShared"),
      disabled: blocked || !canMarkShared(roles, id),
      action: () =>
        void applyRoleChange(
          deps,
          () => SteamService.SetAccountShared(id, role !== "shared"),
          "Toast_Roles_SetSharedFail",
        ),
    },
    { type: "separator" },
    {
      label: t("Context_CopyIds"),
      children: [
        { label: t("Context_CommunityUrl"), action: () => void copyCommunityUrl(deps) },
        { label: t("Context_Steam_Id64"), action: () => void copyId(deps, "ID64") },
        { label: t("Context_Steam_Id3"), action: () => void copyId(deps, "ID3") },
        { label: t("Context_Steam_Id32"), action: () => void copyId(deps, "ID32") },
        { label: t("Context_LoginUsername"), action: () => void copyText(deps, account.accountName ?? "") },
      ],
    },
    {
      label: t("Context_Steam_OpenUserdata"),
      action: () => void openUserdata(deps),
    },
    { type: "separator" },
    {
      // Everything below is reachable but off the daily path (REDESIGN.md §3). These rows
      // were unreachable entirely after the platform pages were deleted; they are demoted
      // here rather than dropped.
      label: t("Menu_Advanced"),
      children: [
        {
          label: t("Context_LoginAsSubmenu"),
          disabled: blocked,
          children: [
            { type: "search", label: t("Context_Search") },
            ...personaStates.map((p) => ({
              label: p.label,
              action: () => void loginAs(deps, p.state),
            })),
          ],
        },
        {
          label: t("Context_CreateShortcut"),
          children: [
            { type: "search", label: t("Context_Search") },
            ...personaStates.map((p) => ({
              label: p.label,
              action: () => void createShortcut(deps, p.state, p.label),
            })),
          ],
        },
        { type: "separator" },
        // Tags survived the redesign as a complete subsystem — Go service, expiry modal,
        // bubbles on the tile — with nothing left that opened it. This is the way back in.
        buildTagsSectionMenuItem({
          platformKey: "Steam",
          uniqueId: id,
          assignedTags: (account.tags ?? []) as TagDefRow[],
          tagDefs: deps.tagDefs,
          tr: t,
          afterChange: () => deps.onAccountsChanged(),
          onSuccess: () => {},
          onError: (e) => deps.onError(t("Toast_SaveFailed"), e),
        }),
        {
          label: t("Context_ChangeName"),
          action: () => void renameAccount(deps),
        },
        {
          label: t("Context_AccountNote"),
          action: () => void editNote(deps),
        },
        {
          label: t("Context_ChangeImage"),
          action: () => void changeImage(deps),
        },
        { type: "separator" },
        {
          label: t("Context_Steam_OpenGameData"),
          action: () => void openGameData(deps),
        },
        {
          label: t("Kit_Settings"),
          action: () => deps.onNavigate("settings"),
        },
        { type: "separator" },
        {
          label: t("Context_ForgetAccount"),
          disabled: blocked,
          action: () => void forgetAccount(deps),
        },
      ],
    },
  ];
}

async function createShortcut(
  deps: AccountMenuDeps,
  personaState: number,
  stateLabel: string,
): Promise<void> {
  const Shortcuts = await import("../../../bindings/steamswitch/internal/shortcuts/service.js");
  try {
    await Shortcuts.CreateAccountShortcut(
      "Steam",
      deps.account.steamId64,
      deps.label,
      String(personaState),
      stateLabel,
      deps.account.accountName ?? "",
    );
    deps.onToast(deps.t("Toast_ShortcutCreated"));
  } catch (e) {
    deps.onError(deps.t("Toast_SwitchFailed"), e);
  }
}

async function editNote(deps: AccountMenuDeps): Promise<void> {
  const { openPrompt } = await import("../../stores/modal");
  let current = "";
  try {
    current = await BasicService.GetAccountNote("Steam", deps.account.steamId64);
  } catch {
    // A missing note is the common case, not an error worth blocking the editor for.
  }
  const next = await openPrompt({
    title: deps.t("Context_AccountNote"),
    initialValue: current,
    multiline: true,
  });
  if (next == null) return;
  try {
    await BasicService.SetAccountNote("Steam", deps.account.steamId64, next);
    deps.onAccountsChanged();
  } catch (e) {
    deps.onError(deps.t("Toast_SaveFailed"), e);
  }
}

async function changeImage(deps: AccountMenuDeps): Promise<void> {
  const { openFolderPicker } = await import("../../stores/modal");
  const path = await openFolderPicker({ title: deps.t("Context_ChangeImage"), dirsOnly: false });
  if (!path) return;
  try {
    await BasicService.ChangeAccountImage("Steam", deps.account.steamId64, path);
    deps.onAccountsChanged();
  } catch (e) {
    deps.onError(deps.t("Toast_SaveFailed"), e);
  }
}

async function openGameData(deps: AccountMenuDeps): Promise<void> {
  try {
    await SteamService.OpenUserdataFolder(deps.account.steamId64);
  } catch (e) {
    deps.onError(deps.t("Toast_NoFindSteamUserdata"), e);
  }
}

async function copyText(deps: AccountMenuDeps, value: string): Promise<void> {
  const text = (value ?? "").trim();
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
  } catch (e) {
    deps.onError(deps.t("Toast_CopyFailed"), e);
  }
}

async function copyCommunityUrl(deps: AccountMenuDeps): Promise<void> {
  await copyText(deps, `https://steamcommunity.com/profiles/${deps.account.steamId64}`);
}

async function copyId(deps: AccountMenuDeps, format: "ID64" | "ID3" | "ID32"): Promise<void> {
  try {
    const formats = await SteamService.GetSteamIDFormats(deps.account.steamId64);
    const value =
      format === "ID64" ? formats.ID64 : format === "ID3" ? formats.ID3 : formats.ID32;
    await copyText(deps, value ?? "");
  } catch (e) {
    deps.onError(deps.t("Toast_CopyFailed"), e);
  }
}

async function openUserdata(deps: AccountMenuDeps): Promise<void> {
  try {
    await SteamService.OpenUserdataFolder(deps.account.steamId64);
  } catch (e) {
    deps.onError(deps.t("Toast_NoFindSteamUserdata"), e);
  }
}

/**
 * "Log in as ▸ <state>" — a switch with an explicit persona state.
 *
 * Routed through `onSwitchAs` rather than calling the backend directly. Calling
 * `SwapToSteamAccount` here bypassed both the single-flight gate in `beginSwitch` and the
 * Session Kit entirely, so it could start a second concurrent swap against the same live
 * Steam files, and it could leave a shared account without ever offering to restore the
 * other person's setup.
 */
async function loginAs(deps: AccountMenuDeps, personaState: number): Promise<void> {
  deps.onSwitchAs(personaState);
}

async function renameAccount(deps: AccountMenuDeps): Promise<void> {
  // The prompt itself lives with the page; the menu only carries the intent.
  const { openPrompt } = await import("../../stores/modal");
  const next = await openPrompt({
    title: deps.t("Modal_ChangeUsername"),
    initialValue: deps.label,
  });
  if (next == null) return;
  const trimmed = next.trim();
  if (!trimmed || trimmed === deps.label) return;
  try {
    await BasicService.RenameAccount("Steam", deps.account.steamId64, trimmed);
    deps.onAccountsChanged();
  } catch (e) {
    deps.onError(deps.t("Toast_RenameAccountFail"), e);
  }
}

async function forgetAccount(deps: AccountMenuDeps): Promise<void> {
  const { openConfirm } = await import("../../stores/modal");
  const ok = await openConfirm({
    title: deps.t("Modal_ConfirmAction"),
    body: deps.t("Modal_ForgetAccount", { name: deps.label }),
    style: "yesno",
  });
  if (!ok) return;
  try {
    await SteamService.ForgetSteamAccount(deps.account.steamId64);
    deps.onAccountsChanged();
  } catch (e) {
    deps.onError(deps.t("Toast_ForgetAccountFail"), e);
  }
}
