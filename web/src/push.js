// Web push: the browser subscribes with its push service, the server sends there; the service worker shows them.
export const pushSupported = 'serviceWorker' in navigator && 'PushManager' in window && window.isSecureContext

const post = (path, body) =>
  fetch('/api/v1/push/' + path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })

export async function pushOn() {
  const reg = await navigator.serviceWorker.getRegistration()
  return !!(reg && (await reg.pushManager.getSubscription()))
}

// setPush subscribes (asking for permission) or unsubscribes, and returns whether notifications are now on.
export async function setPush(on) {
  const reg = await navigator.serviceWorker.register('/sw.js')
  await navigator.serviceWorker.ready
  let sub = await reg.pushManager.getSubscription()
  if (!on) {
    if (sub) {
      await post('unsubscribe', sub)
      await sub.unsubscribe()
    }
    return false
  }
  if ((await Notification.requestPermission()) !== 'granted') return false
  const { key } = await (await fetch('/api/v1/push/key')).json()
  const raw = Uint8Array.from(atob(key.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0))
  sub ||= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: raw })
  const r = await post('subscribe', sub)
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText)
  return true
}
