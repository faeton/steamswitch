<script lang="ts" context="module">
  /**
   * The app's small icon set: one 16px grid, one 1.5px stroke, round caps.
   *
   * Exists so controls stop borrowing text glyphs for icons — `⋯`, `⧉` and `＋` render in
   * whatever font the platform falls back to, at a weight and baseline that matches nothing
   * around them. Add a glyph here rather than inlining another SVG in a component.
   */
  export type IconName = "more" | "plus" | "kit" | "keyboard" | "play" | "refresh" | "warn" | "lock";
</script>

<script lang="ts">
  export let name: IconName;
  export let size = 16;
</script>

<svg
  class="icon"
  width={size}
  height={size}
  style:width="{size}px"
  style:height="{size}px"
  viewBox="0 0 16 16"
  fill="none"
  stroke="currentColor"
  stroke-width="1.5"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {#if name === "more"}
    <circle cx="3.5" cy="8" r="0.6" fill="currentColor" />
    <circle cx="8" cy="8" r="0.6" fill="currentColor" />
    <circle cx="12.5" cy="8" r="0.6" fill="currentColor" />
  {:else if name === "plus"}
    <path d="M8 3.5v9M3.5 8h9" />
  {:else if name === "kit"}
    <!-- A case: your setup travels with you. -->
    <rect x="2.5" y="5" width="11" height="8" rx="1.5" />
    <path d="M6 5V3.75C6 3.34 6.34 3 6.75 3h2.5c.41 0 .75.34.75.75V5M2.5 8.5h11" />
  {:else if name === "keyboard"}
    <rect x="1.75" y="4" width="12.5" height="8" rx="1.5" />
    <path d="M4.5 6.75h.01M7 6.75h.01M9.5 6.75h.01M12 6.75h-.01M5.5 9.5h5" />
  {:else if name === "play"}
    <path d="M5 3.5v9l7-4.5z" />
  {:else if name === "refresh"}
    <path d="M13 8a5 5 0 1 1-1.46-3.54M13 3v2.5h-2.5" />
  {:else if name === "lock"}
    <rect x="3" y="7" width="10" height="6.5" rx="1.5" />
    <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" />
  {:else if name === "warn"}
    <path d="M8 2.5 14 13H2zM8 6.5v3M8 11.25h.01" />
  {/if}
</svg>

<style>
  /* `UI.scss` sizes and fills every SVG inside a button (`button svg { height: 1.5rem; fill:
     … }`). The size is pinned inline above and the fill here, so an icon looks the same in a
     button as outside one. Circles that need a fill set it as their own attribute. */
  .icon {
    display: block;
    flex: 0 0 auto;
    fill: none;
  }
</style>
