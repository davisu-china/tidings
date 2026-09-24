/*
 * Web Push 的接收端。
 *
 * 这个文件不参与打包：它躺在 public/ 下，原样复制到构建产物的根目录，
 * 再由 vite.config.ts 里 workbox.importScripts 挂进 Workbox 生成的那个
 * service worker。
 *
 * 为什么不自己写一个完整的 SW：Workbox 生成的那个负责预缓存与导航兜底
 * （离线能开、发版能更新），而推送只多两个事件处理器。为两个事件
 * 接管整个 SW 的生命周期，等于把缓存策略一起接了过来。
 *
 * 载荷格式见后端 internal/pkg/push 的 Payload：{title, body, url, tag}。
 * 正文里没有对方的昵称和照片 —— 推送体要经推送服务商中转，它在站外。
 */

/* eslint-env serviceworker */

// 载荷解析不出来时的兜底文案。宁可显示一句通用的话，
// 也不要因为 JSON 有问题就整条推送不显示 —— 用户看到的是「什么都没发生」，
// 而实际上服务端已经记成投递成功了。
const FALLBACK = { title: '有信', body: '去看看', url: '/' }

self.addEventListener('push', (event) => {
  let data = FALLBACK
  try {
    // 无载荷的推送是合法的（服务端只用来「戳一下」），
    // 所以 data 为空不是异常路径，走兜底文案。
    if (event.data) data = { ...FALLBACK, ...event.data.json() }
  } catch {
    data = FALLBACK
  }

  event.waitUntil(
    self.registration.showNotification(data.title, {
      body: data.body,
      icon: '/icons/icon-192.png',
      // 不传 badge：它是 Android 状态栏那个单色小图标，
      // 而手头只有彩色的应用图标，传进去会被系统压成一片难辨的剪影。
      // 不传时 Android 用应用图标兜底，比传一张错的强。
      //
      // tag 相同的通知互相覆盖。服务端按「批」给 tag（同一批的几条共用一个），
      // 所以库里是每条一行、用户那里只看到一条 —— 打扰按批算。
      // tag 相同的通知互相覆盖，覆盖时不响铃不震动（renotify 默认就是
      // false，不显式写 —— 写 true 却让 tag 为空时浏览器会直接抛错）。
      tag: data.tag,
      lang: 'zh-CN',
      data: { url: data.url },
    }),
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()

  const url = (event.notification.data && event.notification.data.url) || '/'
  const target = new URL(url, self.location.origin)

  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })

      // 先找已经开着的站内窗口。有就聚焦它，而不是永远新开一个 ——
      // 否则 App 已经在前台时点通知，会多出一个一模一样的副本。
      for (const client of windows) {
        if (new URL(client.url).origin !== target.origin) continue

        await client.focus()
        // 已经在目标页上就不再导航：导航会重新加载整页，
        // 把用户正在填的东西冲掉，而他只是点了一下通知。
        if (new URL(client.url).pathname !== target.pathname) {
          // navigate 对未受本 SW 接管的窗口可能失败。聚焦已经做到了，
          // 通知的目的就是把人带回前台，所以这里失败了不往上抛。
          try {
            await client.navigate(target.href)
          } catch {
            /* 忽略 */
          }
        }
        return
      }

      await self.clients.openWindow(target.href)
    })(),
  )
})
