import { ConnectCoreClient } from '../core/client'
import { createAppStore } from './store'

export const coreClient = new ConnectCoreClient()
export const useApp = createAppStore(coreClient)
