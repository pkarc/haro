import { useCallback, useContext, useSyncExternalStore } from 'react'
import { AppContext } from '../store/context'
import type { Lang } from '../core/types'
import { getActiveLang, makeT, type T } from './index'

/** Current interface language (the `lang` of the app store; outside an app, the active one). */
export function useLang(): Lang {
  const ctx = useContext(AppContext)
  const subscribe = useCallback((cb: () => void) => (ctx ? ctx.store.subscribe(cb) : () => {}), [ctx])
  return useSyncExternalStore(subscribe, () => (ctx ? ctx.store.getState().lang : getActiveLang()), getActiveLang)
}

/** Translator for the current interface language; re-renders when the language changes. */
export function useT(): T {
  return makeT(useLang())
}
