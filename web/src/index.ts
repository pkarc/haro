// Entry point to use the haro web from another package (e.g. haro-site, with
// haro as a submodule). See web/README.md.
export { App, type AppProps } from './App'
export { mountApp, type GateApi, type MountOptions } from './mount'

export type { CoreClient } from './core/client'
export { ConnectCoreClient } from './core/client'
export { WasmCoreClient } from './core/wasm'
export { LocalStore, ServedStore, type ConfigStore, type SaveResult } from './core/configstore'
export * from './core/types'

// For the UI slots (header, actions, gate) and custom stores.
export { useApp, useAppApi, useConfigStore, useCoreClient, createAppStore, type AppState, type AppStoreApi } from './store'
export { Modal } from './components/Modal'
export { confirmBox, openDialog, promptBox } from './components/dialogs'

// Interface texts (English and Spanish): for extra UI built with the extension points.
export { makeT, translate, type Key, type T } from './i18n'
export { useLang, useT } from './i18n/react'
