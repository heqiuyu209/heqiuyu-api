import type { AuthSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { BasicAuthSection } from './basic-auth-section'
import { BotProtectionSection } from './bot-protection-section'
import { CustomOAuthSection } from './custom-oauth/custom-oauth-section'
import { OAuthSection } from './oauth-section'
import { PasskeySection } from './passkey-section'
import { RegistrationProtectionSection } from './registration-protection-section'

const AUTH_SECTIONS = [
  {
    id: 'basic-auth',
    titleKey: 'Basic Authentication',
    descriptionKey: 'Configure password-based login and registration',
    build: (settings: AuthSettings) => (
      <BasicAuthSection
        defaultValues={{
          PasswordLoginEnabled: settings.PasswordLoginEnabled,
          PasswordRegisterEnabled: settings.PasswordRegisterEnabled,
          EmailVerificationEnabled: settings.EmailVerificationEnabled,
          RegisterEnabled: settings.RegisterEnabled,
          EmailDomainRestrictionEnabled: settings.EmailDomainRestrictionEnabled,
          EmailAliasRestrictionEnabled: settings.EmailAliasRestrictionEnabled,
          EmailDomainWhitelist: settings.EmailDomainWhitelist,
        }}
      />
    ),
  },
  {
    id: 'oauth',
    titleKey: 'OAuth Integrations',
    descriptionKey: 'Configure third-party authentication providers',
    build: (settings: AuthSettings) => (
      <OAuthSection
        defaultValues={{
          GitHubOAuthEnabled: settings.GitHubOAuthEnabled,
          GitHubClientId: settings.GitHubClientId,
          GitHubClientSecret: settings.GitHubClientSecret,
          'discord.enabled': settings['discord.enabled'],
          'discord.client_id': settings['discord.client_id'],
          'discord.client_secret': settings['discord.client_secret'],
          'oidc.enabled': settings['oidc.enabled'],
          'oidc.client_id': settings['oidc.client_id'],
          'oidc.client_secret': settings['oidc.client_secret'],
          'oidc.well_known': settings['oidc.well_known'],
          'oidc.authorization_endpoint':
            settings['oidc.authorization_endpoint'],
          'oidc.token_endpoint': settings['oidc.token_endpoint'],
          'oidc.user_info_endpoint': settings['oidc.user_info_endpoint'],
          TelegramOAuthEnabled: settings.TelegramOAuthEnabled,
          TelegramBotToken: settings.TelegramBotToken,
          TelegramBotName: settings.TelegramBotName,
          LinuxDOOAuthEnabled: settings.LinuxDOOAuthEnabled,
          LinuxDOClientId: settings.LinuxDOClientId,
          LinuxDOClientSecret: settings.LinuxDOClientSecret,
          LinuxDOMinimumTrustLevel: settings.LinuxDOMinimumTrustLevel,
          WeChatAuthEnabled: settings.WeChatAuthEnabled,
          WeChatServerAddress: settings.WeChatServerAddress,
          WeChatServerToken: settings.WeChatServerToken,
          WeChatAccountQRCodeImageURL: settings.WeChatAccountQRCodeImageURL,
        }}
      />
    ),
  },
  {
    id: 'passkey',
    titleKey: 'Passkey Authentication',
    descriptionKey: 'Configure Passkey (WebAuthn) login settings',
    build: (settings: AuthSettings) => (
      <PasskeySection
        defaultValues={{
          'passkey.enabled': settings['passkey.enabled'],
          'passkey.rp_display_name': settings['passkey.rp_display_name'],
          'passkey.rp_id': settings['passkey.rp_id'],
          'passkey.origins': settings['passkey.origins'],
          'passkey.allow_insecure_origin':
            settings['passkey.allow_insecure_origin'],
          'passkey.user_verification': settings['passkey.user_verification'] as
            'required' | 'preferred' | 'discouraged',
          'passkey.attachment_preference': (settings[
            'passkey.attachment_preference'
          ] === ''
            ? 'none'
            : settings['passkey.attachment_preference']) as
            'none' | 'platform' | 'cross-platform',
        }}
      />
    ),
  },
  {
    id: 'bot-protection',
    titleKey: 'Bot Protection',
    descriptionKey: 'Protect login and registration with Cloudflare Turnstile',
    build: (settings: AuthSettings) => (
      <BotProtectionSection
        defaultValues={{
          TurnstileCheckEnabled: settings.TurnstileCheckEnabled,
          TurnstileSiteKey: settings.TurnstileSiteKey,
          TurnstileSecretKey: settings.TurnstileSecretKey,
        }}
      />
    ),
  },
  {
    id: 'registration-protection',
    titleKey: 'Registration Protection',
    descriptionKey:
      'Configure registration rate limits, honeypot, form token and async email delivery',
    build: (settings: AuthSettings) => (
      <RegistrationProtectionSection
        defaultValues={{
          enabled: settings['registration_setting.enabled'],
          turnstile_strict: settings['registration_setting.turnstile_strict'],
          ip_attempt_window_seconds:
            settings['registration_setting.ip_attempt_window_seconds'],
          ip_max_attempts_per_window:
            settings['registration_setting.ip_max_attempts_per_window'],
          ip_max_success_per_hour:
            settings['registration_setting.ip_max_success_per_hour'],
          ip_max_success_per_day:
            settings['registration_setting.ip_max_success_per_day'],
          ip_allowlist: settings['registration_setting.ip_allowlist'],
          global_max_success_per_hour:
            settings['registration_setting.global_max_success_per_hour'],
          email_cooldown_seconds:
            settings['registration_setting.email_cooldown_seconds'],
          email_max_per_day: settings['registration_setting.email_max_per_day'],
          email_ip_max_per_hour:
            settings['registration_setting.email_ip_max_per_hour'],
          email_global_max_per_hour:
            settings['registration_setting.email_global_max_per_hour'],
          verification_max_attempts:
            settings['registration_setting.verification_max_attempts'],
          verification_attempt_window_seconds:
            settings[
              'registration_setting.verification_attempt_window_seconds'
            ],
          honeypot_enabled: settings['registration_setting.honeypot_enabled'],
          form_token_enabled:
            settings['registration_setting.form_token_enabled'],
          form_token_min_seconds:
            settings['registration_setting.form_token_min_seconds'],
          form_token_max_minutes:
            settings['registration_setting.form_token_max_minutes'],
          async_email_enabled:
            settings['registration_setting.async_email_enabled'],
          async_email_queue_size:
            settings['registration_setting.async_email_queue_size'],
          async_email_workers:
            settings['registration_setting.async_email_workers'],
          async_email_max_per_minute:
            settings['registration_setting.async_email_max_per_minute'],
          async_email_breaker_failures:
            settings['registration_setting.async_email_breaker_failures'],
          invite_code_required:
            settings['registration_setting.invite_code_required'],
        }}
      />
    ),
  },
  {
    id: 'custom-oauth',
    titleKey: 'Custom OAuth',
    descriptionKey: 'Configure custom OAuth providers for user authentication',
    build: () => <CustomOAuthSection />,
  },
] as const

export type AuthSectionId = (typeof AUTH_SECTIONS)[number]['id']

const authRegistry = createSectionRegistry<AuthSectionId, AuthSettings>({
  sections: AUTH_SECTIONS,
  defaultSection: 'basic-auth',
  basePath: '/system-settings/auth',
  urlStyle: 'path',
})

export const AUTH_SECTION_IDS = authRegistry.sectionIds
export const AUTH_DEFAULT_SECTION = authRegistry.defaultSection
export const getAuthSectionNavItems = authRegistry.getSectionNavItems
export const getAuthSectionContent = authRegistry.getSectionContent
