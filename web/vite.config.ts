import { fileURLToPath } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { VitePWA } from 'vite-plugin-pwa'

const src = fileURLToPath(new URL('./src', import.meta.url))

// API 端口是 deploy/docker-compose.yml 里 api 服务的宿主端口。
// 生产环境不需要这一段：SPA 与 API 同域，由前端那份 nginx 反代吸收，
// 所以前端代码里的 base 始终是写死的 /api/v1。
const API_ORIGIN = 'http://localhost:8081'
const MEDIA_ORIGIN = 'http://localhost:8093'

// 走代理而不是直连，是为了让浏览器眼里所有请求都同源 ——
// 这样 /api 不需要任何 CORS 配置，cookie 与 referer 行为也和生产一致。
//
// dev 与 preview 用同一份：push 在 vite dev 里根本没法测（开发模式不注册
// Service Worker），只能构建后 preview 着测，那份代理不能少。
const proxy = {
  // 实时通道单独一条，且必须排在 '/api' 前面（vite 按声明顺序匹配）。
  //
  // 两条代理的区别只有 changeOrigin。上面那条把 Host 改写成后端的地址，
  // 而 WebSocket 握手会校验「Origin 的 host 等于请求的 Host」（库自带的
  // 同源检查，见 internal/ws/hub.go 的 Serve）—— 改写之后 Origin 是
  // localhost:5173、Host 是 localhost:8081，握手必然 403。
  // 这一条保留原始 Host，并打开 ws 升级。
  '/api/v1/ws': { target: API_ORIGIN, changeOrigin: false, ws: true },
  '/api': { target: API_ORIGIN, changeOrigin: true },
  '/img': { target: MEDIA_ORIGIN, changeOrigin: true },
  // 预签名直传不走这里：它的 URL 由服务端按 MINIO_PUBLIC_ENDPOINT 签好，
  // Host 是签名的一部分，改写会让签名失效。所以上传是真正的跨域请求，
  // CORS 由 MinIO 回答（见 deploy/nginx/default.conf.template）。
}

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      // 只有构建产物才注册 SW：开发期挂着一个缓存住的 SW，
      // 改了代码刷新还是旧的，排查起来非常费劲。
      devOptions: { enabled: false },
      registerType: 'autoUpdate',
      manifest: {
        name: '有信',
        short_name: '有信',
        description: '我们替你留意，然后把信递到手上',
        lang: 'zh-CN',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        background_color: '#EFF1EC',
        theme_color: '#EFF1EC',
        icons: [
          { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
          {
            // maskable 必须单独一张：系统会按自己的形状裁切，
            // 内容得缩进安全区，直接复用上面那张会被裁掉边
            src: '/icons/icon-512-maskable.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        // 推送的两个事件处理器挂在生成的 SW 上，而不是另写一个完整的 SW：
        // 缓存与导航兜底继续由 Workbox 负责，推送只是多两个监听器。
        // 文件在 public/push-sw.js，原样复制到构建产物根目录。
        importScripts: ['push-sw.js'],
        // 图片是 /img/ 下的长缓存对象，交给浏览器 HTTP 缓存即可，
        // 不塞进 precache —— 否则安装 SW 时要拉一堆图片
        globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
        navigateFallback: '/index.html',
        navigateFallbackDenylist: [/^\/api\//, /^\/img\//],
      },
    }),
  ],
  resolve: {
    alias: { '@': src },
  },
  build: {
    rollupOptions: {
      output: {
        // 拆开 vendor：应用代码改一行就发一次版，而 React、路由、zod
        // 这些几乎不动 —— 混在一个文件里等于每次发版都让用户重下 570KB。
        //
        // 用函数而不是对象写法：对象写法只按包名精确匹配，「react-dom/client」
        // 这类子路径会漏网，最后 react-dom 还是掉进应用 chunk 里。
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (/node_modules\/(react|react-dom|react-router|scheduler)\//.test(id)) return 'react'
          if (id.includes('@tanstack')) return 'query'
          if (/node_modules\/(react-hook-form|zod|@hookform)\//.test(id)) return 'form'
          if (/node_modules\/(@radix-ui|lucide-react|class-variance-authority|tailwind-merge|clsx)\//.test(id))
            return 'ui'
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy,
  },
  preview: {
    port: 4173,
    proxy,
  },
})
