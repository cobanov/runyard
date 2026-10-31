// The service worker only shows runyard's push notifications; it caches nothing, so the app is never stale.
self.addEventListener('push', (e) => {
  const m = e.data ? e.data.json() : { title: 'runyard' }
  e.waitUntil(
    self.registration.showNotification(m.title, { body: m.body, tag: m.tag, icon: '/icon-192.png', data: { url: m.url || '/' } }),
  )
})

self.addEventListener('notificationclick', (e) => {
  e.notification.close()
  e.waitUntil(self.clients.openWindow(e.notification.data.url))
})
