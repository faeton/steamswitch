<script lang="ts">
  /**
   * An account's profile picture, sized and clipped by itself.
   *
   * This component used to ship **no CSS at all**: its `.steam-acc-avatar-wrap` /
   * `.steam-acc-avatar` rules existed only under the `label.acc` selector in
   * `styles/acclist.scss`, which the redesigned tile never uses. The result was a raw
   * intrinsic-size image (64px and up) rendered inside a 36px box with no clip, plus a frame
   * overlay carrying `transform: scale(1.22)` — brief Part B #1, the app's most visible defect.
   *
   * The fix is self-containment, not restyling: the box is a CSS variable the caller sets, and
   * every state — image, animated video, missing file, VAC/limited outline, decorative frame —
   * is clipped to exactly that box. The `label.acc` rules stay where they are for the legacy
   * surfaces that still render through them.
   */
  import { offlineMode, offlineSafeImageSrc, withAssetCacheBust } from "../stores/offlineMode";
  import { isProfileVideoUrl } from "../lib/profileImageDrop";
  import { miniProfileHover } from "../lib/actions/miniProfileHover";
  import type { SteamAccountRow } from "../lib/steam/types";

  export let account: SteamAccountRow;
  export let epoch = 0;
  export let fallback = "";
  export let boundary: HTMLElement | null = null;
  /**
   * Whether hovering pops the Steam miniprofile.
   *
   * Off by default now. The brief retires the hover card on the dense list (Part B #4, A14):
   * it injected a ~328px chunk of Steam's own markup with an 80px avatar into a list whose
   * rows are 42px, and account preview moved to click→detail. The detail panel opts back in.
   */
  export let allowMiniProfile = false;

  /** True once the browser has failed to load `avatarSrc`. */
  let broken = false;

  function steamListAvatarUrl(): string | undefined {
    const acc = account;
    if (acc.avatarPending) return undefined;
    const primary = acc.imageUrl?.trim() || undefined;
    const fb = acc.staticImageUrl?.trim() || undefined;
    if ($offlineMode) {
      if (fb) return fb;
      if (primary && !isProfileVideoUrl(primary)) return primary;
      return undefined;
    }
    return primary ?? fb;
  }

  $: avatarSrc = offlineSafeImageSrc(
    $offlineMode,
    withAssetCacheBust(steamListAvatarUrl(), epoch),
    fallback,
  );
  // Reset the broken flag whenever the source changes, so a refreshed avatar gets a fresh
  // chance rather than staying stuck on the initials for the rest of the session.
  $: if (avatarSrc) broken = false;
  $: avatarIsVideo = !$offlineMode && isProfileVideoUrl(avatarSrc);
  $: hoverEnabled =
    allowMiniProfile && !!(account.showMiniProfile && (account.miniProfileHtml ?? "").trim() !== "");

  /**
   * Two letters from the display name, for when there is no picture to show.
   *
   * A generic placeholder icon on every unloaded row makes a list unreadable while avatars
   * stream in; initials keep each row identifiable from the first paint.
   */
  $: initials = (() => {
    const name = (account.displayName || account.personaName || account.accountName || "").trim();
    if (!name) return "··";
    const words = name.split(/\s+/).filter(Boolean);
    const letters =
      words.length > 1 ? `${words[0][0]}${words[1][0]}` : name.slice(0, 2);
    return letters.toUpperCase();
  })();
</script>

<span class="avatar">
  {#if !avatarSrc || broken}
    <span class="avatar__fallback" aria-hidden="true">{initials}</span>
  {:else if avatarIsVideo}
    <video
      class="avatar__media"
      class:avatar__media--vac={account.showVac && account.vac}
      class:avatar__media--limited={account.showLimited && account.ltd}
      src={avatarSrc}
      autoplay
      loop
      muted
      playsinline
      aria-hidden="true"
      draggable="false"
      on:error={() => (broken = true)}
      use:miniProfileHover={{
        html: account.miniProfileHtml ?? "",
        boundary,
        offline: $offlineMode,
        enabled: hoverEnabled,
      }}
    ></video>
  {:else}
    <img
      class="avatar__media"
      class:avatar__media--vac={account.showVac && account.vac}
      class:avatar__media--limited={account.showLimited && account.ltd}
      src={avatarSrc}
      alt=""
      draggable="false"
      on:error={() => (broken = true)}
      use:miniProfileHover={{
        html: account.miniProfileHtml ?? "",
        boundary,
        offline: $offlineMode,
        enabled: hoverEnabled,
      }}
    />
  {/if}

  {#if account.showAvatarFrame && (account.avatarFrameUrl ?? "").trim() !== "" && !$offlineMode}
    <img
      class="avatar__frame"
      src={offlineSafeImageSrc($offlineMode, account.avatarFrameUrl ?? "", fallback)}
      alt=""
      draggable="false"
    />
  {/if}
</span>

<style>
  /*
    `--avatar-size` is the contract: the caller states the box, everything here fits inside
    it. No intrinsic image size ever escapes, which is the whole bug this fixes.
  */
  .avatar {
    position: relative;
    flex: 0 0 auto;
    display: block;
    width: var(--avatar-size, 42px);
    height: var(--avatar-size, 42px);
    border-radius: var(--avatar-radius, var(--radius-md, 6px));
    overflow: hidden;
    background: var(--button-bg);
    line-height: 0;
  }

  .avatar__media {
    display: block;
    width: 100%;
    height: 100%;
    /* `cover`, so a non-square profile picture crops rather than letterboxing or stretching. */
    object-fit: cover;
    border-radius: inherit;
    -webkit-user-drag: none;
  }

  /*
    VAC and limited outlines are drawn *inside* the box with an inset shadow rather than a
    border. A border would either be clipped away by `overflow: hidden` or push the media out
    of the box — the exact class of bug this file exists to fix.
  */
  .avatar__media--vac {
    box-shadow: inset 0 0 0 2px var(--red);
  }

  .avatar__media--limited {
    box-shadow: inset 0 0 0 2px var(--yellow, var(--orange));
  }

  .avatar__fallback {
    display: grid;
    place-items: center;
    width: 100%;
    height: 100%;
    border-radius: inherit;
    background: var(--button-bg);
    color: var(--fg-muted);
    font-family: var(--font-mono, monospace);
    font-size: calc(var(--avatar-size, 42px) * 0.32);
    line-height: 1;
    letter-spacing: 0.04em;
  }

  /*
    The frame is decorative and *contained*, not scaled past the box.
    It used to carry `transform: scale(1.22)`, which is what made frames spill across the row.
    Steam draws its frames with built-in padding, so `contain` inside the same box is right.
  */
  .avatar__frame {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    border-radius: inherit;
    pointer-events: none;
  }
</style>
