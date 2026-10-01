import type {
  BrowserReportPayload,
  ExtendableEvent,
  NotificationClickData,
  NotificationErrorInfo,
  NotificationEvent,
  PushEvent,
  PushMessageData,
  PushOptions,
  PushPayload,
  PushSubscriptionChangeEvent,
  ServiceWorkerGlobalScope,
} from './types';

/**
 * Parses raw push data and resolves the notification title and options.
 * RateRudder's backend push payload is structured as { title, body, tag, icon, badge, data: { url, id, logID } }.
 */
export function parsePushPayload(data: PushMessageData | null | undefined): {
  title: string;
  options: PushOptions;
} {
  let payload: PushPayload = {};
  if (data) {
    try {
      payload = data.json();
    } catch {
      payload = { title: 'RateRudder Notification', body: data.text() };
    }
  }

  const title = payload.title || 'RateRudder Notification';
  const options: PushOptions = {
    body: payload.body || '',
    icon: payload.icon || '/logo_192.png',
    badge: payload.badge || '/badge_96.png',
    data: {
      ...payload.data,
      url: payload.data?.url || '/dashboard',
      id: payload.data?.id || payload.data?.logID || '',
      ts: Date.now(),
    },
  };

  // Only renotify when a tag is present
  if (payload.tag) {
    options.tag = payload.tag;
    options.renotify = true;
  }

  return { title, options };
}

/**
 * Reports service worker notification errors by constructing a synthetic browser report
 * and sending it to /api/report/browser.
 *
 * NOTE: /api/report/browser was designed to accept official browser reports from the W3C Reporting API
 * (e.g. 'csp-violation', 'intervention'). Since we are synthetically constructing ("faking") a browser
 * report for service worker notification failures, we use the custom type 'x-notification-error' (with 'x-' prefix)
 * to avoid colliding with any future standard report types that browser vendors may introduce to the Reporting API.
 *
 * Uses fetch with keepalive: true to ensure delivery even if the worker terminates.
 */
export async function reportNotificationError(
  errorInfo: NotificationErrorInfo,
  sw: ServiceWorkerGlobalScope = self as unknown as ServiceWorkerGlobalScope
): Promise<void> {
  // Construct a synthetic W3C BrowserReport array matching the /api/report/browser ingestion schema
  const report: BrowserReportPayload[] = [
    {
      type: 'x-notification-error',
      age: 0,
      url: sw.location?.href || '/sw.js',
      user_agent: typeof navigator !== 'undefined' ? navigator.userAgent : '',
      body: errorInfo,
    },
  ];

  try {
    await fetch('/api/report/browser', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(report),
      keepalive: true,
      credentials: 'same-origin',
    });
  } catch (reportErr) {
    console.warn('[SW] Failed to report notification error:', reportErr);
  }
}

/**
 * Handles push event by displaying the notification and retrying without icons on failure.
 * Reports errors to /api/report/browser.
 */
export async function handlePushEvent(
  event: PushEvent,
  sw: ServiceWorkerGlobalScope = self as unknown as ServiceWorkerGlobalScope
): Promise<void> {
  let title = 'RateRudder Notification';
  let options: PushOptions;

  try {
    const parsed = parsePushPayload(event.data);
    title = parsed.title;
    options = parsed.options;
  } catch (err) {
    const error = err instanceof Error ? err : new Error(String(err));
    await reportNotificationError(
      {
        message: error.message,
        name: error.name,
        stack: error.stack,
        phase: 'payload_parsing',
      },
      sw
    );
    return;
  }

  try {
    await sw.registration.showNotification(title, options);
    console.log('[SW] Notification displayed successfully:', title);
  } catch (err) {
    console.warn('[SW] Primary showNotification failed, retrying without icons:', err);
    const primaryError = err instanceof Error ? err : new Error(String(err));

    await reportNotificationError(
      {
        message: primaryError.message,
        name: primaryError.name,
        stack: primaryError.stack,
        phase: 'primary_show_notification',
        title,
        tag: options.tag,
        data: options.data as Record<string, unknown>,
      },
      sw
    );

    const fallbackOptions: NotificationOptions = {
      body: options.body,
      data: options.data,
    };
    if (options.tag) {
      fallbackOptions.tag = options.tag;
      fallbackOptions.renotify = true;
    }

    try {
      await sw.registration.showNotification(title, fallbackOptions);
      console.log('[SW] Fallback notification displayed successfully');
    } catch (fallbackErr) {
      console.error('[SW] Fallback showNotification also failed:', fallbackErr);
      const fallbackError = fallbackErr instanceof Error ? fallbackErr : new Error(String(fallbackErr));

      await reportNotificationError(
        {
          message: fallbackError.message,
          name: fallbackError.name,
          stack: fallbackError.stack,
          phase: 'fallback_show_notification',
          title,
          tag: options.tag,
          data: options.data as Record<string, unknown>,
        },
        sw
      );
    }
  }
}

/**
 * Handles notification clicks by closing the notification, logging the click,
 * and focusing or opening the relevant window.
 */
export async function handleNotificationClick(
  event: NotificationEvent,
  sw: ServiceWorkerGlobalScope = self as unknown as ServiceWorkerGlobalScope
): Promise<void> {
  event.notification.close();

  const data = (event.notification.data || {}) as NotificationClickData;
  const relativeUrl = data.url || '/dashboard';
  const targetUrl = new URL(relativeUrl, sw.location.origin).href;
  const id = data.id || data.logID || '';

  const clickPromise = id
    ? fetch(`/api/notifications/click?id=${encodeURIComponent(id)}`, {
        method: 'POST',
        credentials: 'same-origin',
      }).catch((err) => {
        console.warn('[SW] Failed to record notification click:', err);
      })
    : Promise.resolve();

  const navPromise = sw.clients
    .matchAll({ type: 'window', includeUncontrolled: true })
    .then((windowClients) => {
      for (let i = 0; i < windowClients.length; i++) {
        const client = windowClients[i];
        if (client.url && new URL(client.url).origin === sw.location.origin && 'focus' in client) {
          if ('navigate' in client) {
            client.navigate(targetUrl);
          }
          return client.focus();
        }
      }
      if (sw.clients.openWindow) {
        return sw.clients.openWindow(targetUrl);
      }
    });

  await Promise.all([clickPromise, navPromise]);
}

/**
 * Handles push subscription change events for both renewal and revocation.
 */
export async function handlePushSubscriptionChange(
  event: PushSubscriptionChangeEvent,
  sw: ServiceWorkerGlobalScope = self as unknown as ServiceWorkerGlobalScope
): Promise<void> {
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
    const isPermissionRevoked = typeof Notification !== 'undefined' && Notification.permission !== 'granted';

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
          newSub = await sw.registration.pushManager.subscribe({
            userVisibleOnly: true,
            applicationServerKey: appServerKey,
          });
        } catch (err: unknown) {
          if ((err as { name?: string })?.name === 'NotAllowedError') {
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
              auth: newSubJSON.keys?.auth || '',
            },
            userAgent: typeof navigator !== 'undefined' ? navigator.userAgent : '',
          },
        }),
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
          subscription: null,
        }),
      });

      if (!resp.ok) {
        throw new Error(`Server failed to clean up revoked subscription: HTTP ${resp.status}`);
      }
      console.log('[SW] Revoked push subscription cleaned up on server successfully');
    }
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    console.error('[SW] pushsubscriptionchange failed:', message, {
      hasOldSub: Boolean(event.oldSubscription),
      permission: typeof Notification !== 'undefined' ? Notification.permission : undefined,
    });
  }
}

/**
 * Handles the install lifecycle event.
 */
export function handleInstall(
  _event: ExtendableEvent,
  sw: ServiceWorkerGlobalScope = self as unknown as ServiceWorkerGlobalScope
): void {
  sw.skipWaiting();
}

/**
 * Handles the activate lifecycle event.
 */
export function handleActivate(
  event: ExtendableEvent,
  sw: ServiceWorkerGlobalScope = self as unknown as ServiceWorkerGlobalScope
): void {
  event.waitUntil(sw.clients.claim());
}

/**
 * Registers all service worker lifecycle and push event listeners.
 */
export function registerServiceWorker(
  sw: ServiceWorkerGlobalScope = self as unknown as ServiceWorkerGlobalScope
): void {
  sw.addEventListener('install', (event: ExtendableEvent) => handleInstall(event, sw));
  sw.addEventListener('activate', (event: ExtendableEvent) => handleActivate(event, sw));
  sw.addEventListener('push', (event: PushEvent) => {
    event.waitUntil(handlePushEvent(event, sw));
  });
  sw.addEventListener('notificationclick', (event: NotificationEvent) => {
    event.waitUntil(handleNotificationClick(event, sw));
  });
  sw.addEventListener('pushsubscriptionchange', (event: PushSubscriptionChangeEvent) => {
    event.waitUntil(handlePushSubscriptionChange(event, sw));
  });
}
