import App from './App.svelte'
import './styles/context_menu.scss'
import './styles/normalize.scss'
import './styles/style.scss'
import './styles/theme.scss'
// After theme.scss: the built-in Light/Dark base overrides its Dracula-era primitives.
import './styles/appearance.scss'
// After appearance.scss: semantic type/shape/control tokens, which read the colour roles above.
import './styles/tokens.scss'
import './styles/overlayReceivers.scss'
import './styles/UI.scss'
import './styles/modal-primary.scss'
import './styles/acclist.scss'
import './styles/rtl.scss'
import { initI18n } from './stores/i18n'
import { initOfflineMode } from './stores/offlineMode'
import { resolveInitialRoute, installHashSync } from './stores/nav'
import { initTheme } from './lib/themes'
import { hostOS } from './lib/hostOS'

// Platform conventions that are pure CSS (dialog button order, for one) key off this, so the
// first paint is already right for the OS the app is running on.
document.documentElement.dataset.os = hostOS

const app = void (async () => {
  await initI18n()
  await initOfflineMode()
  await initTheme()
  await resolveInitialRoute()
  installHashSync()
  new App({ target: document.getElementById('app')! })
  // Blank-window evidence: see internal/app/frontend_report.go. Painted means the page did its
  // part, so a window still showing nothing is the webview or the OS.
  const report = (window as unknown as { __reportStage?: (s: string, d?: string) => void }).__reportStage
  report?.('mounted', `${innerWidth}x${innerHeight} ${document.visibilityState}`)
  requestAnimationFrame(() => report?.('first-frame', `${innerWidth}x${innerHeight}`))
})()

export default app
