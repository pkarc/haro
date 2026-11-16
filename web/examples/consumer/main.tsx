// Consumidor mínimo de la web de haro, como lo haría el repo haro-site con haro
// como submódulo: importa solo de `src/index` (en haro-site sería
// `../haro/web/src/index`), aporta su propio ConfigStore, una pantalla previa
// (gate), una cabecera y un botón de acción. Sirve además de prueba de que esa
// API pública alcanza para montar la aplicación (ver e2e/consumer.spec.ts).
import '../../src/styles.css'
import { mountApp, LocalStore, WasmCoreClient, type ConfigStore, type SaveResult, type SourceFile } from '../../src/index'

const REMOTE = 'backend api\n    balance roundrobin\n    server a1 10.0.0.1:80 check\n'

/** Un «repositorio» en memoria con control de versión, como lo tendría GitHub. */
class MemoryStore implements ConfigStore {
  readonly label = 'memoria/demo@main'
  readonly canSave = true
  readonly saveHint = 'Se guarda en la rama main del repositorio de ejemplo.'
  readonly storageKey = 'haro.consumer.v1'
  files: SourceFile[] = [{ path: 'haproxy.cfg', text: REMOTE }]
  version = 1
  seen = 1
  saved: SourceFile[][] = []
  async load() { this.seen = this.version; return this.files }
  async save(files: SourceFile[]): Promise<SaveResult> {
    if (this.seen !== this.version) return { saved: false, conflict: true }
    this.saved.push(files)
    this.files = files
    this.version++
    this.seen = this.version
    return { saved: true }
  }
  /** Simula que alguien más cambió el repositorio. */
  remoteChange(text: string) { this.files = [{ path: 'haproxy.cfg', text }]; this.version++ }
}

const store = new MemoryStore()
Object.assign(window, { __store: store })

mountApp({
  client: new WasmCoreClient(),
  store: new LocalStore(),
  container: document.getElementById('root')!,
  header: <div data-testid="consumer-header" style={{ padding: '4px 12px' }}>haro-site · demo</div>,
  actions: <button className="tbtn" data-testid="consumer-action" onClick={() => alert('acción del consumidor')}>Acción</button>,
  gate: ({ enter }) => (
    <div data-testid="gate" style={{ padding: 24 }}>
      <h2>Elige un repositorio</h2>
      <button className="btn primary" data-testid="gate-enter" onClick={() => enter(store)}>Entrar en {store.label}</button>
    </div>
  ),
})
