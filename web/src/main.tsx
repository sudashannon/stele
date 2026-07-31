import { createRoot } from 'react-dom/client'
import App from './App'
import './styles.css'

// Hashed assets are served `immutable`, so a tab that stays open across a
// redeploy keeps importing chunk names the new binary no longer embeds; every
// lazy panel and diagram in that tab then fails with a module fetch error.
// Reloading picks up the fresh index.html. The session guard keeps a genuinely
// missing chunk from looping: after one attempt the error propagates to the
// component that requested it, which renders its own fallback.
const staleBundleReloadKey = 'stele:stale-bundle-reloaded'
window.addEventListener('vite:preloadError', (event) => {
  if (sessionStorage.getItem(staleBundleReloadKey) !== null) return
  event.preventDefault()
  sessionStorage.setItem(staleBundleReloadKey, String(Date.now()))
  window.location.reload()
})

createRoot(document.getElementById('root')!).render(<App />)
