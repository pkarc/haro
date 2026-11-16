// Punto de entrada para usar la web de haro desde otro paquete (p. ej. haro-site,
// con haro como submódulo). Ver web/README.md.
export { App, type AppProps } from './App'
export { mountApp, type GateApi, type MountOptions } from './mount'

export type { CoreClient } from './core/client'
export { ConnectCoreClient } from './core/client'
export { WasmCoreClient } from './core/wasm'
export { LocalStore, ServedStore, type ConfigStore, type SaveResult } from './core/configstore'
export * from './core/types'

// Para los huecos de la interfaz (header, actions, gate) y los almacenes propios.
export { useApp, useAppApi, useConfigStore, useCoreClient, createAppStore, type AppState, type AppStoreApi } from './store'
export { Modal } from './components/Modal'
export { confirmBox, openDialog, promptBox } from './components/dialogs'
