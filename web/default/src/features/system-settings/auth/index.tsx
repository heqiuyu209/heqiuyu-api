import { SettingsPage } from '../components/settings-page'
import type { AuthSettings } from '../types'
import {
  AUTH_DEFAULT_SECTION,
  getAuthSectionContent,
} from './section-registry.tsx'

const defaultAuthSettings: AuthSettings = {
  PasswordLoginEnabled: true,
  PasswordRegisterEnabled: true,
  EmailVerificationEnabled: false,
  RegisterEnabled: true,
  EmailDomainRestrictionEnabled: false,
  EmailAliasRestrictionEnabled: false,
  EmailDomainWhitelist: '',
  GitHubOAuthEnabled: false,
  GitHubClientId: '',
  GitHubClientSecret: '',
  'discord.enabled': false,
  'discord.client_id': '',
  'discord.client_secret': '',
  'oidc.enabled': false,
  'oidc.client_id': '',
  'oidc.client_secret': '',
  'oidc.well_known': '',
  'oidc.authorization_endpoint': '',
  'oidc.token_endpoint': '',
  'oidc.user_info_endpoint': '',
  TelegramOAuthEnabled: false,
  TelegramBotToken: '',
  TelegramBotName: '',
  LinuxDOOAuthEnabled: false,
  LinuxDOClientId: '',
  LinuxDOClientSecret: '',
  LinuxDOMinimumTrustLevel: '0',
  WeChatAuthEnabled: false,
  WeChatServerAddress: '',
  WeChatServerToken: '',
  WeChatAccountQRCodeImageURL: '',
  TurnstileCheckEnabled: false,
  TurnstileSiteKey: '',
  TurnstileSecretKey: '',
  'passkey.enabled': false,
  'passkey.rp_display_name': '',
  'passkey.rp_id': '',
  'passkey.origins': '',
  'passkey.allow_insecure_origin': false,
  'passkey.user_verification': 'preferred',
  'passkey.attachment_preference': '',
  'registration_setting.enabled': true,
  'registration_setting.turnstile_strict': true,
  'registration_setting.ip_attempt_window_seconds': 600,
  'registration_setting.ip_max_attempts_per_window': 10,
  'registration_setting.ip_max_success_per_hour': 5,
  'registration_setting.ip_max_success_per_day': 20,
  'registration_setting.ip_allowlist': '',
  'registration_setting.global_max_success_per_hour': 0,
  'registration_setting.email_cooldown_seconds': 60,
  'registration_setting.email_max_per_day': 5,
  'registration_setting.email_ip_max_per_hour': 10,
  'registration_setting.email_global_max_per_hour': 120,
  'registration_setting.verification_max_attempts': 5,
  'registration_setting.verification_attempt_window_seconds': 600,
  'registration_setting.honeypot_enabled': true,
  'registration_setting.form_token_enabled': false,
  'registration_setting.form_token_min_seconds': 3,
  'registration_setting.form_token_max_minutes': 30,
  'registration_setting.async_email_enabled': true,
  'registration_setting.async_email_queue_size': 256,
  'registration_setting.async_email_workers': 2,
  'registration_setting.async_email_max_per_minute': 60,
  'registration_setting.async_email_breaker_failures': 5,
  'registration_setting.invite_code_required': false,
}

export function AuthSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/auth/$section'
      defaultSettings={defaultAuthSettings}
      defaultSection={AUTH_DEFAULT_SECTION}
      getSectionContent={getAuthSectionContent}
    />
  )
}
