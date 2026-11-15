import { HttpCoreClient } from '../core/client'
import { createAppStore } from './store'

export const coreClient = new HttpCoreClient()
export const useApp = createAppStore(coreClient)
