import { createApp } from 'vue'
import App from './App.vue'
import './style.css'
import { installBrowserPreview } from './preview'
import { FrontendLog, FrontendReady } from '../wailsjs/go/main/App'

if (import.meta.env.DEV) {
  installBrowserPreview()
}

function errorDetails(value: unknown) {
  if (value instanceof Error) {
    return { message: `${value.name}: ${value.message}`, stack: value.stack || '' }
  }
  return { message: typeof value === 'string' ? value : 'Unknown frontend error', stack: '' }
}

function reportFrontendError(value: unknown) {
  const details = errorDetails(value)
  void FrontendLog('error', details.message, details.stack).catch(() => {
    console.error(details.message, details.stack)
  })
}

window.addEventListener('error', (event) => reportFrontendError(event.error || event.message))
window.addEventListener('unhandledrejection', (event) => reportFrontendError(event.reason))

const root = document.querySelector<HTMLElement>('#app')
const application = createApp(App)
application.config.errorHandler = (error) => reportFrontendError(error)

try {
  application.mount('#app')
  void FrontendReady().catch(reportFrontendError)
} catch (error) {
  reportFrontendError(error)
  if (root) {
    root.innerHTML = `
      <main class="fatal-screen">
        <strong>expand 前端启动失败</strong>
        <p>错误已经写入本地诊断日志，请查看：</p>
        <code>%LOCALAPPDATA%\\expand\\logs\\expand.log</code>
      </main>
    `
  }
}
