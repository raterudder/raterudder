import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  parsePushPayload,
  reportNotificationError,
  handlePushEvent,
  handleNotificationClick,
  handlePushSubscriptionChange,
  handleInstall,
  handleActivate,
  registerServiceWorker,
} from './handlers';
import type {
  PushEvent,
  PushMessageData,
  NotificationEvent,
  PushSubscriptionChangeEvent,
  ServiceWorkerGlobalScope,
  WindowClient,
} from './types';

describe('Service Worker', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(console, 'log').mockImplementation(() => {});
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  describe('parsePushPayload', () => {
    it('correctly parses RateRudder backend push payload without clobbering top-level attributes with inner data', () => {
      const rawPayload = {
        title: '🚨 Price Spike: $0.45/kWh',
        body: 'ComEd hourly rate has spiked. Battery is discharging.',
        tag: 'price-spike-site1',
        icon: '/custom_logo.png',
        badge: '/custom_badge.png',
        data: {
          url: '/forecast?site=site1',
          id: 'log-uuid-1234',
          logID: 'log-uuid-1234',
        },
      };

      const mockData: PushMessageData = {
        json: () => rawPayload,
        text: () => JSON.stringify(rawPayload),
        arrayBuffer: () => new ArrayBuffer(0),
        blob: () => new Blob(),
      };

      const { title, options } = parsePushPayload(mockData);

      // Verifies resolution of bug where payload = parsed.data || parsed caused title/body/tag loss
      expect(title).toBe('🚨 Price Spike: $0.45/kWh');
      expect(options.body).toBe('ComEd hourly rate has spiked. Battery is discharging.');
      expect(options.tag).toBe('price-spike-site1');
      expect(options.renotify).toBe(true);
      expect(options.icon).toBe('/custom_logo.png');
      expect(options.badge).toBe('/custom_badge.png');
      expect(options.data.url).toBe('/forecast?site=site1');
      expect(options.data.id).toBe('log-uuid-1234');
      expect(options.data.ts).toBeTypeOf('number');
    });

    it('preserves custom metadata fields passed in payload data', () => {
      const payloadWithMeta = {
        title: '☀️ Solar Alert',
        body: 'Production low',
        data: {
          url: '/forecast',
          id: 'log-101',
          metadata: {
            solarRatio: '0.45',
            reason: 'cloud_cover',
          },
        },
      };

      const mockData: PushMessageData = {
        json: () => payloadWithMeta,
        text: () => JSON.stringify(payloadWithMeta),
        arrayBuffer: () => new ArrayBuffer(0),
        blob: () => new Blob(),
      };

      const { title, options } = parsePushPayload(mockData);

      expect(title).toBe('☀️ Solar Alert');
      expect(options.body).toBe('Production low');
      expect(options.data.url).toBe('/forecast');
      expect(options.data.id).toBe('log-101');
      expect(options.data.metadata).toEqual({
        solarRatio: '0.45',
        reason: 'cloud_cover',
      });
      expect(options.tag).toBeUndefined();
      expect(options.renotify).toBeUndefined();
    });

    it('handles non-JSON plain text push messages gracefully', () => {
      const mockData: PushMessageData = {
        json: () => {
          throw new SyntaxError('Unexpected token in JSON');
        },
        text: () => 'System maintenance scheduled tonight at 11 PM',
        arrayBuffer: () => new ArrayBuffer(0),
        blob: () => new Blob(),
      };

      const { title, options } = parsePushPayload(mockData);

      expect(title).toBe('RateRudder Notification');
      expect(options.body).toBe('System maintenance scheduled tonight at 11 PM');
      expect(options.icon).toBe('/logo_192.png');
      expect(options.badge).toBe('/badge_96.png');
      expect(options.data.url).toBe('/dashboard');
      expect(options.data.id).toBe('');
      expect(options.tag).toBeUndefined();
      expect(options.renotify).toBeUndefined();
    });

    it('handles null or undefined push event data with safe defaults', () => {
      const { title, options } = parsePushPayload(null);

      expect(title).toBe('RateRudder Notification');
      expect(options.body).toBe('');
      expect(options.icon).toBe('/logo_192.png');
      expect(options.badge).toBe('/badge_96.png');
      expect(options.data.url).toBe('/dashboard');
      expect(options.data.id).toBe('');
      expect(options.tag).toBeUndefined();
      expect(options.renotify).toBeUndefined();
    });

    it('does not set tag or renotify when tag is missing in payload', () => {
      const rawPayload = {
        title: '☀️ Morning Outlook',
        body: 'Clear skies expected today.',
      };

      const mockData: PushMessageData = {
        json: () => rawPayload,
        text: () => JSON.stringify(rawPayload),
        arrayBuffer: () => new ArrayBuffer(0),
        blob: () => new Blob(),
      };

      const { options } = parsePushPayload(mockData);

      expect(options.tag).toBeUndefined();
      expect(options.renotify).toBeUndefined();
    });
  });

  describe('reportNotificationError', () => {
    it('sends synthetic x-notification-error report via fetch with keepalive: true', async () => {
      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal('fetch', mockFetch);

      const mockSw = {
        location: { href: 'https://example.com/sw.js' },
      } as unknown as ServiceWorkerGlobalScope;

      await reportNotificationError(
        {
          message: 'Fetch keepalive test error',
          name: 'TypeError',
          stack: 'Error stack...',
          phase: 'primary_show_notification',
          title: 'Test Alert',
          tag: 'test-tag',
        },
        mockSw
      );

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/report/browser',
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          keepalive: true,
          credentials: 'same-origin',
          body: expect.stringContaining('x-notification-error'),
        })
      );
    });

    it('handles network error in fetch gracefully without throwing', async () => {
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network offline')));

      await expect(
        reportNotificationError({
          message: 'Offline test',
          phase: 'fallback_show_notification',
        })
      ).resolves.not.toThrow();
    });
  });

  describe('handlePushEvent', () => {
    it('displays the notification on the service worker registration', async () => {
      const showNotificationMock = vi.fn().mockResolvedValue(undefined);
      const mockSw = {
        registration: {
          showNotification: showNotificationMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const rawPayload = {
        title: '⚠️ High Home Load',
        body: 'Home usage exceeded 8 kW.',
        tag: 'high-load',
      };

      const event = {
        data: {
          json: () => rawPayload,
          text: () => JSON.stringify(rawPayload),
          arrayBuffer: () => new ArrayBuffer(0),
          blob: () => new Blob(),
        },
        waitUntil: vi.fn(),
      } as unknown as PushEvent;

      await handlePushEvent(event, mockSw);

      expect(showNotificationMock).toHaveBeenCalledTimes(1);
      expect(showNotificationMock).toHaveBeenCalledWith(
        '⚠️ High Home Load',
        expect.objectContaining({
          body: 'Home usage exceeded 8 kW.',
          tag: 'high-load',
          renotify: true,
          icon: '/logo_192.png',
        })
      );
    });

    it('retries without icons and reports error to /api/report/browser if primary showNotification rejects', async () => {
      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal('fetch', mockFetch);

      const showNotificationMock = vi
        .fn()
        .mockRejectedValueOnce(new Error('Failed to load notification icon'))
        .mockResolvedValueOnce(undefined);

      const mockSw = {
        location: { href: 'https://example.com/sw.js' },
        registration: {
          showNotification: showNotificationMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const rawPayload = {
        title: '⚡ VPP Event',
        body: 'Virtual power plant event starting now.',
        tag: 'vpp-event',
        data: { url: '/history', id: 'log-99' },
      };

      const event = {
        data: {
          json: () => rawPayload,
          text: () => JSON.stringify(rawPayload),
          arrayBuffer: () => new ArrayBuffer(0),
          blob: () => new Blob(),
        },
        waitUntil: vi.fn(),
      } as unknown as PushEvent;

      await handlePushEvent(event, mockSw);

      expect(showNotificationMock).toHaveBeenCalledTimes(2);

      // First call includes icons
      expect(showNotificationMock.mock.calls[0][1]).toHaveProperty('icon');
      expect(showNotificationMock.mock.calls[0][1]).toHaveProperty('badge');

      // Second fallback call strips icons but preserves tag and data
      const fallbackOptions = showNotificationMock.mock.calls[1][1];
      expect(fallbackOptions).not.toHaveProperty('icon');
      expect(fallbackOptions).not.toHaveProperty('badge');
      expect(fallbackOptions.body).toBe('Virtual power plant event starting now.');
      expect(fallbackOptions.tag).toBe('vpp-event');
      expect(fallbackOptions.renotify).toBe(true);
      expect(fallbackOptions.data.url).toBe('/history');

      // Verify report was sent for primary failure
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/report/browser',
        expect.objectContaining({
          method: 'POST',
          keepalive: true,
          body: expect.stringContaining('primary_show_notification'),
        })
      );
    });

    it('reports fallback failure when both primary and fallback showNotification fail', async () => {
      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal('fetch', mockFetch);

      const showNotificationMock = vi
        .fn()
        .mockRejectedValue(new Error('Notification permission denied or system error'));

      const mockSw = {
        location: { href: 'https://example.com/sw.js' },
        registration: {
          showNotification: showNotificationMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const event = {
        data: null,
        waitUntil: vi.fn(),
      } as unknown as PushEvent;

      await expect(handlePushEvent(event, mockSw)).resolves.not.toThrow();
      expect(showNotificationMock).toHaveBeenCalledTimes(2);

      // Both primary and fallback failures should be reported
      expect(mockFetch).toHaveBeenCalledTimes(2);
      expect(mockFetch.mock.calls[0][1].body).toContain('primary_show_notification');
      expect(mockFetch.mock.calls[1][1].body).toContain('fallback_show_notification');
    });
  });

  describe('handleNotificationClick', () => {
    it('closes notification, calls click tracking API, and focuses matching client window', async () => {
      const closeMock = vi.fn();
      const focusMock = vi.fn().mockResolvedValue(undefined);
      const navigateMock = vi.fn().mockResolvedValue(undefined);

      const matchingClient: WindowClient = {
        id: 'client-1',
        url: 'https://example.com/some/page',
        focused: false,
        focus: focusMock,
        navigate: navigateMock,
      };

      const matchAllMock = vi.fn().mockResolvedValue([matchingClient]);
      const openWindowMock = vi.fn();

      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
      vi.stubGlobal('fetch', mockFetch);

      const mockSw = {
        location: { origin: 'https://example.com' },
        clients: {
          matchAll: matchAllMock,
          openWindow: openWindowMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const event = {
        notification: {
          close: closeMock,
          data: {
            url: '/forecast?site=site1',
            id: 'log-click-123',
          },
        },
        waitUntil: vi.fn(),
      } as unknown as NotificationEvent;

      await handleNotificationClick(event, mockSw);

      expect(closeMock).toHaveBeenCalledTimes(1);
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/notifications/click?id=log-click-123',
        expect.objectContaining({
          method: 'POST',
          credentials: 'same-origin',
        })
      );
      expect(navigateMock).toHaveBeenCalledWith('https://example.com/forecast?site=site1');
      expect(focusMock).toHaveBeenCalledTimes(1);
      expect(openWindowMock).not.toHaveBeenCalled();
    });

    it('opens a new window when no existing client window matches origin', async () => {
      const closeMock = vi.fn();
      const openWindowMock = vi.fn().mockResolvedValue(undefined);
      const matchAllMock = vi.fn().mockResolvedValue([]);

      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 200 })));

      const mockSw = {
        location: { origin: 'https://example.com' },
        clients: {
          matchAll: matchAllMock,
          openWindow: openWindowMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const event = {
        notification: {
          close: closeMock,
          data: {
            url: '/dashboard',
          },
        },
        waitUntil: vi.fn(),
      } as unknown as NotificationEvent;

      await handleNotificationClick(event, mockSw);

      expect(closeMock).toHaveBeenCalledTimes(1);
      expect(openWindowMock).toHaveBeenCalledWith('https://example.com/dashboard');
    });

    it('does not make a tracking API call if notification ID is missing', async () => {
      const closeMock = vi.fn();
      const openWindowMock = vi.fn().mockResolvedValue(undefined);
      const mockFetch = vi.fn();
      vi.stubGlobal('fetch', mockFetch);

      const mockSw = {
        location: { origin: 'https://example.com' },
        clients: {
          matchAll: vi.fn().mockResolvedValue([]),
          openWindow: openWindowMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const event = {
        notification: {
          close: closeMock,
          data: {},
        },
        waitUntil: vi.fn(),
      } as unknown as NotificationEvent;

      await handleNotificationClick(event, mockSw);

      expect(mockFetch).not.toHaveBeenCalled();
      expect(openWindowMock).toHaveBeenCalledWith('https://example.com/dashboard');
    });

    it('handles click tracking API network error without blocking navigation', async () => {
      const openWindowMock = vi.fn().mockResolvedValue(undefined);
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')));

      const mockSw = {
        location: { origin: 'https://example.com' },
        clients: {
          matchAll: vi.fn().mockResolvedValue([]),
          openWindow: openWindowMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const event = {
        notification: {
          close: vi.fn(),
          data: { id: 'err-test' },
        },
        waitUntil: vi.fn(),
      } as unknown as NotificationEvent;

      await expect(handleNotificationClick(event, mockSw)).resolves.not.toThrow();
      expect(openWindowMock).toHaveBeenCalledWith('https://example.com/dashboard');
    });
  });

  describe('handlePushSubscriptionChange', () => {
    it('replaces subscription on server when browser provides newSubscription', async () => {
      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
      vi.stubGlobal('fetch', mockFetch);

      const oldSub = {
        endpoint: 'https://push.example.com/old-sub',
        toJSON: () => ({
          keys: {
            auth: 'old-auth-key',
            p256dh: 'old-p256dh-key',
          },
        }),
      } as unknown as PushSubscription;

      const newSub = {
        endpoint: 'https://push.example.com/new-sub',
        toJSON: () => ({
          keys: {
            auth: 'new-auth-key',
            p256dh: 'new-p256dh-key',
          },
        }),
      } as unknown as PushSubscription;

      const event = {
        oldSubscription: oldSub,
        newSubscription: newSub,
        waitUntil: vi.fn(),
      } as unknown as PushSubscriptionChangeEvent;

      const mockSw = {} as ServiceWorkerGlobalScope;

      await handlePushSubscriptionChange(event, mockSw);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/notifications/replace',
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            prevEndpoint: 'https://push.example.com/old-sub',
            prevAuth: 'old-auth-key',
            subscription: {
              endpoint: 'https://push.example.com/new-sub',
              keys: {
                p256dh: 'new-p256dh-key',
                auth: 'new-auth-key',
              },
              userAgent: navigator.userAgent,
            },
          }),
        })
      );
    });

    it('cleans up dead endpoint on server when push permission was revoked', async () => {
      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
      vi.stubGlobal('fetch', mockFetch);

      // Simulate permission denied / revoked
      const originalNotification = globalThis.Notification;
      globalThis.Notification = {
        permission: 'denied',
      } as unknown as typeof Notification;

      const oldSub = {
        endpoint: 'https://push.example.com/revoked-sub',
        toJSON: () => ({
          keys: {
            auth: 'revoked-auth-key',
            p256dh: 'revoked-p256dh-key',
          },
        }),
      } as unknown as PushSubscription;

      const event = {
        oldSubscription: oldSub,
        newSubscription: null,
        waitUntil: vi.fn(),
      } as unknown as PushSubscriptionChangeEvent;

      const mockSw = {} as ServiceWorkerGlobalScope;

      await handlePushSubscriptionChange(event, mockSw);

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/notifications/replace',
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            prevEndpoint: 'https://push.example.com/revoked-sub',
            prevAuth: 'revoked-auth-key',
            subscription: null,
          }),
        })
      );

      globalThis.Notification = originalNotification;
    });

    it('attempts renewal via pushManager.subscribe when permission is still granted but newSubscription is null', async () => {
      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
      vi.stubGlobal('fetch', mockFetch);

      const renewedSub = {
        endpoint: 'https://push.example.com/auto-renewed-sub',
        toJSON: () => ({
          keys: {
            auth: 'renewed-auth-key',
            p256dh: 'renewed-p256dh-key',
          },
        }),
      } as unknown as PushSubscription;

      const subscribeMock = vi.fn().mockResolvedValue(renewedSub);

      const originalNotification = globalThis.Notification;
      globalThis.Notification = {
        permission: 'granted',
      } as unknown as typeof Notification;

      const mockAppServerKey = new Uint8Array([1, 2, 3, 4]).buffer;

      const oldSub = {
        endpoint: 'https://push.example.com/old-sub',
        options: {
          applicationServerKey: mockAppServerKey,
        },
        toJSON: () => ({
          keys: {
            auth: 'old-auth-key',
            p256dh: 'old-p256dh-key',
          },
        }),
      } as unknown as PushSubscription;

      const event = {
        oldSubscription: oldSub,
        newSubscription: null,
        waitUntil: vi.fn(),
      } as unknown as PushSubscriptionChangeEvent;

      const mockSw = {
        registration: {
          pushManager: {
            subscribe: subscribeMock,
          },
        },
      } as unknown as ServiceWorkerGlobalScope;

      await handlePushSubscriptionChange(event, mockSw);

      expect(subscribeMock).toHaveBeenCalledWith({
        userVisibleOnly: true,
        applicationServerKey: mockAppServerKey,
      });

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/notifications/replace',
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('https://push.example.com/auto-renewed-sub'),
        })
      );

      globalThis.Notification = originalNotification;
    });

    it('cleans up revoked endpoint if subscribe fails with NotAllowedError', async () => {
      const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
      vi.stubGlobal('fetch', mockFetch);

      const notAllowedErr = new Error('Permission denied');
      notAllowedErr.name = 'NotAllowedError';
      const subscribeMock = vi.fn().mockRejectedValue(notAllowedErr);

      const originalNotification = globalThis.Notification;
      globalThis.Notification = {
        permission: 'granted',
      } as unknown as typeof Notification;

      const oldSub = {
        endpoint: 'https://push.example.com/old-sub',
        options: {
          applicationServerKey: new Uint8Array([1, 2, 3]).buffer,
        },
        toJSON: () => ({
          keys: {
            auth: 'old-auth-key',
            p256dh: 'old-p256dh-key',
          },
        }),
      } as unknown as PushSubscription;

      const event = {
        oldSubscription: oldSub,
        newSubscription: null,
        waitUntil: vi.fn(),
      } as unknown as PushSubscriptionChangeEvent;

      const mockSw = {
        registration: {
          pushManager: {
            subscribe: subscribeMock,
          },
        },
      } as unknown as ServiceWorkerGlobalScope;

      await handlePushSubscriptionChange(event, mockSw);

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/notifications/replace',
        expect.objectContaining({
          body: JSON.stringify({
            prevEndpoint: 'https://push.example.com/old-sub',
            prevAuth: 'old-auth-key',
            subscription: null,
          }),
        })
      );

      globalThis.Notification = originalNotification;
    });
  });

  describe('Lifecycle & Registration', () => {
    it('handleInstall calls skipWaiting', () => {
      const skipWaitingMock = vi.fn();
      const mockSw = {
        skipWaiting: skipWaitingMock,
      } as unknown as ServiceWorkerGlobalScope;

      handleInstall({} as any, mockSw);

      expect(skipWaitingMock).toHaveBeenCalledTimes(1);
    });

    it('handleActivate claims clients and waits for completion', () => {
      const claimMock = vi.fn().mockResolvedValue(undefined);
      const waitUntilMock = vi.fn();

      const mockSw = {
        clients: {
          claim: claimMock,
        },
      } as unknown as ServiceWorkerGlobalScope;

      const event = {
        waitUntil: waitUntilMock,
      } as any;

      handleActivate(event, mockSw);

      expect(waitUntilMock).toHaveBeenCalledTimes(1);
      expect(claimMock).toHaveBeenCalledTimes(1);
    });

    it('registerServiceWorker attaches all expected event listeners', () => {
      const listeners: Record<string, (event: unknown) => void> = {};
      const addEventListenerMock = vi.fn((type: string, listener: (event: unknown) => void) => {
        listeners[type] = listener;
      });

      const mockSw = {
        addEventListener: addEventListenerMock,
        registration: {
          showNotification: vi.fn(),
        },
        clients: {
          claim: vi.fn(),
          matchAll: vi.fn(),
        },
        skipWaiting: vi.fn(),
      } as unknown as ServiceWorkerGlobalScope;

      registerServiceWorker(mockSw);

      expect(addEventListenerMock).toHaveBeenCalledWith('install', expect.any(Function));
      expect(addEventListenerMock).toHaveBeenCalledWith('activate', expect.any(Function));
      expect(addEventListenerMock).toHaveBeenCalledWith('push', expect.any(Function));
      expect(addEventListenerMock).toHaveBeenCalledWith('notificationclick', expect.any(Function));
      expect(addEventListenerMock).toHaveBeenCalledWith('pushsubscriptionchange', expect.any(Function));
      expect(Object.keys(listeners)).toEqual([
        'install',
        'activate',
        'push',
        'notificationclick',
        'pushsubscriptionchange',
      ]);
    });
  });
});
