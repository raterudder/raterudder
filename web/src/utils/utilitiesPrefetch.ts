import { fetchUtilities, type UtilityProviderInfo } from '../api';

let prefetchedUtilities: UtilityProviderInfo[] | null = null;
let inFlightPrefetch: Promise<UtilityProviderInfo[] | null> | null = null;

export function filterSupportedUtilities(liveUtilities: UtilityProviderInfo[]): UtilityProviderInfo[] {
    return liveUtilities.filter((u) => !u.hidden && u.id !== 'tou_example');
}

export function prefetchSupportedUtilities(): Promise<UtilityProviderInfo[] | null> {
    if (prefetchedUtilities !== null) {
        return Promise.resolve(prefetchedUtilities);
    }
    if (inFlightPrefetch !== null) {
        return inFlightPrefetch;
    }

    const request: Promise<UtilityProviderInfo[] | null> = fetchUtilities()
        .then((liveUtilities) => {
            if (Array.isArray(liveUtilities)) {
                const visible = filterSupportedUtilities(liveUtilities);
                prefetchedUtilities = visible;
                return visible;
            }
            return null;
        })
        .catch(() => {
            prefetchedUtilities = null;
            return null;
        })
        .finally(() => {
            if (inFlightPrefetch === request) {
                inFlightPrefetch = null;
            }
        });

    inFlightPrefetch = request;
    return request;
}

export function getPrefetchedUtilities(): UtilityProviderInfo[] | null {
    return prefetchedUtilities;
}

export function getInFlightUtilitiesPrefetch(): Promise<UtilityProviderInfo[] | null> | null {
    return inFlightPrefetch;
}

export function clearPrefetchedUtilities(): void {
    prefetchedUtilities = null;
}

export function resetUtilitiesPrefetch(): void {
    prefetchedUtilities = null;
    inFlightPrefetch = null;
}
