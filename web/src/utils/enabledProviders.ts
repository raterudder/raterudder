export function getEnabledProviders(): string[] {
    try {
        const raw = localStorage.getItem('enabled');
        if (!raw) return [];
        return raw.split(',').map((s) => s.trim().toLowerCase()).filter(Boolean);
    } catch {
        return [];
    }
}

export function isESSEnabled(id: string): boolean {
    if (!id) return false;
    const enabledList = getEnabledProviders();
    return enabledList.includes(id.trim().toLowerCase());
}

export function checkAndStoreEnabledParam(searchString?: string): void {
    try {
        const search = searchString !== undefined ? searchString : (typeof window !== 'undefined' ? window.location.search : '');
        if (!search) return;
        const params = new URLSearchParams(search);
        const enabledParam = params.get('enabled');
        if (enabledParam) {
            const existing = getEnabledProviders();
            const newItems = enabledParam.split(',').map(s => s.trim().toLowerCase()).filter(Boolean);
            const combined = Array.from(new Set([...existing, ...newItems]));
            localStorage.setItem('enabled', combined.join(','));
        }
    } catch (err) {
        console.error('Failed to process ?enabled query parameter:', err);
    }
}
