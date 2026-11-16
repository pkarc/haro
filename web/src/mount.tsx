import { StrictMode, useState, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import type { CoreClient } from './core/client'
import type { ConfigStore } from './core/configstore'

/** Lo que recibe la pantalla previa (`gate`) para dar paso a la aplicación. */
export interface GateApi {
  /** Entra en la aplicación; con `store` sustituye al almacén inicial (p. ej. el del repositorio elegido). */
  enter(store?: ConfigStore): void
}

export interface MountOptions {
  client: CoreClient
  /** Almacén de configuración; con `gate` puede sustituirse al entrar. */
  store: ConfigStore
  container: HTMLElement
  /** Contenido sobre la barra superior. */
  header?: ReactNode
  /** Botones extra en la barra superior, antes de Ajustes. */
  actions?: ReactNode
  /** Pantalla previa (login, selector de repositorio…): se pinta en lugar de la app hasta llamar a `enter`. */
  gate?: (api: GateApi) => ReactNode
  /** Envuelve en React.StrictMode (por defecto, sí). */
  strict?: boolean
}

function Root({ gate, store: initial, ...rest }: Omit<MountOptions, 'container' | 'strict'>) {
  const [entered, setEntered] = useState(!gate)
  const [store, setStore] = useState(initial)
  if (!entered && gate) return <>{gate({ enter: s => { if (s) setStore(s); setEntered(true) } })}</>
  return <App {...rest} store={store} key={store.label} />
}

/** Monta la aplicación en `container`. Devuelve `unmount` para desmontarla. */
export function mountApp(opts: MountOptions): { unmount(): void } {
  const { container, strict = true, ...rest } = opts
  const root = createRoot(container)
  const el = <Root {...rest} />
  root.render(strict ? <StrictMode>{el}</StrictMode> : el)
  return { unmount: () => root.unmount() }
}
