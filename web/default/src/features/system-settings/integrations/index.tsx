import { SettingsPage } from '../components/settings-page'
import type { IntegrationSettings as IntegrationSettingsType } from '../types'
import {
  INTEGRATIONS_DEFAULT_SECTION,
  getIntegrationsSectionContent,
} from './section-registry.tsx'

const defaultIntegrationSettings: IntegrationSettingsType = {
  SMTPServer: '',
  SMTPPort: '',
  SMTPAccount: '',
  SMTPFrom: '',
  SMTPToken: '',
  SMTPSSLEnabled: false,
  SMTPForceAuthLogin: false,
  WorkerUrl: '',
  WorkerValidKey: '',
  WorkerAllowHttpImageRequestEnabled: false,
  ChannelDisableThreshold: '',
  QuotaRemindThreshold: '',
  AutomaticDisableChannelEnabled: false,
  AutomaticEnableChannelEnabled: false,
  AutomaticDisableKeywords: '',
  AutomaticDisableStatusCodes: '401',
  AutomaticRetryStatusCodes:
    '100-199,300-399,401-407,409-499,500-503,505-523,525-599',
  'monitor_setting.auto_test_channel_enabled': false,
  'monitor_setting.auto_test_channel_minutes': 10,
  'model_deployment.ionet.api_key': '',
  'model_deployment.ionet.enabled': false,
}

export function IntegrationSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/integrations/$section'
      defaultSettings={defaultIntegrationSettings}
      defaultSection={INTEGRATIONS_DEFAULT_SECTION}
      getSectionContent={getIntegrationsSectionContent}
    />
  )
}
