export const isIOSDevice = (): boolean => {
    if (typeof navigator === 'undefined') return false;
    return (
        /iPad|iPhone|iPod/.test(navigator.userAgent) ||
        (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
    );
};

export const hasNotificationSupport = (): boolean => {
    return typeof window !== 'undefined' && 'Notification' in window && typeof window.Notification !== 'undefined';
};

export const isIOSHomeScreen = (): boolean => {
    return isIOSDevice() && hasNotificationSupport();
};

export const hasServiceWorkerSupport = (): boolean => {
    return typeof navigator !== 'undefined' && 'serviceWorker' in navigator;
};

export const hasPushManagerSupport = (): boolean => {
    return typeof window !== 'undefined' && 'PushManager' in window;
};

export const isPushSupportedInBrowser = (): boolean => {
    if (!hasServiceWorkerSupport()) {
        return false;
    }
    if (isIOSDevice()) {
        // On iOS Safari browser tabs, both Notification and PushManager are undefined
        // until the user adds the web app to the Home Screen. As long as serviceWorker
        // exists, the iOS device supports Web Push via Home Screen installation.
        return true;
    }
    return hasPushManagerSupport() && hasNotificationSupport();
};

export const isStandalone = (): boolean => {
    if (typeof window === 'undefined') return false;
    const isStandaloneDisplay =
        (typeof window.matchMedia === 'function' &&
            (window.matchMedia('(display-mode: standalone)').matches ||
                window.matchMedia('(display-mode: fullscreen)').matches)) ||
        false;
    const isNavigatorStandalone = (window.navigator as unknown as { standalone?: boolean })?.standalone === true;
    return isStandaloneDisplay || isNavigatorStandalone;
};

export const isAndroidDevice = (): boolean => {
    if (typeof navigator === 'undefined') return false;
    return /Android/i.test(navigator.userAgent);
};

export const isAndroidBrowserTab = (): boolean => {
    return isAndroidDevice() && !isStandalone();
};

export interface BeforeInstallPromptEvent extends Event {
    prompt: () => Promise<void>;
    userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
}

let deferredInstallPrompt: BeforeInstallPromptEvent | null = null;
const installListeners = new Set<(prompt: BeforeInstallPromptEvent | null) => void>();

if (typeof window !== 'undefined') {
    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault();
        deferredInstallPrompt = e as BeforeInstallPromptEvent;
        installListeners.forEach((fn) => fn(deferredInstallPrompt));
    });

    window.addEventListener('appinstalled', () => {
        deferredInstallPrompt = null;
        installListeners.forEach((fn) => fn(null));
    });
}

export const getDeferredInstallPrompt = (): BeforeInstallPromptEvent | null => deferredInstallPrompt;

export const setDeferredInstallPrompt = (prompt: BeforeInstallPromptEvent | null): void => {
    deferredInstallPrompt = prompt;
    installListeners.forEach((fn) => fn(deferredInstallPrompt));
};

export const promptInstallApp = async (): Promise<boolean> => {
    const prompt = deferredInstallPrompt;
    if (!prompt) return false;
    await prompt.prompt();
    const choice = await prompt.userChoice;
    if (choice.outcome === 'accepted') {
        if (deferredInstallPrompt === prompt) {
            deferredInstallPrompt = null;
            installListeners.forEach((fn) => fn(null));
        }
        return true;
    }
    return false;
};

export const subscribeInstallPrompt = (callback: (prompt: BeforeInstallPromptEvent | null) => void): (() => void) => {
    installListeners.add(callback);
    callback(deferredInstallPrompt);
    return () => {
        installListeners.delete(callback);
    };
};


