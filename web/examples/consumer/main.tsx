// Minimal consumer of the haro web, as the haro-site repo would do with haro as
// a submodule: it imports only from `src/index` (in haro-site it would be
// `../haro/web/src/index`) and provides its own ConfigStore, a previous screen
// (gate), a header and an action button. It also proves that this public API is
// enough to mount the application (see e2e/consumer.spec.ts).
import '../../src/styles.css'
import { mountApp, LocalStore, WasmCoreClient, type ConfigStore, type SaveResult, type SourceFile } from '../../src/index'

const REMOTE = 'backend api\n    balance roundrobin\n    server a1 10.0.0.1:80 check\n'

/** An in-memory «repository» with version control, like GitHub would have. */
class MemoryStore implements ConfigStore {
  readonly label = 'memory/demo@main'
  readonly canSave = true
  readonly saveHint = 'Saved to the main branch of the sample repository.'
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
  /** Simulates somebody else changing the repository. */
  remoteChange(text: string) { this.files = [{ path: 'haproxy.cfg', text }]; this.version++ }
}

const store = new MemoryStore()
Object.assign(window, { __store: store })

mountApp({
  client: new WasmCoreClient(),
  store: new LocalStore(),
  container: document.getElementById('root')!,
  header: <div data-testid="consumer-header" style={{ padding: '4px 12px' }}>haro-site · demo</div>,
  actions: <button className="tbtn" data-testid="consumer-action" onClick={() => alert('consumer action')}>Action</button>,
  gate: ({ enter }) => (
    <div data-testid="gate" style={{ padding: 24 }}>
      <h2>Choose a repository</h2>
      <button className="btn primary" data-testid="gate-enter" onClick={() => enter(store)}>Enter {store.label}</button>
    </div>
  ),
})
