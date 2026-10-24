import { useEffect, useState } from 'react'
import { z } from 'zod'
import { useForm, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const registrationProtectionSchema = z.object({
  enabled: z.boolean(),
  turnstile_strict: z.boolean(),
  ip_attempt_window_seconds: z.coerce.number().int().min(0),
  ip_max_attempts_per_window: z.coerce.number().int().min(0),
  ip_max_success_per_hour: z.coerce.number().int().min(0),
  ip_max_success_per_day: z.coerce.number().int().min(0),
  ip_allowlist: z.string(),
  global_max_success_per_hour: z.coerce.number().int().min(0),
  email_cooldown_seconds: z.coerce.number().int().min(0),
  email_max_per_day: z.coerce.number().int().min(0),
  email_ip_max_per_hour: z.coerce.number().int().min(0),
  email_global_max_per_hour: z.coerce.number().int().min(0),
  verification_max_attempts: z.coerce.number().int().min(0),
  verification_attempt_window_seconds: z.coerce.number().int().min(0),
  honeypot_enabled: z.boolean(),
  form_token_enabled: z.boolean(),
  form_token_min_seconds: z.coerce.number().int().min(0),
  form_token_max_minutes: z.coerce.number().int().min(0),
  async_email_enabled: z.boolean(),
  async_email_queue_size: z.coerce.number().int().min(1),
  async_email_workers: z.coerce.number().int().min(1),
  async_email_max_per_minute: z.coerce.number().int().min(0),
  async_email_breaker_failures: z.coerce.number().int().min(0),
  invite_code_required: z.boolean(),
})

type RegistrationProtectionFormValues = z.infer<
  typeof registrationProtectionSchema
>

type RegistrationProtectionSectionProps = {
  defaultValues: RegistrationProtectionFormValues
}

export function RegistrationProtectionSection(
  props: RegistrationProtectionSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaultValues = props.defaultValues

  const form = useForm<RegistrationProtectionFormValues>({
    resolver: zodResolver(
      registrationProtectionSchema
    ) as unknown as Resolver<RegistrationProtectionFormValues>,
    defaultValues,
  })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const [myInviteCode, setMyInviteCode] = useState('')
  const [isInviteCodeLoading, setIsInviteCodeLoading] = useState(true)

  // Load the current admin's own invite code. GET /api/user/aff generates one
  // on first use, so this is also the "ensure my invite code exists" call.
  useEffect(() => {
    let active = true
    api
      .get<{ success: boolean; message?: string; data?: string }>(
        '/api/user/aff',
        { skipErrorHandler: true } as Record<string, unknown>
      )
      .then((res) => {
        if (!active) return
        const code = res.data?.data
        if (res.data?.success && typeof code === 'string') {
          setMyInviteCode(code)
        } else {
          toast.error(t('Failed to load invite code'))
        }
      })
      .catch(() => {
        if (active) toast.error(t('Failed to load invite code'))
      })
      .finally(() => {
        if (active) setIsInviteCodeLoading(false)
      })
    return () => {
      active = false
    }
  }, [t])

  const handleCopyInviteCode = async () => {
    if (!myInviteCode) return
    try {
      await navigator.clipboard.writeText(myInviteCode)
      toast.success(t('Copied to clipboard'))
    } catch (_error) {
      toast.error(t('Copy failed'))
    }
  }

  const { isDirty, isSubmitting } = form.formState

  const onSubmit = async (values: RegistrationProtectionFormValues) => {
    const changedKeys = (
      Object.keys(values) as Array<keyof RegistrationProtectionFormValues>
    ).filter((key) => values[key] !== defaultValues[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({
        key: `registration_setting.${key}`,
        value: String(values[key]),
      })
    }

    form.reset(values)
  }

  return (
    <SettingsSection
      title={t('Registration Protection')}
      description={t(
        'Configure registration rate limits, honeypot, form token and async email delivery'
      )}
    >
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className='space-y-6'
          autoComplete='off'
        >
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Enable registration protection')}
                  </FormLabel>
                  <FormDescription>
                    {t('Master switch for all registration defense features')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='turnstile_strict'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Strict Turnstile verification')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Verify Turnstile on every registration and verification code request instead of reusing the session'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='ip_attempt_window_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('IP attempt window (seconds)')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='ip_max_attempts_per_window'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max attempts per IP window')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='ip_max_success_per_hour'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Max successful registrations per IP per hour')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='ip_max_success_per_day'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Max successful registrations per IP per day')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='ip_allowlist'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('IP allowlist (comma separated IP/CIDR)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      placeholder='1.2.3.4, 10.0.0.0/8'
                      autoComplete='off'
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='global_max_success_per_hour'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t(
                      'Global max successful registrations per hour (0 = disabled)'
                    )}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='email_cooldown_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Email code cooldown (seconds)')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='email_max_per_day'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Max verification codes per email per day')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='email_ip_max_per_hour'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Max verification codes per IP per hour')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='email_global_max_per_hour'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Global max verification codes per hour')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='verification_max_attempts'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max verification code attempts')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='verification_attempt_window_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Verification attempt window (seconds)')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='honeypot_enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Enable honeypot field')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Reject registrations that fill the hidden website field'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='form_token_enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Enable registration form token')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Require a one-time form token issued by the server when registering'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='form_token_min_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum form fill time (seconds)')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='form_token_max_minutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Form token max validity (minutes)')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='async_email_enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Enable async email delivery')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Send verification and password reset emails asynchronously'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='async_email_queue_size'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Async email queue size (restart required)')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={1} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='async_email_workers'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Async email worker count (restart required)')}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={1} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='async_email_max_per_minute'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max email sends per minute')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='async_email_breaker_failures'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t(
                      'Consecutive email failures before circuit breaker (breaker lasts 60s)'
                    )}
                  </FormLabel>
                  <FormControl>
                    <Input type='number' min={0} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='invite_code_required'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Require an invite code to register')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      "New users must provide a valid invite code (an existing user's aff code) to register"
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <Button
            type='submit'
            disabled={!isDirty || updateOption.isPending || isSubmitting}
          >
            {updateOption.isPending || isSubmitting
              ? t('Saving...')
              : t('Save Changes')}
          </Button>
        </form>
      </Form>

      <div className='space-y-2 rounded-lg border p-4'>
        <div className='space-y-0.5'>
          <div className='text-base font-medium'>{t('My Invite Code')}</div>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Invite code is generated automatically. Share it with the people you want to invite.'
            )}
          </p>
        </div>
        <div className='flex items-center gap-2'>
          <Input
            readOnly
            value={myInviteCode}
            className='font-mono'
            aria-label={t('My Invite Code')}
          />
          <Button
            type='button'
            variant='outline'
            onClick={handleCopyInviteCode}
            disabled={!myInviteCode || isInviteCodeLoading}
          >
            {t('Copy')}
          </Button>
        </div>
      </div>
    </SettingsSection>
  )
}
