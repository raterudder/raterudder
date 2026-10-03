import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
    isIOSDevice,
    hasNotificationSupport,
    isIOSHomeScreen,
    hasServiceWorkerSupport,
    hasPushManagerSupport,
    isPushSupportedInBrowser,
    isStandalone,
    isAndroidDevice,
    isAndroidBrowserTab,
    getDeferredInstallPrompt,
    setDeferredInstallPrompt,
    promptInstallApp,
    subscribeInstallPrompt,
} from './pwaUtils';

describe('pwaUtils', () => {
    beforeEach(() => {
        vi.restoreAllMocks();
    });

    describe('isIOSDevice', () => {
        it('detects iPhone, iPad, and iPod from userAgent', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            expect(isIOSDevice()).toBe(true);

            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPad; CPU OS 16_5 like Mac OS X)');
            expect(isIOSDevice()).toBe(true);

            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPod touch; CPU iPhone OS 14_0 like Mac OS X)');
            expect(isIOSDevice()).toBe(true);
        });

        it('detects iPadOS with MacIntel and touch points > 1', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)');
            vi.spyOn(navigator, 'platform', 'get').mockReturnValue('MacIntel');
            Object.defineProperty(navigator, 'maxTouchPoints', { value: 5, configurable: true });
            expect(isIOSDevice()).toBe(true);
        });

        it('returns false for macOS desktop without touch points', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)');
            vi.spyOn(navigator, 'platform', 'get').mockReturnValue('MacIntel');
            Object.defineProperty(navigator, 'maxTouchPoints', { value: 0, configurable: true });
            expect(isIOSDevice()).toBe(false);
        });

        it('returns false for Windows and Android', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            expect(isIOSDevice()).toBe(false);

            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Linux; Android 14)');
            expect(isIOSDevice()).toBe(false);
        });
    });

    describe('hasNotificationSupport', () => {
        it('returns true when window.Notification is defined', () => {
            (window as any).Notification = { permission: 'default' };
            expect(hasNotificationSupport()).toBe(true);
        });

        it('returns false when window.Notification is undefined', () => {
            delete (window as any).Notification;
            expect(hasNotificationSupport()).toBe(false);
        });
    });

    describe('isIOSHomeScreen', () => {
        it('returns true when on iOS and Notification is supported', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            (window as any).Notification = { permission: 'default' };
            expect(isIOSHomeScreen()).toBe(true);
        });

        it('returns false when on iOS and Notification is not supported', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            delete (window as any).Notification;
            expect(isIOSHomeScreen()).toBe(false);
        });

        it('returns false when Notification is supported but device is not iOS', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            (window as any).Notification = { permission: 'default' };
            expect(isIOSHomeScreen()).toBe(false);
        });
    });

    describe('hasServiceWorkerSupport', () => {
        it('returns true when serviceWorker is in navigator', () => {
            Object.defineProperty(navigator, 'serviceWorker', { value: {}, configurable: true });
            expect(hasServiceWorkerSupport()).toBe(true);
        });

        it('returns false when serviceWorker is not in navigator', () => {
            const originalSW = Object.getOwnPropertyDescriptor(navigator, 'serviceWorker');
            delete (navigator as any).serviceWorker;
            expect(hasServiceWorkerSupport()).toBe(false);
            if (originalSW) {
                Object.defineProperty(navigator, 'serviceWorker', originalSW);
            }
        });
    });

    describe('hasPushManagerSupport', () => {
        it('returns true when PushManager is in window', () => {
            (window as any).PushManager = {};
            expect(hasPushManagerSupport()).toBe(true);
        });

        it('returns false when PushManager is not in window', () => {
            delete (window as any).PushManager;
            expect(hasPushManagerSupport()).toBe(false);
        });
    });

    describe('isPushSupportedInBrowser', () => {
        beforeEach(() => {
            Object.defineProperty(navigator, 'serviceWorker', { value: {}, configurable: true });
            (window as any).PushManager = {};
            (window as any).Notification = { permission: 'default' };
        });

        it('returns true on non-iOS desktop when serviceWorker, PushManager, and Notification exist', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            expect(isPushSupportedInBrowser()).toBe(true);
        });

        it('returns false on non-iOS when Notification is undefined', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            delete (window as any).Notification;
            expect(isPushSupportedInBrowser()).toBe(false);
        });

        it('returns false when serviceWorker is missing', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            delete (navigator as any).serviceWorker;
            expect(isPushSupportedInBrowser()).toBe(false);
        });

        it('returns false when PushManager is missing', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            delete (window as any).PushManager;
            expect(isPushSupportedInBrowser()).toBe(false);
        });

        it('returns true on iOS even when Notification and PushManager are undefined (standard iOS Safari tab)', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            delete (window as any).Notification;
            delete (window as any).PushManager;
            expect(isPushSupportedInBrowser()).toBe(true);
        });

        it('returns false on iOS when serviceWorker is missing', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            delete (navigator as any).serviceWorker;
            expect(isPushSupportedInBrowser()).toBe(false);
        });
    });

    describe('isStandalone', () => {
        it('returns true when display-mode matches standalone', () => {
            window.matchMedia = vi.fn().mockImplementation((query) => ({
                matches: query === '(display-mode: standalone)',
                media: query,
                onchange: null,
                addListener: vi.fn(),
                removeListener: vi.fn(),
                addEventListener: vi.fn(),
                removeEventListener: vi.fn(),
                dispatchEvent: vi.fn(),
            }));
            expect(isStandalone()).toBe(true);
        });

        it('returns true when navigator.standalone is true', () => {
            window.matchMedia = vi.fn().mockReturnValue({ matches: false });
            (window.navigator as any).standalone = true;
            expect(isStandalone()).toBe(true);
            delete (window.navigator as any).standalone;
        });

        it('returns false when not standalone', () => {
            window.matchMedia = vi.fn().mockReturnValue({ matches: false });
            expect(isStandalone()).toBe(false);
        });
    });

    describe('isAndroidDevice', () => {
        it('returns true for Android userAgent', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Linux; Android 14; Pixel 8)');
            expect(isAndroidDevice()).toBe(true);
        });

        it('returns false for iOS and desktop', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            expect(isAndroidDevice()).toBe(false);

            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64)');
            expect(isAndroidDevice()).toBe(false);
        });
    });

    describe('isAndroidBrowserTab', () => {
        it('returns true when Android device and not standalone', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Linux; Android 14; Pixel 8)');
            window.matchMedia = vi.fn().mockReturnValue({ matches: false });
            expect(isAndroidBrowserTab()).toBe(true);
        });

        it('returns false when Android device in standalone mode', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Linux; Android 14; Pixel 8)');
            window.matchMedia = vi.fn().mockImplementation((query) => ({
                matches: query === '(display-mode: standalone)',
            }));
            expect(isAndroidBrowserTab()).toBe(false);
        });

        it('returns false when not an Android device', () => {
            vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)');
            window.matchMedia = vi.fn().mockReturnValue({ matches: false });
            expect(isAndroidBrowserTab()).toBe(false);
        });
    });

    describe('manifest.json', () => {
        it('contains valid PWA shortcuts for Dashboard and Forecast', async () => {
            const manifest = (await import('../../public/manifest.json')).default;

            expect(manifest.name).toBe('RateRudder');
            expect(manifest.display).toBe('standalone');
            expect(Array.isArray(manifest.shortcuts)).toBe(true);

            const shortcuts = manifest.shortcuts;
            expect(shortcuts).toHaveLength(2);

            const dashboard = shortcuts.find((s: any) => s.name === 'Dashboard');
            expect(dashboard).toBeDefined();
            expect(dashboard?.url).toBe('/dashboard');
            expect((dashboard as any)?.icons).toBeUndefined();

            const forecast = shortcuts.find((s: any) => s.name === 'Forecast');
            expect(forecast).toBeDefined();
            expect(forecast?.url).toBe('/forecast');
            expect((forecast as any)?.icons).toBeUndefined();

            // Also check that maskable icons are specified
            const maskable = manifest.icons.filter((icon: any) => icon.purpose === 'maskable');
            expect(maskable.length).toBeGreaterThan(0);
        });
    });

    describe('installPrompt helpers', () => {
        it('tracks deferred install prompt and triggers promptInstallApp', async () => {
            const promptMock = vi.fn().mockResolvedValue(undefined);
            const fakeEvent = {
                preventDefault: vi.fn(),
                prompt: promptMock,
                userChoice: Promise.resolve({ outcome: 'accepted', platform: 'android' }),
            } as any;

            let notified: any = null;
            const unsubscribe = subscribeInstallPrompt((p) => {
                notified = p;
            });

            setDeferredInstallPrompt(fakeEvent);
            expect(getDeferredInstallPrompt()).toBe(fakeEvent);
            expect(notified).toBe(fakeEvent);

            const accepted = await promptInstallApp();
            expect(accepted).toBe(true);
            expect(promptMock).toHaveBeenCalled();
            expect(getDeferredInstallPrompt()).toBeNull();

            unsubscribe();
        });

        it('returns false when prompt is dismissed or not set', async () => {
            setDeferredInstallPrompt(null);
            expect(await promptInstallApp()).toBe(false);

            const promptMock = vi.fn().mockResolvedValue(undefined);
            const dismissEvent = {
                prompt: promptMock,
                userChoice: Promise.resolve({ outcome: 'dismissed', platform: 'android' }),
            } as any;

            setDeferredInstallPrompt(dismissEvent);
            const accepted = await promptInstallApp();
            expect(accepted).toBe(false);
            setDeferredInstallPrompt(null);
        });
    });
});


