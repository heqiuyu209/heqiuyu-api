/**
 * 2FA challenge helpers.
 *
 * The backend routes every login completion path (password, OAuth providers,
 * WeChat, Telegram, Passkey) through the same helper. When the authenticating
 * account has 2FA enabled, no login session is established and the response is:
 *
 *   { success: true, message: '<i18n text>', data: { require_2fa: true } }
 *
 * Only `pending_username` / `pending_user_id` are stored in the session; the
 * user must finish the challenge with `POST /api/user/login/2fa`.
 */
export type TwoFactorChallengePayload = {
  require_2fa?: boolean
}

/**
 * Returns true when a login response is a 2FA challenge instead of a session.
 */
export function isTwoFactorRequired(data: unknown): boolean {
  if (!data || typeof data !== 'object') return false
  return Boolean((data as TwoFactorChallengePayload).require_2fa)
}
