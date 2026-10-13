import z from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Models } from '@/features/models'
import {
  MODELS_SECTION_IDS,
  MODELS_DEFAULT_SECTION,
} from '@/features/models/section-registry'

const modelsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(10),
  filter: z.string().optional().catch(''),
  vendor: z.array(z.string()).optional().catch([]),
  status: z.array(z.string()).optional().catch([]),
  sync: z.array(z.string()).optional().catch([]),
  dPage: z.number().optional().catch(1),
  dPageSize: z.number().optional().catch(10),
  dFilter: z.string().optional().catch(''),
  dStatus: z.array(z.string()).optional().catch([]),
  // 深链到某个 io.net 部署：进入部署分区后直接打开它的详情弹窗
  deploymentId: z.string().optional(),
})

export const Route = createFileRoute('/_authenticated/models/$section')({
  beforeLoad: ({ params }) => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({
        to: '/403',
      })
    }

    const validSections = MODELS_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/models/$section',
        params: { section: MODELS_DEFAULT_SECTION },
      })
    }
  },
  validateSearch: modelsSearchSchema,
  component: Models,
})
