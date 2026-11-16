import { createContext, useContext, type ReactNode } from 'react'
import { useStore } from 'zustand'
import type { CoreClient } from '../core/client'
import type { ConfigStore } from '../core/configstore'
import type { AppState, createAppStore } from './store'

export type AppStoreApi = ReturnType<typeof createAppStore>

export interface AppContextValue {
  client: CoreClient
  configStore: ConfigStore
  store: AppStoreApi
  /** Contenido sobre la barra superior. */
  header?: ReactNode
  /** Botones extra al final de la barra superior (antes de Ajustes). */
  actions?: ReactNode
}

export const AppContext = createContext<AppContextValue | null>(null)

export function useAppContext(): AppContextValue {
  const v = useContext(AppContext)
  if (!v) throw new Error('Falta <App>: los componentes de haro solo funcionan dentro de ella')
  return v
}

/** Estado de la aplicación (zustand), con selector. Equivale al antiguo singleton `useApp`. */
export function useApp<T>(selector: (s: AppState) => T): T {
  return useStore(useAppContext().store, selector)
}

/** El almacén zustand en sí, para leer o llamar acciones fuera de render (`useAppApi().getState()`). */
export const useAppApi = (): AppStoreApi => useAppContext().store
/** El cliente del núcleo inyectado. */
export const useCoreClient = (): CoreClient => useAppContext().client
/** El almacén de configuración inyectado. */
export const useConfigStore = (): ConfigStore => useAppContext().configStore
