// 1. Lifecycle: Skip waiting on install and claim clients on activate
self.addEventListener('install', function () {
  self.skipWaiting();
});

self.addEventListener('activate', function (event) {
  event.waitUntil(self.clients.claim());
});

// 2. Push event with icon-stripping fallback and renotify only when tag is present
self.addEventListener('push', function (event) {
  let payload = {};
  if (event.data) {
    try {
      const parsed = event.data.json();
      payload = parsed.data || parsed;
    } catch (e) {
      payload = { title: 'RateRudder Notification', body: event.data.text() };
    }
  }

  const title = payload.title || 'RateRudder Notification';
  const options = {
    body: payload.body || '',
    icon: payload.icon || '/logo_192.png',
    badge: payload.badge || '/badge_96.png',
    data: {
      url: payload.url || (payload.data && payload.data.url) || '/dashboard',
      id: payload.id || (payload.data && (payload.data.id || payload.data.logID)) || '',
      ts: Date.now()
    }
  };

  // Only renotify when a tag is present
  if (payload.tag) {
    options.tag = payload.tag;
    options.renotify = true;
  }

  event.waitUntil(
    self.registration.showNotification(title, options)
      .then(function () {
        console.log('[SW] Notification displayed successfully:', title);
      })
      .catch(function (err) {
        console.warn('[SW] Primary showNotification failed, retrying without icons:', err);
        const fallbackOptions = {
          body: options.body,
          data: options.data
        };
        if (options.tag) {
          fallbackOptions.tag = options.tag;
          fallbackOptions.renotify = true;
        }

        return self.registration.showNotification(title, fallbackOptions)
          .then(function () {
            console.log('[SW] Fallback notification displayed successfully');
          })
          .catch(function (fallbackErr) {
            console.error('[SW] Fallback showNotification also failed:', fallbackErr);
          });
      })
  );
});

// 3. Notification click: Navigates and focuses matching window, or opens a new window
self.addEventListener('notificationclick', function (event) {
  event.notification.close();

  const data = event.notification.data || {};
  const relativeUrl = data.url || '/dashboard';
  const targetUrl = new URL(relativeUrl, self.location.origin).href;
  const id = data.id || data.logID || '';

  const clickPromise = id
    ? fetch(`/api/notifications/click?id=${encodeURIComponent(id)}`, {
        method: 'POST',
        credentials: 'same-origin'
      }).catch(function (err) {
        console.warn('[SW] Failed to record notification click:', err);
      })
    : Promise.resolve();

  const navPromise = self.clients
    .matchAll({ type: 'window', includeUncontrolled: true })
    .then(function (windowClients) {
      for (let i = 0; i < windowClients.length; i++) {
        const client = windowClients[i];
        if (client.url && new URL(client.url).origin === self.location.origin && 'focus' in client) {
          if ('navigate' in client) {
            client.navigate(targetUrl);
          }
          return client.focus();
        }
      }
      if (self.clients.openWindow) {
        return self.clients.openWindow(targetUrl);
      }
    });

  event.waitUntil(Promise.all([clickPromise, navPromise]));
});

// 4. pushsubscriptionchange: Handles both renewal and revocation
self.addEventListener('pushsubscriptionchange', function (event) {
  event.waitUntil(
    (async function () {
      try {
        const oldSub = event.oldSubscription;
        if (!oldSub) {
          throw new Error('pushsubscriptionchange fired without oldSubscription');
        }

        const oldSubJSON = oldSub.toJSON();
        const prevEndpoint = oldSub.endpoint;
        const prevAuth = oldSubJSON.keys?.auth;

        if (!prevAuth) {
          throw new Error('oldSubscription missing keys.auth required for verification');
        }

        let newSub = event.newSubscription;
        const isPermissionRevoked = Notification.permission !== 'granted';

        // When browser does not auto-populate newSubscription, manually renew if permission is still granted
        if (!newSub && !isPermissionRevoked) {
          let appServerKey = oldSub.options?.applicationServerKey;
          if (!appServerKey) {
            console.warn('[SW] oldSubscription missing applicationServerKey, fetching from server...');
            const keyResp = await fetch('/api/notifications/vapidPublicKey');
            if (keyResp.ok) {
              appServerKey = await keyResp.arrayBuffer();
            }
          }

          if (appServerKey) {
            try {
              newSub = await self.registration.pushManager.subscribe({
                userVisibleOnly: true,
                applicationServerKey: appServerKey
              });
            } catch (err) {
              if (err.name === 'NotAllowedError') {
                console.warn('[SW] Push permission denied during re-subscribe; subscription was revoked');
                newSub = null;
              } else {
                throw err;
              }
            }
          }
        }

        if (newSub) {
          const newSubJSON = newSub.toJSON();
          const resp = await fetch('/api/notifications/replace', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              prevEndpoint: prevEndpoint,
              prevAuth: prevAuth,
              subscription: {
                endpoint: newSub.endpoint,
                keys: {
                  p256dh: newSubJSON.keys?.p256dh || '',
                  auth: newSubJSON.keys?.auth || ''
                },
                userAgent: navigator.userAgent
              }
            })
          });

          if (!resp.ok) {
            throw new Error(`Server failed to replace subscription: HTTP ${resp.status}`);
          }
          console.log('[SW] Push subscription replaced successfully');
        } else {
          // Subscription revoked: proactively clean up dead endpoint on server
          const resp = await fetch('/api/notifications/replace', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              prevEndpoint: prevEndpoint,
              prevAuth: prevAuth,
              subscription: null
            })
          });

          if (!resp.ok) {
            throw new Error(`Server failed to clean up revoked subscription: HTTP ${resp.status}`);
          }
          console.log('[SW] Revoked push subscription cleaned up on server successfully');
        }
      } catch (err) {
        console.error('[SW] pushsubscriptionchange failed:', err.message, {
          hasOldSub: Boolean(event.oldSubscription),
          permission: Notification.permission
        });
      }
    })()
  );
});
