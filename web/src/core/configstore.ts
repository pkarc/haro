import type { CoreClient } from './client'
import type { PutResult, SourceFile } from './types'

/**
 * Resultado de guardar: el de `CoreClient.putFiles` más la señal de conflicto
 * de versión (alguien cambió el origen desde que se cargó; hay que recargar).
 */
export interface SaveResult extends PutResult {
  conflict?: boolean
}

/**
 * De dónde vienen y adónde van los archivos de una configuración. La web solo
 * lo usa a través de esta interfaz; las implementaciones viven fuera del
 * núcleo (disco servido por `haro serve`, un repositorio de GitHub…).
 */
export interface ConfigStore {
  /** Nombre corto del destino, para la interfaz («disco», «owner/repo@main»). */
  readonly label: string
  /** Se puede guardar. Se lee tras `load()`, así que puede depender de lo que este averigüe. */
  readonly canSave: boolean
  /** Por qué no se puede guardar, si `canSave` es false (se muestra al pulsar «Guardar»). */
  readonly readOnlyHint?: string
  /** Aviso bajo el título del diálogo de guardado (p. ej. «se deja una copia .bak»). */
  readonly saveHint?: string
  /** Clave del localStorage con la sesión (por defecto `haro.v1`); distinta por origen para no mezclar archivos. */
  readonly storageKey?: string
  /**
   * Carga los archivos, en orden de carga. `null`: el origen no tiene
   * configuración y la sesión es solo local (sin botón de guardar).
   */
  load(): Promise<SourceFile[] | null>
  /**
   * Guarda los archivos modificados (todos o nada). Los fallos esperables
   * (validación, permisos, conflicto) son un `SaveResult` con `saved: false`,
   * no una excepción.
   */
  save(files: SourceFile[]): Promise<SaveResult>
}

/**
 * Sin origen: la sesión vive solo en el navegador (el estado de la
 * aplicación, incluidos los archivos, se persiste en localStorage) y se sale
 * con importar/exportar. Es el almacén de la web sin servidor.
 */
export class LocalStore implements ConfigStore {
  readonly label = 'navegador'
  readonly canSave = false
  readonly readOnlyHint = 'La sesión se guarda en este navegador; usa Exportar para sacar los archivos.'
  async load() { return null }
  async save(): Promise<SaveResult> {
    return { saved: false, error: this.readOnlyHint, status: 400 }
  }
}

/**
 * Archivos de `haro serve --file` (GetConfig/PutConfig del CoreClient). Es un
 * almacén aparte de LocalStore porque depende del cliente y de las
 * capacidades del servidor (`--allow-write`), y porque la validación con
 * `haproxy -c` y la copia `.bak` las hace el servidor al guardar. Sin
 * `--file`, `load()` da `null` y la sesión queda como local.
 */
export class ServedStore implements ConfigStore {
  readonly label = 'disco'
  readonly saveHint = 'Se valida la configuración completa antes de escribir y se deja una copia .bak de cada archivo.'
  private writable = false

  constructor(private client: CoreClient) {}

  get canSave() { return this.writable }
  get readOnlyHint() { return 'No hay permiso de escritura: inicia haro serve con --allow-write para guardar en disco.' }

  async load() {
    const [caps, files] = await Promise.all([this.client.capabilities().catch(() => null), this.client.getConfig().catch(() => null)])
    this.writable = !!caps?.canWrite
    return files && files.length ? files : null
  }

  save(files: SourceFile[]): Promise<SaveResult> { return this.client.putFiles(files) }
}
