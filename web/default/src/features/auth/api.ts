import { api } from '@/lib/api'
import type {
  LoginPayload,
  LoginResponse,
  Login2FAResponse,
  TwoFAPayload,
  RegisterPayload,
  RegistrationTokenResponse,
  ApiResponse,
} from './types'

// OAuth state is created by the shared implementation in @/lib/oauth so that
// every OAuth entry point carries both aff and invite_code (from localStorage).
export { getOAuthState } from '@/lib/oauth'

// ============================================================================
// Authentication APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Login & Logout
// ----------------------------------------------------------------------------

// User login with username and password
export async function login(payload: LoginPayload) {
  const turnstile = payload.turnstile ?? ''
  const res = await api.post<LoginResponse>(
    `/api/user/login?turnstile=${turnstile}`,
    {
      username: payload.username,
      password: payload.password,
    }
  )
  return res.data
}

// Two-factor authentication login
export async function login2fa(payload: TwoFAPayload) {
  const res = await api.post<Login2FAResponse>('/api/user/login/2fa', payload)
  return res.data
}

// User logout
export async function logout(): Promise<ApiResponse> {
  const res = await api.get('/api/user/logout')
  return res.data
}

// ----------------------------------------------------------------------------
// Password Management
// ----------------------------------------------------------------------------

// Send password reset email
export async function sendPasswordResetEmail(
  email: string,
  turnstile?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/reset_password', {
    params: { email, turnstile },
  })
  return res.data
}

// ----------------------------------------------------------------------------
// OAuth
// ----------------------------------------------------------------------------

// Start GitHub OAuth flow
export async function githubOAuthStart(clientId: string, state: string) {
  const url = `https://github.com/login/oauth/authorize?client_id=${clientId}&state=${state}&scope=user:email`
  window.open(url)
}

// WeChat login by authorization code
export async function wechatLoginByCode(
  code: string,
  inviteCode?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/oauth/wechat', {
    params: { code, invite_code: inviteCode },
  })
  return res.data
}

// ----------------------------------------------------------------------------
// Registration
// ----------------------------------------------------------------------------

// User registration
export async function register(payload: RegisterPayload): Promise<ApiResponse> {
  const res = await api.post(`/api/user/register`, payload, {
    params: { turnstile: payload.turnstile ?? '' },
  })
  return res.data
}

// Get a one-time registration form token (404 when the feature is disabled)
export async function getRegistrationToken(): Promise<RegistrationTokenResponse> {
  const res = await api.get<RegistrationTokenResponse>(
    '/api/user/register/token',
    {
      // 404 when form tokens are disabled; don't show a global error toast.
      skipErrorHandler: true,
    } as Record<string, unknown>
  )
  return res.data
}

// Send email verification code
// purpose="register" scopes the code to registration (and carries the invite
// code when registration requires one); other callers omit it.
export async function sendEmailVerification(
  email: string,
  turnstile?: string,
  inviteCode?: string,
  purpose?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/verification', {
    params: {
      email,
      turnstile,
      invite_code: inviteCode,
      purpose,
    },
  })
  return res.data
}

// Bind email to OAuth account
//
// 后端只注册了 POST /api/oauth/email/bind，body 为 {email, code}；
// 此前用 GET + params 调用会直接 404（按钮可达性验证报告 F1）。
export async function bindEmail(
  email: string,
  code: string
): Promise<ApiResponse> {
  const res = await api.post('/api/oauth/email/bind', { email, code })
  return res.data
}
