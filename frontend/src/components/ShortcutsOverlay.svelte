<script lang="ts">
  /**
   * The discoverable shortcut list (REDESIGN_BRIEF.md A6 "Keyboard shortcuts", A13-A11y).
   *
   * The brief's complaint about the old `1` badge was not only that it lacked an affordance,
   * but that there was nowhere to *learn* the shortcuts at all. The keycap now says what it
   * does on hover; this is where the full set lives.
   */
  import { t } from "../stores/i18n";
  import { modalFocus } from "../lib/modalFocus";
  import { createEventDispatcher } from "svelte";

  export let open = false;

  const dispatch = createEventDispatcher<{ close: void }>();

  type Row = { keys: string[]; descKey: string };

  const GROUPS: { titleKey: string; rows: Row[] }[] = [
    {
      titleKey: "Shortcuts_Group_Switching",
      rows: [
        { keys: ["1", "…", "9"], descKey: "Shortcuts_QuickSwitch" },
        { keys: ["Shift", "1–9"], descKey: "Shortcuts_QuickDetail" },
        { keys: ["Enter"], descKey: "Shortcuts_Activate" },
        { keys: ["Shift", "Click"], descKey: "Shortcuts_ShiftClick" },
      ],
    },
    {
      titleKey: "Shortcuts_Group_Navigation",
      rows: [
        { keys: ["Alt", "←"], descKey: "Shortcuts_Back" },
        { keys: ["Alt", "→"], descKey: "Shortcuts_Forward" },
        { keys: ["Esc"], descKey: "Shortcuts_Escape" },
      ],
    },
    {
      titleKey: "Shortcuts_Group_Finding",
      rows: [
        { keys: ["Ctrl", "K"], descKey: "Shortcuts_CommandPalette" },
        { keys: ["A", "…", "Z"], descKey: "Shortcuts_TypeToSearch" },
        { keys: ["Menu"], descKey: "Shortcuts_ContextMenu" },
      ],
    },
  ];

  function close(): void {
    dispatch("close");
  }

  /*
    Escape is bound at the window rather than on the sheet: focus can legitimately sit on any
    control inside, and a handler on the container only fires for descendants that bubble.
    Guarded on `open` so it never competes with the page's own Escape handling when closed.
  */
  function onWindowKeydown(e: KeyboardEvent): void {
    if (!open || e.key !== "Escape") return;
    e.preventDefault();
    e.stopPropagation();
    close();
  }
</script>

<svelte:window on:keydown|capture={onWindowKeydown} />

{#if open}
  <div class="scrim">
    <!-- A real button behind the sheet, so "click outside to dismiss" is reachable by
         keyboard and announced, rather than a click handler on a decorative div. -->
    <button type="button" class="scrim__dismiss" aria-label={$t("Button_Close")} on:click={close}
    ></button>
    <div
      class="sheet"
      role="dialog"
      aria-modal="true"
      aria-label={$t("Shortcuts_Title")}
      use:modalFocus={{}}
    >
      <header class="sheet__head">
        <h2 class="sheet__title">{$t("Shortcuts_Title")}</h2>
        <button type="button" class="ss-btn" on:click={close}>{$t("Button_Close")}</button>
      </header>

      <div class="sheet__body">
        {#each GROUPS as group (group.titleKey)}
          <section class="group">
            <h3 class="ss-eyebrow">{$t(group.titleKey)}</h3>
            <dl class="rows">
              {#each group.rows as row (row.descKey)}
                <dt class="row__keys">
                  {#each row.keys as key, i (i)}
                    {#if key === "…" || key === "+"}
                      <span class="row__sep" aria-hidden="true">{key}</span>
                    {:else}
                      <kbd class="keycap">{key}</kbd>
                    {/if}
                  {/each}
                </dt>
                <dd class="row__desc">{$t(row.descKey)}</dd>
              {/each}
            </dl>
          </section>
        {/each}

        <!-- Says out loud what "1–9" binds to. The brief flags the ambiguity between visible
             order and a sticky pin as a real open question; this build answers "visible
             order", and the copy has to say so or the numbers look arbitrary after a filter. -->
        <p class="ss-help">{$t("Shortcuts_OrderNote")}</p>
      </div>
    </div>
  </div>
{/if}

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 40;
    display: grid;
    place-items: center;
    padding: var(--space-6);
    background: var(--modal-scrim, var(--backdrop-scrim-55));
  }

  .scrim__dismiss {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: none;
    cursor: default;
  }

  .sheet {
    position: relative;
    width: min(560px, 100%);
    max-height: 100%;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--hairline-strong, var(--border-bar-bg));
    border-radius: var(--radius-xl);
    background: var(--surface-panel-raised, var(--mainContentBackground));
    box-shadow: var(--shadow-panel);
    overflow: hidden;
  }

  .sheet__head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .sheet__title {
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .sheet__body {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .rows {
    margin: 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--space-3) var(--space-5);
    align-items: center;
  }

  .row__keys {
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }

  .row__sep {
    color: var(--fg-disabled);
    font-size: var(--fs-meta);
  }

  .row__desc {
    margin: 0;
    font-size: var(--fs-body);
    color: var(--fg-secondary);
  }

  /* Same key affordance as the tile's, so the overlay is visibly documenting *that* control. */
  .keycap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: var(--keycap-size);
    height: var(--keycap-size);
    padding: 0 6px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-bottom-width: 2px;
    border-radius: var(--radius-xs);
    font-family: var(--font-mono, monospace);
    font-size: var(--keycap-fs);
    line-height: 1;
    color: var(--fg-secondary);
  }
</style>
