const inFlightBackgroundPreloads = new Map<string, number>();

function normalizeRoutePath(path: string): string {
    const withoutQueryOrHash = path.split(/[?#]/)[0] || '/';
    if (withoutQueryOrHash.length > 1 && withoutQueryOrHash.endsWith('/')) {
        return withoutQueryOrHash.slice(0, -1);
    }
    return withoutQueryOrHash;
}

function incrementBackgroundPreload(path: string): void {
    inFlightBackgroundPreloads.set(path, (inFlightBackgroundPreloads.get(path) ?? 0) + 1);
}

function decrementBackgroundPreload(path: string): void {
    const remaining = (inFlightBackgroundPreloads.get(path) ?? 1) - 1;
    if (remaining <= 0) {
        inFlightBackgroundPreloads.delete(path);
    } else {
        inFlightBackgroundPreloads.set(path, remaining);
    }
}

export const routeImporters = {
    '/dashboard': () => import('../pages/Dashboard'),
    '/settings': () => import('../pages/Settings'),
    '/forecast': () => import('../pages/Forecast'),
    '/history': () => import('../pages/History'),
    '/login': () => import('../pages/LoginPage'),
    '/join-site': () => import('../pages/JoinSitePage'),
    '/new-site': () => import('../pages/NewSitePage'),
    '/welcome': () => import('../pages/BetaInterstitialPage'),
    '/admin': () => import('../pages/AdminPage'),
    '/privacy': () => import('../pages/PrivacyPolicy'),
    '/terms': () => import('../pages/TermsOfService'),
    '/how-it-works': () => import('../pages/HowItWorksPage'),
    '/getting-started': () => import('../pages/GettingStartedPage'),
    '/changelog': () => import('../pages/ChangelogPage'),
    '/utilities': () => import('../pages/SupportedUtilitiesPage'),
    '/callback/tesla': () => import('../pages/TeslaCallback'),
    '/oauth/authorize': () => import('../pages/OAuthAuthorizePage'),
} as const;

export type LazyRoutePath = keyof typeof routeImporters;

export function hasOnlyBackgroundPreloadInFlight(pathname: string): boolean {
    if (inFlightBackgroundPreloads.size === 0) {
        return false;
    }
    const currentPath = normalizeRoutePath(pathname);
    return !inFlightBackgroundPreloads.has(currentPath);
}

export function resetRoutePreloadState(): void {
    inFlightBackgroundPreloads.clear();
}

export function preloadRoute(path: string): void {
    const cleanPath = normalizeRoutePath(path) as LazyRoutePath;
    const importer = routeImporters[cleanPath];
    if (!importer) {
        return;
    }
    incrementBackgroundPreload(cleanPath);
    try {
        void importer()
            .catch(() => {
                // Swallow transient preload errors on hover/focus; actual navigation handles recovery
            })
            .finally(() => {
                decrementBackgroundPreload(cleanPath);
            });
    } catch {
        decrementBackgroundPreload(cleanPath);
    }
}

export function getRoutePreloadProps(path: string) {
    const trigger = () => {
        preloadRoute(path);
    };
    return {
        onPointerEnter: trigger,
        onFocus: trigger,
    };
}
