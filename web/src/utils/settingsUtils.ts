export function areSettingsEqual(a: any, b: any): boolean {
    if (a === b) return true;

    // Both are undefined or null
    if ((a === undefined || a === null) && (b === undefined || b === null)) return true;

    // Handle boolean false vs undefined/null (for settings where omitting means false)
    if ((a === false || a === undefined || a === null) && (b === false || b === undefined || b === null)) {
        if (typeof a === 'boolean' || typeof b === 'boolean') {
            return true;
        }
    }

    // Handle empty string vs undefined/null
    if ((a === '' || a === undefined || a === null) && (b === '' || b === undefined || b === null)) {
        if (typeof a === 'string' || typeof b === 'string') {
            return true;
        }
    }

    // Handle empty array vs undefined/null
    const isArrayA = Array.isArray(a);
    const isArrayB = Array.isArray(b);
    if ((isArrayA && a.length === 0 && (b === undefined || b === null)) ||
        (isArrayB && b.length === 0 && (a === undefined || a === null))) {
        return true;
    }

    if (isArrayA !== isArrayB) return false;
    if (isArrayA && isArrayB) {
        if (a.length !== b.length) return false;
        for (let i = 0; i < a.length; i++) {
            if (!areSettingsEqual(a[i], b[i])) return false;
        }
        return true;
    }

    // Handle empty object vs undefined/null
    const isObjA = a !== null && typeof a === 'object';
    const isObjB = b !== null && typeof b === 'object';
    if ((isObjA && Object.keys(a).length === 0 && (b === undefined || b === null)) ||
        (isObjB && Object.keys(b).length === 0 && (a === undefined || a === null))) {
        return true;
    }

    if (!isObjA || !isObjB) {
        return false;
    }

    const keysA = Object.keys(a).filter(k => a[k] !== undefined && a[k] !== null);
    const keysB = Object.keys(b).filter(k => b[k] !== undefined && b[k] !== null);

    const allKeys = new Set([...keysA, ...keysB]);
    for (const key of allKeys) {
        if (!areSettingsEqual(a[key], b[key])) {
            return false;
        }
    }
    return true;
}
