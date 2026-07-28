<script lang="ts">
    import { route, navigateBackLikeButton, canGoBack } from '../stores/nav'
    import { t } from "../stores/i18n";
    import { onMount } from 'svelte'
    import { appBarTitle } from '../stores/nav'
    import { activeModal } from '../stores/modal'
    import { contextMenu } from '../stores/contextMenu'
    import { searchOverlayCtrl } from '../stores/searchOverlay'
    import { Events, Window } from "@wailsio/runtime";
    import { hostOS } from "../lib/hostOS";

    /*
      The window is frameless on every platform, so the app draws its own caption controls.
      Which controls, and where, is not cosmetic — it is the first thing that tells a user
      whether an app belongs on their desktop. The design doc opens with "window chrome per
      OS" for exactly that reason:

        Windows  — the ─ ▢ ✕ cluster on the right, close turning red on hover.
        macOS    — traffic lights on the left (close / minimise / zoom), title centred.
        GNOME    — a round close button on the right of a taller headerbar.

      Before this the Windows cluster was drawn on all three, which on macOS put caption
      buttons on the wrong side and left the top-left corner conspicuously empty.
    */
    const isMac = hostOS === "macos"
    const isLinux = hostOS === "linux"

    let minimised = false
    let maximised = false
    let escapeOwnedPopupOpen = false

    const common = Events.Types.Common

    async function refreshWindowState() {
        const [min, max] = await Promise.all([
            Window.IsMinimised(),
            Window.IsMaximised(),
        ])
        minimised = min
        maximised = max
    }

    function refreshEscapeOwnedPopupOpen() {
        escapeOwnedPopupOpen = !!document.querySelector(
            ".shortcutDropdown.open, .dropdown-menu.show, .custom-dropdown-menu.show, [role='dialog']"
        )
    }

    onMount(() => {
        void refreshWindowState()
        refreshEscapeOwnedPopupOpen()

        const tracked = [
            common.WindowMinimise,
            common.WindowUnMinimise,
            common.WindowMaximise,
            common.WindowUnMaximise,
            common.WindowRestore,
            common.WindowFullscreen,
            common.WindowUnFullscreen,
        ] as const

        const unsubs = tracked.map((subscribedAs) =>
            Events.On(subscribedAs, (ev) => {
                void refreshWindowState()
            })
        )
        const onUiStateChange = () => requestAnimationFrame(refreshEscapeOwnedPopupOpen)
        window.addEventListener("focusin", onUiStateChange, true)
        window.addEventListener("keydown", onUiStateChange, true)
        window.addEventListener("pointerdown", onUiStateChange, true)

        return () => {
            for (const off of unsubs) off()
            window.removeEventListener("focusin", onUiStateChange, true)
            window.removeEventListener("keydown", onUiStateChange, true)
            window.removeEventListener("pointerdown", onUiStateChange, true)
        }
    })

    /*
      The back control is driven by real navigation history (REDESIGN_BRIEF.md Part B #3).

      It used to be rendered on every screen, disabled only while a modal was open, and — on
      `home`, where it could not navigate at all — it ran a random-axis 360° spin instead. That
      read as "the app is busy" and taught users the control was broken. Two rules now:

        - Not rendered at all when there is nowhere to go back to. An absent control is
          honest; an inert one is not.
        - Disabled (still rendered) while a modal owns the screen, because the destination
          exists, it is just not reachable this second.
    */
    $: backAvailable = $canGoBack || $route.page !== 'home'
    $: backDisabled = !!$activeModal
    $: backPromptHidden = !backAvailable || backDisabled || !!$contextMenu || $searchOverlayCtrl.open || escapeOwnedPopupOpen
    $: titleLabel = (() => {
        switch ($route.page) {
            case "dota-configs":
                return $t("Title_DotaConfigs")
            case "preview-css":
                return $t("Title_Settings_TestCss")
            case "settings":
                return $t("Title_Settings")
            case "vault":
                return $t("Title_Vault")
            case "about":
                return $t("Title_About")
            default:
                return $appBarTitle
        }
    })()

    function backClick() {
        navigateBackLikeButton()
    }
</script>

<header class="headerbar" data-os={hostOS}>
    <span class="title-left">
        {#if isMac}
            <!-- Traffic lights, in macOS order and colours: close, minimise, zoom. Real
                 buttons with labels rather than the decorative circles the design doc
                 sketches — on a frameless window these are the only way to close it. -->
            <span class="traffic" role="toolbar">
                <button
                    type="button"
                    class="traffic__light traffic__light--close"
                    aria-label={$t("Aria_WindowClose")}
                    title={$t("Aria_WindowClose")}
                    on:click={() => Window.Close()}
                ><span class="traffic__glyph" aria-hidden="true">✕</span></button>
                <button
                    type="button"
                    class="traffic__light traffic__light--min"
                    aria-label={$t("Aria_WindowMinimize")}
                    title={$t("Aria_WindowMinimize")}
                    on:click={() => Window.Minimise()}
                ><span class="traffic__glyph" aria-hidden="true">−</span></button>
                <button
                    type="button"
                    class="traffic__light traffic__light--zoom"
                    aria-label={maximised ? $t("Aria_WindowRestore") : $t("Aria_WindowMaximize")}
                    title={maximised ? $t("Aria_WindowRestore") : $t("Aria_WindowMaximize")}
                    on:click={() => (maximised ? Window.Restore() : Window.Maximise())}
                ><span class="traffic__glyph" aria-hidden="true">{maximised ? "−" : "+"}</span></button>
            </span>
        {/if}
        {#if backAvailable}
            <button
                type="button"
                class="win-btn win-btn-back"
                class:win-btn-back--controller-prompt={!backPromptHidden}
                title={$t("Aria_WindowBack")}
                disabled={backDisabled}
                aria-disabled={backDisabled}
                on:click={backClick}
            >
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320 512"><!--!Font Awesome Free v5.15.4 by @fontawesome - https://fontawesome.com License - https://fontawesome.com/license/free Copyright 2026 Fonticons, Inc.--><path d="M34.52 239.03L228.87 44.69c9.37-9.37 24.57-9.37 33.94 0l22.67 22.67c9.36 9.36 9.37 24.52.04 33.9L131.49 256l154.02 154.75c9.34 9.38 9.32 24.54-.04 33.9l-22.67 22.67c-9.37 9.37-24.57 9.37-33.94 0L34.52 272.97c-9.37-9.37-9.37-24.57 0-33.94z"/></svg>
            </button>
        {/if}
        {#if !isMac}
            <svg class="header_icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 768 264" fill-rule="evenodd" stroke-linejoin="round" stroke-miterlimit="2">
                <use href="img/SteamSwitch_Logo.svg#logo"></use>
            </svg>
        {/if}
    </span>
    <span id="title-label" class="title-drag">
        {titleLabel}
    </span>
    {#if !isMac}
        <span class="window-controls" role="toolbar">
            {#if !isLinux}
                <button type="button" class="win-btn win-btn-min" aria-label={$t("Aria_WindowMinimize")} on:click={() => Window.Minimise()}>
                    <svg class="win-btn__glyph win-btn__glyph--min" viewBox="0 0 10 10" aria-hidden="true">
                        <path d="M1 5h8" />
                    </svg>
                </button>
                {#if maximised}
                    <button type="button" class="win-btn win-btn-max" aria-label={$t("Aria_WindowRestore")} on:click={() => Window.Restore()}>
                        <svg class="win-btn__glyph win-btn__glyph--restore" viewBox="0 0 10 10" aria-hidden="true">
                            <path d="M2.5 1.5h5v5h-5z" />
                            <path d="M4.5 3.5h4v5h-5v-4" />
                        </svg>
                    </button>
                {:else}
                    <button type="button" class="win-btn win-btn-max" aria-label={$t("Aria_WindowMaximize")} on:click={() => Window.Maximise()}>
                        <svg class="win-btn__glyph win-btn__glyph--max" viewBox="0 0 10 10" aria-hidden="true">
                            <path d="M1.5 1.5h7v7h-7z" />
                        </svg>
                    </button>
                {/if}
            {/if}
            <button
                type="button"
                class="win-btn win-btn-close"
                class:win-btn-close--round={isLinux}
                aria-label={$t("Aria_WindowClose")}
                on:click={() => Window.Close()}
            >
                <svg class="win-btn__glyph win-btn__glyph--close" viewBox="0 0 10 10" aria-hidden="true">
                    <path d="M2 2l6 6" />
                    <path d="M8 2L2 8" />
                </svg>
            </button>
        </span>
    {:else}
        <!-- Balances the traffic lights so the centred title is centred in the *bar*, not in
             whatever is left of it. -->
        <span class="mac-spacer" aria-hidden="true"></span>
    {/if}
</header>

<style lang="scss">
    .headerbar {
        --webkit-app-region: drag;
        --wails-draggable: drag;
        -moz-user-select: none;
        -ms-user-select: none;
        -webkit-user-select: none;
        user-select: none;
        z-index: 5;
        background: var(--surface-chrome, var(--border-bar-bg));
        border-bottom: 1px solid var(--hairline, var(--border-bar-bg));
        position: relative;
        height: var(--chrome-height, 34px);
        min-height: var(--chrome-height, 34px);
        width: 100%;
        -webkit-app-region: drag;
        color: var(--fg-secondary, var(--whiteSecondary));
        grid-column: 1;
        display: flex;
        justify-content: space-between;
        align-items: center;
        overflow: hidden;
        font-size: var(--fs-secondary, 12.5px);
        font-weight: var(--fw-normal, 400);
    }
    .title-left {
        z-index: 1;
        height: 100%;
        display: flex;
        flex-direction: row;
        align-items: center;
    }

    /* GNOME's headerbar is taller than Windows'; macOS' is a little taller too. */
    .headerbar[data-os="macos"] {
        --chrome-height: 38px;
    }
    .headerbar[data-os="linux"] {
        --chrome-height: 46px;
    }

    /* ---------------------------------------------------------- macOS traffic lights */

    .traffic {
        --wails-draggable: no-drag;
        display: flex;
        align-items: center;
        gap: 8px;
        padding-left: 13px;
    }

    .traffic__light {
        width: 12px;
        height: 12px;
        min-width: 12px;
        padding: 0;
        margin: 0;
        border: 0;
        border-radius: 50%;
        display: grid;
        place-items: center;
        cursor: default;
        line-height: 1;
    }

    .traffic__light--close { background: #ff5f57; }
    .traffic__light--min { background: #febc2e; }
    .traffic__light--zoom { background: #28c840; }

    /*
      macOS reveals the glyphs only while the pointer is over the cluster — hovering one
      lights all three. Reproducing that is the difference between "traffic lights" and
      "three coloured dots"; `:focus-within` covers keyboard users, who get no hover at all.
    */
    .traffic__glyph {
        font-size: 9px;
        font-weight: 700;
        line-height: 1;
        color: rgba(0, 0, 0, 0.55);
        opacity: 0;
    }

    .traffic:hover .traffic__glyph,
    .traffic:focus-within .traffic__glyph {
        opacity: 1;
    }

    .traffic__light:hover {
        background: var(--traffic-hover, inherit);
        filter: brightness(0.92);
    }

    .traffic__light:focus-visible {
        outline: 2px solid var(--role-focus-ring, var(--accent));
        outline-offset: 2px;
    }

    /* Same width as the traffic cluster, so the centred title is centred in the whole bar. */
    .mac-spacer {
        width: 73px;
        flex: 0 0 auto;
    }
    .title-drag {
        position: absolute;
        top: 50%;
        left: 50%;
        transform: translate(-50%, -50%);
        max-width: calc(100% - 300px);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        pointer-events: none;
    }
    .win-btn-back {
        --wails-draggable: no-drag;
        position: relative;
        svg {
            fill: var(--whiteSecondary);
            height: 0.8rem;
            display: block;
        }
        &:disabled {
            opacity: 0.35;
            cursor: not-allowed;
        }
    }
    :global(.input-modality-controller) .win-btn-back--controller-prompt::after {
        content: var(--controller-back-prompt-glyph);
        position: absolute;
        top: 2px;
        right: 2px;
        z-index: 3;
        width: 1rem;
        height: 1rem;
        min-width: 1rem;
        min-height: 1rem;
        border-radius: 50%;
        border: 2px solid var(--controller-back-prompt-border);
        box-sizing: border-box;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        background: var(--controller-back-prompt-bg);
        color: var(--controller-back-prompt-fg);
        box-shadow: 0 2px 7px var(--shadow-color-45, rgba(0, 0, 0, 0.45));
        font-family: Arial, Helvetica, sans-serif;
        font-size: 0.66rem;
        font-weight: 800;
        line-height: 1;
        pointer-events: none;
        text-shadow: none;
    }
    @media (forced-colors: active) {
        :global(.input-modality-controller) .win-btn-back--controller-prompt::after {
            background: ButtonFace;
            border-color: ButtonText;
            color: ButtonText;
            box-shadow: none;
        }
    }
    .header_icon {
        height: 10px;
        margin: 12px;
        display: block;
        fill: var(--whiteSecondary);
    }
    .window-controls {
        --wails-draggable: no-drag;
        z-index: 1;
        /* Flex, not a fixed three-column grid: GNOME renders one control, Windows three, and
           a `repeat(3, 46px)` track left two empty cells to the right of the Linux close
           button — which is why it looked adrift rather than right-aligned. */
        display: flex;
        align-items: center;
        top: 0;
        right: 0;
        height: 100%;
    }
    .win-btn {
        border-radius: 0;
        background: none;
        border: 0;
        margin: 0;
        display: flex;
        justify-content: center;
        align-items: center;
        width: 46px;
        height: 100%;

        &:hover {
            background: var(--window-control-hover-bg);
        }
    }
    .win-btn__glyph {
        width: 10px;
        height: 10px;
        display: block;
        overflow: visible;
        color: currentColor;
        fill: none;
        stroke: currentColor;
        stroke-width: 1.2;
        stroke-linecap: square;
        stroke-linejoin: miter;
        vector-effect: non-scaling-stroke;
        forced-color-adjust: auto;
    }
    .win-btn__glyph--min {
        stroke-linecap: butt;
    }
    .win-btn-close:hover {background: var(--window-close-hover); color: #fff;}
    .win-btn-close:hover .win-btn__glyph {stroke: #fff;}

    /*
      GNOME/Adwaita puts a single round close button on the right of the headerbar; there is
      no minimise or maximise button in the default GTK4 decoration.
    */
    .win-btn-close--round {
        width: 30px;
        height: 30px;
        margin-right: 10px;
        border-radius: 50%;
        background: var(--button-bg);
    }
    .win-btn-close--round:hover {
        background: var(--window-close-hover);
    }
</style>
