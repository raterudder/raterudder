import { fetchUtilities, type UtilityProviderInfo } from '../api';

let prefetchedUtilities: UtilityProviderInfo[] | null = null;

export async function prefetchSupportedUtilities(): Promise<void> {
    try {
        const liveUtilities = await fetchUtilities();
        if (Array.isArray(liveUtilities)) {
            prefetchedUtilities = liveUtilities.filter((u) => !u.hidden && u.id !== 'tou_example');
        }
    } catch {
        prefetchedUtilities = null;
    }
}

export function consumePrefetchedUtilities(): UtilityProviderInfo[] | null {
    const cached = prefetchedUtilities;
    prefetchedUtilities = null;
    return cached;
}
