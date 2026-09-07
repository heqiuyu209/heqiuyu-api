import { useTranslation } from 'react-i18next'
import { Card } from '@/components/ui/card'
import { PublicLayout } from '@/components/layout'
import { Markdown } from '@/components/ui/markdown'
import { apiGuideMarkdown } from './guide-content'

/**
 * 站内「文档」页：渲染项目根 README.md 中的《Api使用指南》正文，
 * 与现有深色主题协调（通过 markdown.tsx 的 prose + dark:prose-invert 实现）。
 */
export function DocsPage() {
  const { t } = useTranslation()
  return (
    <PublicLayout>
      <div className='mx-auto max-w-4xl space-y-6 py-12'>
        <Card className='p-6 sm:p-8'>
          <Markdown className='prose-neutral dark:prose-invert max-w-none'>
            {apiGuideMarkdown}
          </Markdown>
        </Card>
        <p className='text-muted-foreground text-center text-sm'>
          {t('Copyright Technology is easy, and knowledge is shared.')}
        </p>
      </div>
    </PublicLayout>
  )
}
