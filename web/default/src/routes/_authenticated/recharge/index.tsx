import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/_authenticated/recharge/')({
  component: RouteComponent,
})

function RouteComponent() {
  return (
    <div className='flex min-h-[60vh] items-center justify-center p-6'>
      <Card className='w-full max-w-lg'>
        <CardHeader>
          <CardTitle className='text-center text-2xl font-semibold tracking-tight'>
            代充 GPT
          </CardTitle>
        </CardHeader>
        <CardContent className='space-y-4 text-center'>
          <p className='text-muted-foreground text-base'>
            如有需要请联系管理员
          </p>
          <p className='text-primary text-lg font-semibold'>QQ：3756686882</p>
        </CardContent>
      </Card>
    </div>
  )
}
