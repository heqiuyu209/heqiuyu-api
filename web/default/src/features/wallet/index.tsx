import { useState, useEffect, useCallback } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getSelf } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SectionPageLayout } from '@/components/layout'
import { AffiliateRewardsCard } from './components/affiliate-rewards-card'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { TransferDialog } from './components/dialogs/transfer-dialog'
import { WalletStatsCard } from './components/wallet-stats-card'
import { useAffiliate, useRedemption } from './hooks'
import type { UserWalletData } from './types'

interface WalletProps {
  initialShowHistory?: boolean
}

export function Wallet(props: WalletProps) {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [transferDialogOpen, setTransferDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [redemptionCode, setRedemptionCode] = useState('')

  const {
    affiliateLink,
    loading: affiliateLoading,
    transferQuota,
    transferring,
  } = useAffiliate()
  const { redeeming, redeemCode } = useRedemption()

  // Fetch and refresh user data
  const fetchUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    } finally {
      setUserLoading(false)
    }
  }, [])

  useEffect(() => {
    const timeoutId = window.setTimeout(() => {
      void fetchUser()
    }, 0)
    return () => window.clearTimeout(timeoutId)
  }, [fetchUser])

  useEffect(() => {
    if (props.initialShowHistory) {
      const timeoutId = window.setTimeout(() => {
        setBillingDialogOpen(true)
      }, 0)
      window.history.replaceState({}, '', window.location.pathname)
      return () => window.clearTimeout(timeoutId)
    }
  }, [props.initialShowHistory])

  // Handle redemption
  const handleRedeem = async () => {
    if (!redemptionCode) return

    const success = await redeemCode(redemptionCode)
    if (success) {
      setRedemptionCode('')
      await fetchUser()
    }
  }

  // Handle transfer
  const handleTransfer = async (amount: number) => {
    const success = await transferQuota(amount)
    if (success) {
      await fetchUser()
    }
    return success
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t('Manage your balance and rewards')}
        </SectionPageLayout.Description>
        <SectionPageLayout.Content>
          <div className='space-y-6'>
            <WalletStatsCard user={user} loading={userLoading} />

            <div className='grid gap-6 lg:grid-cols-3'>
              {/* Left Column - Redemption */}
              <div className='space-y-6 lg:col-span-2'>
                <Card>
                  <CardHeader>
                    <h3 className='text-xl font-semibold tracking-tight'>
                      {t('Redeem Code')}
                    </h3>
                    <p className='text-muted-foreground mt-2 text-sm'>
                      {t(
                        'Enter a redemption code to add quota to your balance'
                      )}
                    </p>
                  </CardHeader>
                  <CardContent className='space-y-4'>
                    <div className='space-y-3'>
                      <Label className='text-muted-foreground text-xs tracking-wider uppercase'>
                        {t('Redemption Code')}
                      </Label>
                      <div className='flex gap-2'>
                        <Input
                          value={redemptionCode}
                          onChange={(e) => setRedemptionCode(e.target.value)}
                          placeholder={t('Enter redemption code')}
                          className='font-mono'
                        />
                        <Button
                          onClick={handleRedeem}
                          disabled={redeeming || !redemptionCode}
                          aria-busy={redeeming}
                          className='shrink-0'
                        >
                          {redeeming && (
                            <Loader2 className='size-4 animate-spin' />
                          )}
                          {t('Redeem')}
                        </Button>
                      </div>
                    </div>
                  </CardContent>
                </Card>

                <Button
                  variant='outline'
                  onClick={() => setBillingDialogOpen(true)}
                  className='w-full'
                >
                  {t('View Billing History')}
                </Button>
              </div>

              {/* Right Column - Affiliate */}
              <div className='space-y-6 lg:col-span-1'>
                <AffiliateRewardsCard
                  user={user}
                  affiliateLink={affiliateLink}
                  onTransfer={() => setTransferDialogOpen(true)}
                  loading={affiliateLoading}
                />
              </div>
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <TransferDialog
        open={transferDialogOpen}
        onOpenChange={setTransferDialogOpen}
        onConfirm={handleTransfer}
        availableQuota={user?.aff_quota ?? 0}
        transferring={transferring}
      />

      <BillingHistoryDialog
        open={billingDialogOpen}
        onOpenChange={setBillingDialogOpen}
      />
    </>
  )
}
