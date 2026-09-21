import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, mock, test } from 'bun:test'
import { renderToString } from 'react-dom/server'
import { useBillingHistory } from '../src/features/wallet/hooks/use-billing-history'
import type { AuthUser } from '../src/stores/auth-store'

let currentUser: AuthUser | null = null

// Server rendering normally uses Zustand's initial snapshot. Supply the current
// browser auth state so this test exercises account switches without a DOM.
mock.module('../src/stores/auth-store', () => ({
  useAuthStore: (
    selector: (state: { auth: { user: AuthUser | null } }) => unknown
  ) => selector({ auth: { user: currentUser } }),
}))

afterEach(() => {
  currentUser = null
})

test('a new account cannot render the previous account billing cache', () => {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  })
  function BillingRecords() {
    const { records } = useBillingHistory()
    return <span>{records.map((record) => record.trade_no).join(',')}</span>
  }
  const render = () =>
    renderToString(
      <QueryClientProvider client={client}>
        <BillingRecords />
      </QueryClientProvider>
    )

  currentUser = { id: 1, username: 'first', role: 1 }
  render()
  const firstQuery = client.getQueryCache().getAll()[0]
  client.setQueryData(firstQuery.queryKey, {
    total: 1,
    items: [{ id: 1, trade_no: 'private-order-for-first-account' }],
  })
  expect(render()).toContain('private-order-for-first-account')

  // An expired session resets the auth store without reloading the app.
  currentUser = null
  expect(render()).not.toContain('private-order-for-first-account')
  currentUser = { id: 2, username: 'second', role: 1 }
  expect(render()).not.toContain('private-order-for-first-account')
  client.clear()
})
