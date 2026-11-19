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
    '/utilities': () =>
        Promise.all([
            import('../pages/SupportedUtilitiesPage'),
            import('./utilitiesPrefetch').then((p) => p.prefetchSupportedUtilities()),
        ]).then(([m]) => m),
    '/callback/tesla': () => import('../pages/TeslaCallback'),
    '/oauth/authorize': () => import('../pages/OAuthAuthorizePage'),
} as const;

export type LazyRoutePath = keyof typeof routeImporters;

export function preloadRoute(path: string): void {
    const cleanPath = path.split('?')[0] as LazyRoutePath;
    const importer = routeImporters[cleanPath];
    if (importer) {
        void importer().catch(() => {
            // Swallow transient preload errors on hover/focus; actual navigation handles recovery
        });
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
