import { useCallback, useEffect, useMemo, useState } from 'react'
import type { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { PasswordInput } from '@/components/password-input'
import { Turnstile } from '@/components/turnstile'
import {
  register,
  getRegistrationToken,
  wechatLoginByCode,
} from '@/features/auth/api'
import { LegalConsent } from '@/features/auth/components/legal-consent'
import { OAuthProviders } from '@/features/auth/components/oauth-providers'
import { registerFormSchema } from '@/features/auth/constants'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { useEmailVerification } from '@/features/auth/hooks/use-email-verification'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'
import { getAffiliateCode } from '@/features/auth/lib/storage'
import { isTwoFactorRequired } from '@/features/auth/lib/two-factor'

// Shared with lib/oauth.ts (getOAuthState reads the invite code from here).
const INVITE_CODE_STORAGE_KEY = 'invite_code'

export function SignUpForm({
  className,
  ...props
}: React.HTMLAttributes<HTMLFormElement>) {
  const { t } = useTranslation()
  const [isLoading, setIsLoading] = useState(false)
  const [verificationCode, setVerificationCode] = useState('')
  const [agreedToLegal, setAgreedToLegal] = useState(false)
  const [wechatCode, setWeChatCode] = useState('')
  const [isWeChatDialogOpen, setIsWeChatDialogOpen] = useState(false)
  const [isWeChatSubmitting, setIsWeChatSubmitting] = useState(false)
  // Honeypot field: invisible to users, must stay empty
  const [website, setWebsite] = useState('')
  // One-time registration form token
  const [formToken, setFormToken] = useState('')
  // Invite code (only used while registration requires one); persisted so OAuth
  // redirects and page reloads keep it. Restored from localStorage only when the
  // feature is enabled (see the effect below).
  const [inviteCode, setInviteCode] = useState('')
  const legalConsentErrorMessage = t('Please agree to the legal terms first')

  const { status } = useStatus()
  const inviteCodeRequired = Boolean(
    status?.invite_code_required ?? status?.data?.invite_code_required
  )
  const inviteCodeMissing = inviteCodeRequired && !inviteCode.trim()
  const {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    setTurnstileToken,
    validateTurnstile,
    resetSignal,
    resetTurnstile,
  } = useTurnstile()
  const { redirectToLogin, redirectTo2FA, handleLoginSuccess } =
    useAuthRedirect()
  const {
    isSending: isSendingCode,
    secondsLeft,
    isActive,
    sendCode,
  } = useEmailVerification({
    turnstileToken,
    validateTurnstile,
    inviteCode: inviteCodeRequired ? inviteCode.trim() || undefined : undefined,
    purpose: 'register',
    onAfterSend: resetTurnstile,
  })

  const form = useForm<z.infer<typeof registerFormSchema>>({
    resolver: zodResolver(registerFormSchema),
    defaultValues: {
      username: '',
      email: '',
      password: '',
      confirmPassword: '',
    },
  })

  const emailValue = form.watch('email')
  const emailVerificationRequired = !!status?.email_verification
  const hasUserAgreement = Boolean(status?.user_agreement_enabled)
  const hasPrivacyPolicy = Boolean(status?.privacy_policy_enabled)
  const requiresLegalConsent = hasUserAgreement || hasPrivacyPolicy
  const oauthRegisterEnabled =
    status?.oauth_register_enabled ??
    status?.data?.oauth_register_enabled ??
    true
  const hasWeChatLogin = Boolean(status?.wechat_login)
  const registerEnabled =
    status?.register_enabled ?? status?.data?.register_enabled ?? true
  const registrationFormTokenEnabled = Boolean(
    status?.registration_form_token_enabled ??
    status?.data?.registration_form_token_enabled
  )
  const honeypotEnabled = Boolean(
    status?.registration_honeypot_enabled ??
    status?.data?.registration_honeypot_enabled
  )

  const requestRegistrationToken = useCallback(async (): Promise<string> => {
    // Drop the previous token first: it may already be consumed, so a
    // concurrent submit must not reuse it while this refetch is in flight.
    setFormToken('')
    try {
      const res = await getRegistrationToken()
      const token = res?.data?.token ?? ''
      if (res?.success && token) {
        setFormToken(token)
        return token
      }
    } catch (_error) {
      // 404 when the feature is disabled; caller decides what to show
    }
    return ''
  }, [])

  useEffect(() => {
    if (!registerEnabled || !registrationFormTokenEnabled) {
      setFormToken('')
      return
    }
    void requestRegistrationToken()
  }, [registerEnabled, registrationFormTokenEnabled, requestRegistrationToken])

  // Invite code persistence:
  // - feature off: clear the stored value and the form state (never send it);
  // - feature on: restore the stored value once, then keep localStorage in sync
  //   so the OAuth redirect (lib/oauth.ts getOAuthState) can carry it.
  useEffect(() => {
    if (typeof window === 'undefined') return
    if (!inviteCodeRequired) {
      setInviteCode('')
      window.localStorage.removeItem(INVITE_CODE_STORAGE_KEY)
      return
    }
    const trimmed = inviteCode.trim()
    if (!trimmed) {
      const stored = window.localStorage.getItem(INVITE_CODE_STORAGE_KEY)
      if (stored) {
        setInviteCode(stored)
        return
      }
    }
    if (trimmed) {
      window.localStorage.setItem(INVITE_CODE_STORAGE_KEY, trimmed)
    } else {
      window.localStorage.removeItem(INVITE_CODE_STORAGE_KEY)
    }
  }, [inviteCode, inviteCodeRequired])

  // Prefill the invite code from the affiliate code when the user has one
  // (an existing user's aff code is a valid invite code).
  useEffect(() => {
    if (!inviteCodeRequired) return
    setInviteCode((current) => (current.trim() ? current : getAffiliateCode()))
  }, [inviteCodeRequired])

  const wechatQrCodeUrl = useMemo(() => {
    return (
      status?.wechat_qrcode ||
      status?.wechat_qr_code ||
      status?.wechat_qrcode_image_url ||
      status?.wechat_qr_code_image_url ||
      status?.wechat_account_qrcode_image_url ||
      status?.WeChatAccountQRCodeImageURL ||
      status?.data?.wechat_qrcode ||
      status?.data?.WeChatAccountQRCodeImageURL ||
      ''
    )
  }, [status])

  useEffect(() => {
    if (requiresLegalConsent) {
      setAgreedToLegal(false)
    } else {
      setAgreedToLegal(true)
    }
  }, [requiresLegalConsent])

  if (!registerEnabled) {
    return (
      <div className={cn('rounded-lg border p-6 text-center', className)}>
        <h3 className='text-lg font-semibold'>
          {t('Registration is currently closed')}
        </h3>
        <p className='text-muted-foreground mt-2 text-sm'>
          {t(
            'Registration is currently closed, please contact the administrator.'
          )}
        </p>
      </div>
    )
  }

  async function onSubmit(data: z.infer<typeof registerFormSchema>) {
    if (requiresLegalConsent && !agreedToLegal) {
      toast.error(legalConsentErrorMessage)
      return
    }

    // Validate email verification if required
    if (emailVerificationRequired) {
      if (!data.email) {
        toast.error(t('Please enter your email'))
        return
      }
      if (!verificationCode) {
        toast.error(t('Please enter the verification code'))
        return
      }
    }

    // Invite code is mandatory for registration while the feature is enabled
    if (inviteCodeMissing) {
      toast.error(t('Please enter the invite code'))
      return
    }

    // Turnstile tokens are single-use in strict mode
    if (!validateTurnstile()) {
      return
    }

    // Resolve a one-time form token when the feature is enabled
    let token = formToken
    if (registrationFormTokenEnabled) {
      if (!token) {
        token = await requestRegistrationToken()
      }
      if (!token) {
        toast.error(
          t(
            'Failed to prepare registration verification. Please refresh the page and try again.'
          )
        )
        return
      }
    }

    setIsLoading(true)
    try {
      const res = await register({
        username: data.username,
        password: data.password,
        email: data.email || undefined,
        verification_code: verificationCode || undefined,
        aff: getAffiliateCode(),
        turnstile: turnstileToken,
        website: honeypotEnabled ? website : undefined,
        form_token: registrationFormTokenEnabled ? token : undefined,
        invite_code: inviteCodeRequired ? inviteCode.trim() : undefined,
      })

      if (res?.success) {
        toast.success(t('Account created! Please sign in'))
        redirectToLogin()
        return
      }

      // The failed attempt may have consumed the form/turnstile token, so
      // refresh both before the next submit.
      resetTurnstile()
      if (registrationFormTokenEnabled) {
        void requestRegistrationToken()
      }
    } catch (_error) {
      // Errors are handled by global interceptor
      resetTurnstile()
      if (registrationFormTokenEnabled) {
        void requestRegistrationToken()
      }
    } finally {
      setIsLoading(false)
    }
  }

  async function handleSendVerificationCode() {
    if (inviteCodeMissing) {
      toast.error(t('Please enter the invite code'))
      return
    }
    await sendCode(emailValue || '')
  }

  const handleOpenWeChatDialog = () => {
    if (requiresLegalConsent && !agreedToLegal) {
      toast.error(legalConsentErrorMessage)
      return
    }

    if (inviteCodeMissing) {
      toast.error(t('Please enter the invite code'))
      return
    }

    setIsWeChatDialogOpen(true)
  }

  const handleWeChatDialogChange = (open: boolean) => {
    setIsWeChatDialogOpen(open)
    if (!open) {
      setWeChatCode('')
      setIsWeChatSubmitting(false)
    }
  }

  async function handleWeChatLogin() {
    if (inviteCodeMissing) {
      toast.error(t('Please enter the invite code'))
      return
    }

    if (!wechatCode.trim()) {
      toast.error(t('Please enter the verification code'))
      return
    }

    setIsWeChatSubmitting(true)
    try {
      const res = await wechatLoginByCode(
        wechatCode,
        inviteCodeRequired ? inviteCode.trim() : undefined
      )
      if (res?.success) {
        if (isTwoFactorRequired(res.data)) {
          toast.info(t('Please enter the authentication code.'))
          handleWeChatDialogChange(false)
          redirectTo2FA()
          return
        }

        await handleLoginSuccess(res.data as { id?: number } | null)
        toast.success(t('Signed in via WeChat'))
        handleWeChatDialogChange(false)
      } else {
        toast.error(res?.message || t('Login failed'))
      }
    } catch (_error) {
      toast.error(t('Login failed'))
    } finally {
      setIsWeChatSubmitting(false)
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className={cn('grid gap-4', className)}
        {...props}
      >
        {/* Honeypot: hidden from users and assistive tech; bots that fill it are rejected */}
        {honeypotEnabled && (
          <div
            aria-hidden='true'
            className='pointer-events-none absolute -left-[9999px] h-0 w-0 overflow-hidden'
          >
            <label htmlFor='website'>Website</label>
            <input
              id='website'
              name='website'
              type='text'
              tabIndex={-1}
              autoComplete='off'
              value={website}
              onChange={(event) => setWebsite(event.target.value)}
            />
          </div>
        )}

        {/* Username Field */}
        <FormField
          control={form.control}
          name='username'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Username')}</FormLabel>
              <FormControl>
                <Input placeholder={t('Enter your username')} {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Password Field */}
        <FormField
          control={form.control}
          name='password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Password')}</FormLabel>
              <FormControl>
                <PasswordInput
                  placeholder={t('Enter password (8-20 characters)')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Confirm Password Field */}
        <FormField
          control={form.control}
          name='confirmPassword'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Confirm password')}</FormLabel>
              <FormControl>
                <PasswordInput placeholder={t('Confirm password')} {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Invite code: only shown (and sent) while registration requires one */}
        {inviteCodeRequired && (
          <div className='space-y-2'>
            <Label htmlFor='invite-code'>{t('Invite Code')}</Label>
            <Input
              id='invite-code'
              placeholder={t('Enter the invite code')}
              value={inviteCode}
              onChange={(event) => setInviteCode(event.target.value)}
              autoComplete='off'
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Invite code is required to register. Please contact the administrator if you do not have one.'
              )}
            </p>
          </div>
        )}

        {/* Email Verification Section */}
        {emailVerificationRequired && (
          <>
            {/* Email Field */}
            <FormField
              control={form.control}
              name='email'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Email (required for verification)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('name@example.com')}
                      type='email'
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            {/* Verification Code Field */}
            <div className='flex items-end gap-2'>
              <div className='flex-1'>
                <Input
                  placeholder={t('Verification code')}
                  value={verificationCode}
                  onChange={(e) => setVerificationCode(e.target.value)}
                />
              </div>
              <Button
                variant='outline'
                type='button'
                disabled={
                  isLoading ||
                  isSendingCode ||
                  isActive ||
                  !emailValue ||
                  inviteCodeMissing
                }
                onClick={handleSendVerificationCode}
              >
                {isActive ? (
                  t('Resend ({{seconds}}s)', { seconds: secondsLeft })
                ) : isSendingCode ? (
                  <Loader2 className='h-4 w-4 animate-spin' />
                ) : (
                  t('Send code')
                )}
              </Button>
            </div>
          </>
        )}

        {/* Turnstile: rendered whenever enabled, independent from email verification */}
        {isTurnstileEnabled && (
          <div className='mt-2'>
            <Turnstile
              siteKey={turnstileSiteKey}
              onVerify={setTurnstileToken}
              resetSignal={resetSignal}
            />
          </div>
        )}

        <LegalConsent
          status={status}
          checked={agreedToLegal}
          onCheckedChange={setAgreedToLegal}
          className='mt-1'
        />

        {/* Submit Button */}
        <Button
          className='mt-2 w-full justify-center gap-2'
          disabled={
            isLoading ||
            (requiresLegalConsent && !agreedToLegal) ||
            inviteCodeMissing
          }
        >
          {isLoading ? <Loader2 className='h-4 w-4 animate-spin' /> : null}
          {t('Create account')}
        </Button>

        {oauthRegisterEnabled && (
          <OAuthProviders
            status={status}
            disabled={isLoading || (requiresLegalConsent && !agreedToLegal)}
            inviteCodeMissing={inviteCodeMissing}
            onWeChatLogin={hasWeChatLogin ? handleOpenWeChatDialog : undefined}
            isWeChatLoading={isWeChatSubmitting}
            className='pt-2'
          />
        )}
      </form>

      {hasWeChatLogin && (
        <Dialog
          open={isWeChatDialogOpen}
          onOpenChange={handleWeChatDialogChange}
        >
          <DialogContent className='max-w-sm'>
            <DialogHeader className='text-left'>
              <DialogTitle>{t('WeChat sign in')}</DialogTitle>
              <DialogDescription>
                {t(
                  'Scan the QR code to follow the official account and reply with “验证码” to receive your verification code.'
                )}
              </DialogDescription>
            </DialogHeader>

            {wechatQrCodeUrl ? (
              <div className='flex justify-center'>
                <img
                  src={wechatQrCodeUrl}
                  alt={t('WeChat login QR code')}
                  className='h-40 w-40 rounded-md border object-contain'
                />
              </div>
            ) : (
              <p className='text-muted-foreground text-sm'>
                {t('QR code is not configured. Please contact support.')}
              </p>
            )}

            <div className='grid gap-2'>
              <Label htmlFor='wechat-code'>{t('Verification code')}</Label>
              <Input
                id='wechat-code'
                placeholder={t('Enter the verification code')}
                value={wechatCode}
                onChange={(event) => setWeChatCode(event.target.value)}
                autoComplete='one-time-code'
              />
            </div>

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                onClick={() => handleWeChatDialogChange(false)}
                disabled={isWeChatSubmitting}
              >
                {t('Cancel')}
              </Button>
              <Button
                type='button'
                onClick={handleWeChatLogin}
                disabled={
                  isWeChatSubmitting ||
                  !wechatCode.trim() ||
                  (requiresLegalConsent && !agreedToLegal)
                }
                className='gap-2'
              >
                {isWeChatSubmitting ? (
                  <Loader2 className='h-4 w-4 animate-spin' />
                ) : null}
                {t('Confirm')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </Form>
  )
}
