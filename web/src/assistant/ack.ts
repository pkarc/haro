import { confirmBox } from '../components/dialogs'
import type { Key, T } from '../i18n'
import type { AppStoreApi } from '../store'

/**
 * The first time the assistant is used in the session, asks the user to confirm that
 * the configuration (or the section) is sent to the Anthropic API. It shares the flag
 * (`chatAck`) with the chat, so the question is asked only once. `msgKey` picks the wording.
 */
export async function ensureAssistAck(api: AppStoreApi, t: T, msgKey: Key = 'chat.confirmSend'): Promise<boolean> {
  const s = api.getState()
  if (s.chatAck) return true
  const model = s.capabilities?.assistantModel
  if (!(await confirmBox(t(msgKey, { model: model || t('chat.modelConfigured') }), t('chat.confirmSendOk')))) return false
  api.getState().ackChat()
  return true
}
