import { describe, it, expect } from 'vitest';
import { areSettingsEqual } from './settingsUtils';

describe('areSettingsEqual', () => {
    it('returns true for identical primitive values', () => {
        expect(areSettingsEqual(1, 1)).toBe(true);
        expect(areSettingsEqual('abc', 'abc')).toBe(true);
        expect(areSettingsEqual(true, true)).toBe(true);
        expect(areSettingsEqual(false, false)).toBe(true);
        expect(areSettingsEqual(null, null)).toBe(true);
        expect(areSettingsEqual(undefined, undefined)).toBe(true);
    });

    it('returns false for different primitive values', () => {
        expect(areSettingsEqual(1, 2)).toBe(false);
        expect(areSettingsEqual('a', 'b')).toBe(false);
        expect(areSettingsEqual(true, false)).toBe(false);
    });

    it('handles boolean false and undefined/null equivalence for settings', () => {
        expect(areSettingsEqual(false, undefined)).toBe(true);
        expect(areSettingsEqual(undefined, false)).toBe(true);
        expect(areSettingsEqual(false, null)).toBe(true);
        expect(areSettingsEqual(true, undefined)).toBe(false);
        expect(areSettingsEqual(true, false)).toBe(false);
    });

    it('handles empty string and undefined/null equivalence', () => {
        expect(areSettingsEqual('', undefined)).toBe(true);
        expect(areSettingsEqual(undefined, '')).toBe(true);
        expect(areSettingsEqual('', null)).toBe(true);
        expect(areSettingsEqual('abc', undefined)).toBe(false);
    });

    it('handles empty array and undefined/null equivalence', () => {
        expect(areSettingsEqual([], undefined)).toBe(true);
        expect(areSettingsEqual(undefined, [])).toBe(true);
        expect(areSettingsEqual([], null)).toBe(true);
        expect(areSettingsEqual([1], [])).toBe(false);
    });

    it('handles empty object and undefined/null equivalence', () => {
        expect(areSettingsEqual({}, undefined)).toBe(true);
        expect(areSettingsEqual(undefined, {})).toBe(true);
        expect(areSettingsEqual({}, null)).toBe(true);
        expect(areSettingsEqual({ a: 1 }, {})).toBe(false);
    });

    it('ignores key insertion order in objects', () => {
        const objA = {
            dryRun: false,
            utilityProvider: 'comed',
            manageTOUSchedules: true,
            customGridSettings: true,
        };
        const objB = {
            manageTOUSchedules: true,
            customGridSettings: true,
            utilityProvider: 'comed',
            dryRun: false,
        };
        expect(areSettingsEqual(objA, objB)).toBe(true);
    });

    it('correctly compares nested objects and arrays', () => {
        const complexA = {
            rateOptions: { netMeteringScheme: 'net', multiplier: 1.5 },
            periods: [{ start: 1, end: 5 }],
        };
        const complexB = {
            periods: [{ start: 1, end: 5 }],
            rateOptions: { multiplier: 1.5, netMeteringScheme: 'net' },
        };
        expect(areSettingsEqual(complexA, complexB)).toBe(true);

        const complexC = {
            periods: [{ start: 1, end: 6 }],
            rateOptions: { multiplier: 1.5, netMeteringScheme: 'net' },
        };
        expect(areSettingsEqual(complexA, complexC)).toBe(false);
    });
});
