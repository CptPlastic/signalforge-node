import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'
import { registerPWA } from './lib/pwa'

const rootElement = document.getElementById('root')

if (!rootElement) {
  throw new Error('Failed to find root element')
}

const showFatal = (message: string) => {
  rootElement.innerHTML = `<pre style="color:#ff4444;background:#0a0a0a;padding:16px;">FATAL: ${message}</pre>`
}

globalThis.addEventListener('error', (event) => {
  showFatal(event.message || 'Unexpected runtime error')
})

try {
  registerPWA()
  ReactDOM.createRoot(rootElement).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  )
} catch (error) {
  const message = error instanceof Error ? error.message : 'Unknown bootstrap error'
  showFatal(message)
}
