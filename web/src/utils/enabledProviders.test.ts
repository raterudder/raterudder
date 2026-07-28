import { describe, it, expect, beforeEach } from 'vitest';
import { getEnabledProviders, isESSEnabled, checkAndStoreEnabledParam } from './enabledProviders';

describe('enabledProviders utility', () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it('stores enabled param in localStorage when present', () => {
        checkAndStoreEnabledParam('?enabled=enphase');
        expect(localStorage.getItem('enabled')).toBe('enphase');
        expect(isESSEnabled('enphase')).toBe(true);
        expect(isESSEnabled('tesla')).toBe(false);
    });

    it('merges multiple enabled params without duplicates', () => {
        localStorage.setItem('enabled', 'enphase');
        checkAndStoreEnabledParam('?enabled=tesla,enphase');
        expect(localStorage.getItem('enabled')).toBe('enphase,tesla');
        expect(isESSEnabled('enphase')).toBe(true);
        expect(isESSEnabled('tesla')).toBe(true);
    });

    it('handles comma-separated strings in localStorage', () => {
        localStorage.setItem('enabled', 'enphase, tesla');
        expect(getEnabledProviders()).toEqual(['enphase', 'tesla']);
        expect(isESSEnabled('enphase')).toBe(true);
        expect(isESSEnabled('TESLA')).toBe(true);
    });

    it('returns false for empty or non-matching ESS ids', () => {
        expect(isESSEnabled('')).toBe(false);
        expect(isESSEnabled('unknown')).toBe(false);
    });
});
