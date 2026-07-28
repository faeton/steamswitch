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

const app = void (async () => {
  await initI18n()
  await initOfflineMode()
  await initTheme()
  await resolveInitialRoute()
  installHashSync()
  new App({ target: document.getElementById('app')! })
})()

export default app
