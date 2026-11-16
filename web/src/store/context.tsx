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
  /** Content above the top bar. */
  header?: ReactNode
  /** Extra buttons at the end of the top bar (before Settings). */
  actions?: ReactNode
}

export const AppContext = createContext<AppContextValue | null>(null)

export function useAppContext(): AppContextValue {
  const v = useContext(AppContext)
  if (!v) throw new Error('Missing <App>: haro components only work inside it')
  return v
}

/** Application state (zustand), with a selector. */
export function useApp<T>(selector: (s: AppState) => T): T {
  return useStore(useAppContext().store, selector)
}

/** The zustand store itself, to read or call actions outside render (`useAppApi().getState()`). */
export const useAppApi = (): AppStoreApi => useAppContext().store
/** The injected core client. */
export const useCoreClient = (): CoreClient => useAppContext().client
/** The injected configuration store. */
export const useConfigStore = (): ConfigStore => useAppContext().configStore
