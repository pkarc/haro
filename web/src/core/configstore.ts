import { tr } from '../i18n'
import type { CoreClient } from './client'
import type { PutResult, SourceFile } from './types'

/**
 * Result of saving: that of `CoreClient.putFiles` plus the version-conflict
 * signal (someone changed the origin since it was loaded; a reload is needed).
 */
export interface SaveResult extends PutResult {
  conflict?: boolean
}

/**
 * Where the files of a configuration come from and where they go. The web only
 * uses it through this interface; the implementations live outside the core
 * (disk served by `haro serve`, a GitHub repository…).
 */
export interface ConfigStore {
  /** Short name of the destination, for the UI («disk», «owner/repo@main»). */
  readonly label: string
  /** Saving is possible. Read after `load()`, so it may depend on what that finds out. */
  readonly canSave: boolean
  /** Why saving is not possible, if `canSave` is false (shown when pressing «Save»). */
  readonly readOnlyHint?: string
  /** Note under the title of the save dialog (e.g. «a .bak copy is kept»). */
  readonly saveHint?: string
  /** localStorage key of the session (default `haro.v1`); distinct per origin so files do not get mixed. */
  readonly storageKey?: string
  /**
   * Loads the files, in load order. `null`: the origin has no configuration
   * and the session is local only (no save button).
   */
  load(): Promise<SourceFile[] | null>
  /**
   * Saves the modified files (all or nothing). Expected failures (validation,
   * permissions, conflict) are a `SaveResult` with `saved: false`, not an
   * exception.
   */
  save(files: SourceFile[]): Promise<SaveResult>
}

/**
 * No origin: the session lives only in the browser (the application state,
 * files included, is persisted in localStorage) and leaves through
 * import/export. It is the store of the serverless web.
 *
 * Its texts follow the interface language (the app keeps the active language
 * in sync), so they are getters.
 */
export class LocalStore implements ConfigStore {
  get label() { return tr('store.local.label') }
  readonly canSave = false
  get readOnlyHint() { return tr('store.local.readOnly') }
  async load() { return null }
  async save(): Promise<SaveResult> {
    return { saved: false, error: this.readOnlyHint, status: 400 }
  }
}

/**
 * Files of `haro serve --file` (GetConfig/PutConfig of the CoreClient). It is
 * a separate store from LocalStore because it depends on the client and on the
 * server capabilities (`--allow-write`), and because validation with
 * `haproxy -c` and the `.bak` copy are done by the server when saving. Without
 * `--file`, `load()` gives `null` and the session stays local.
 */
export class ServedStore implements ConfigStore {
  get label() { return tr('store.served.label') }
  get saveHint() { return tr('store.served.saveHint') }
  private writable = false

  constructor(private client: CoreClient) {}

  get canSave() { return this.writable }
  get readOnlyHint() { return tr('store.served.readOnly') }

  async load() {
    const [caps, files] = await Promise.all([this.client.capabilities().catch(() => null), this.client.getConfig().catch(() => null)])
    this.writable = !!caps?.canWrite
    return files && files.length ? files : null
  }

  save(files: SourceFile[]): Promise<SaveResult> { return this.client.putFiles(files) }
}
