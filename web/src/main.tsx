import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import * as React from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router/dom'

import { ApiError, setSessionExpiredHandler } from '@/api/client'
import { ToastProvider } from '@/components/ui/toast'
import { router } from '@/router'

import '@/styles/index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 业务错误重试没有意义：性别不可改、昵称含联系方式，重试一百次也是同一个结果。
      // 只对网络层抖动重试一次。
      retry: (count, err) => !(err instanceof ApiError) && count < 1,
      // 切回前台时刷一次。这是个推送型产品，回到页面看到的必须是新的
      refetchOnWindowFocus: true,
    },
    mutations: { retry: false },
  },
})

/**
 * refresh 也救不回来时的终局处理。
 *
 * 用整页跳转而不是 router.navigate：走到这里意味着凭据已经作废，
 * 内存里的一切都该丢掉；而且它可能正发生在某个 loader 执行的过程中，
 * 在 loader 里调 navigate 会和 loader 自己的 redirect 打架。
 */
setSessionExpiredHandler(() => {
  window.location.replace('/login?expired=1')
})

const root = document.getElementById('root')
if (!root) throw new Error('index.html 里缺少 #root')

createRoot(root).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <RouterProvider router={router} />
      </ToastProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
