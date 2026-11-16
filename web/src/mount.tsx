import { StrictMode, useState, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import type { CoreClient } from './core/client'
import type { ConfigStore } from './core/configstore'

/** What the previous screen (`gate`) receives to let the application through. */
export interface GateApi {
  /** Enters the application; with `store` it replaces the initial store (e.g. that of the chosen repository). */
  enter(store?: ConfigStore): void
}

export interface MountOptions {
  client: CoreClient
  /** Configuration store; with `gate` it can be replaced on entering. */
  store: ConfigStore
  container: HTMLElement
  /** Content above the top bar. */
  header?: ReactNode
  /** Extra buttons in the top bar, before Settings. */
  actions?: ReactNode
  /** Previous screen (login, repository picker…): rendered instead of the app until `enter` is called. */
  gate?: (api: GateApi) => ReactNode
  /** Wraps in React.StrictMode (default: yes). */
  strict?: boolean
}

function Root({ gate, store: initial, ...rest }: Omit<MountOptions, 'container' | 'strict'>) {
  const [entered, setEntered] = useState(!gate)
  const [store, setStore] = useState(initial)
  // The app is recreated when the store is replaced. The key is a counter, not
  // `store.label`, because labels may be translated and change with the language.
  const [generation, setGeneration] = useState(0)
  if (!entered && gate) return <>{gate({ enter: s => { if (s) { setStore(s); setGeneration(g => g + 1) } setEntered(true) } })}</>
  return <App {...rest} store={store} key={generation} />
}

/** Mounts the application in `container`. Returns `unmount` to unmount it. */
export function mountApp(opts: MountOptions): { unmount(): void } {
  const { container, strict = true, ...rest } = opts
  const root = createRoot(container)
  const el = <Root {...rest} />
  root.render(strict ? <StrictMode>{el}</StrictMode> : el)
  return { unmount: () => root.unmount() }
}
