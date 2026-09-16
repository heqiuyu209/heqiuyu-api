import { useCallback, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'
import { getUserBillingHistory, isApiSuccess } from '../api'
import type { BillingHistoryResponse, TopupRecord } from '../types'

// ============================================================================
// Billing History Hook
// ============================================================================

interface UseBillingHistoryOptions {
  /** Initial page number */
  initialPage?: number
  /** Initial page size */
  initialPageSize?: number
}

/**
 * Stable React Query key for the current user's billing history.
 */
export function billingHistoryQueryKey(
  page: number,
  pageSize: number,
  keyword: string
): readonly ['wallet', 'billing-history', number, number, string] {
  return ['wallet', 'billing-history', page, pageSize, keyword] as const
}

/**
 * Billing history for the current user only.
 *
 * The admin (all users) view and the manual "complete order" action were
 * removed together with the backend routes that served them.
 */
export function useBillingHistory(options: UseBillingHistoryOptions = {}) {
  const { initialPage = 1, initialPageSize = 10 } = options

  const [page, setPage] = useState(initialPage)
  const [pageSize, setPageSize] = useState(initialPageSize)
  const [keyword, setKeyword] = useState('')

  const query = useQuery({
    queryKey: billingHistoryQueryKey(page, pageSize, keyword),
    queryFn: async (): Promise<BillingHistoryResponse> => {
      const response = await getUserBillingHistory(page, pageSize, keyword)

      if (!isApiSuccess(response) || !response.data) {
        throw new Error(
          response.message || i18next.t('Failed to load billing history')
        )
      }

      return response.data
    },
  })

  const error = query.error

  useEffect(() => {
    if (!error) return

    // eslint-disable-next-line no-console
    console.error('Failed to fetch billing history:', error)
    toast.error(
      error instanceof Error && error.message
        ? error.message
        : i18next.t('Failed to load billing history')
    )
  }, [error])

  /**
   * Change page
   */
  const handlePageChange = useCallback((newPage: number) => {
    setPage(newPage)
  }, [])

  /**
   * Change page size
   */
  const handlePageSizeChange = useCallback((newPageSize: number) => {
    setPageSize(newPageSize)
    setPage(1) // Reset to first page when changing page size
  }, [])

  /**
   * Search by keyword
   */
  const handleSearch = useCallback((newKeyword: string) => {
    setKeyword(newKeyword)
    setPage(1) // Reset to first page when searching
  }, [])

  const records: TopupRecord[] = query.data?.items ?? []
  const total = query.data?.total ?? 0

  return {
    records,
    total,
    page,
    pageSize,
    keyword,
    loading: query.isPending,
    handlePageChange,
    handlePageSizeChange,
    handleSearch,
    refresh: query.refetch,
  }
}
