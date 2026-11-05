<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import type { ComponentType, SvelteComponent } from "svelte";
  import ModalBodyShell from "./ModalBodyShell.svelte";

  export let html: string | undefined = undefined;
  export let component: ComponentType<SvelteComponent> | undefined = undefined;
  export let componentProps: Record<string, unknown> | undefined = undefined;
  export let positiveLabel = "";
  export let negativeLabel: string | undefined = undefined;
  export let style: string = "";
  export let destructive = false;

  const dispatch = createEventDispatcher<{ resolve: boolean }>();

  function positive(): void {
    dispatch("resolve", true);
  }

  function negative(): void {
    dispatch("resolve", false);
  }
</script>

<div class="modal-block">
  <ModalBodyShell
    {html}
    {component}
    {componentProps}
  />
  <div class="modal-inline-actions settingsCol inputAndButton">
    <span class="modal-actions-spacer"></span>
    <button type="button" class="ss-btn ss-btn--primary" class:ss-btn--destructive={destructive} on:click={positive}>
      {positiveLabel}
    </button>
    {#if style === "yesno" || style === "okcancel"}
      <button type="button" class="ss-btn" on:click={negative}>
        {negativeLabel}
      </button>
    {/if}
  </div>
</div>
