import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fetchUtilityPeriods, updateSettings, UpdateSettingsError, type Settings } from './api';
import { defaultSettings } from './test/apiMocks';

const testSettings: Settings = {
    ...defaultSettings,
    solarNetMeteringCreditsValue: 'lowest',
};

describe('updateSettings', () => {
    beforeEach(() => {
        vi.restoreAllMocks();
    });

    it('throws UpdateSettingsError with credentialsSaved=true when server confirms replacement tokens were persisted', async () => {
        vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
            new Response(
                JSON.stringify({
                    error: 'failed to verify ess credentials: failed to get default site id: upstream 503',
                    credentialsSaved: true,
                }),
                { status: 400, headers: { 'Content-Type': 'application/json' } }
            )
        );

        let caught: unknown;
        try {
            await updateSettings(testSettings, 'site1', { tesla: { authCode: 'NA_code' } });
        } catch (err) {
            caught = err;
        }

        expect(caught).toBeInstanceOf(UpdateSettingsError);
        expect((caught as Error).message).toBe('failed to verify ess credentials: failed to get default site id: upstream 503');
        expect((caught as UpdateSettingsError).credentialsSaved).toBe(true);
    });

    it('throws UpdateSettingsError with credentialsSaved=false when error response omits credentialsSaved', async () => {
        vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
            new Response(
                JSON.stringify({
                    error: 'failed to verify ess credentials: failed to exchange auth code: invalid_grant',
                }),
                { status: 400, headers: { 'Content-Type': 'application/json' } }
            )
        );

        let caught: unknown;
        try {
            await updateSettings(testSettings, 'site1', { tesla: { authCode: 'NA_expired' } });
        } catch (err) {
            caught = err;
        }

        expect(caught).toBeInstanceOf(UpdateSettingsError);
        expect((caught as Error).message).toBe('failed to verify ess credentials: failed to exchange auth code: invalid_grant');
        expect((caught as UpdateSettingsError).credentialsSaved).toBe(false);
    });
    it('falls back to default message with credentialsSaved=false on non-JSON error responses', async () => {
        vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
            new Response('Bad Gateway', { status: 502 })
        );

        let caught: unknown;
        try {
            await updateSettings(testSettings, 'site1', { tesla: { authCode: 'NA_code' } });
        } catch (err) {
            caught = err;
        }

        expect(caught).toBeInstanceOf(UpdateSettingsError);
        expect((caught as Error).message).toBe('Failed to update settings');
        expect((caught as UpdateSettingsError).credentialsSaved).toBe(false);
    });
});

describe('fetchUtilityPeriods', () => {
    beforeEach(() => {
        vi.restoreAllMocks();
    });

    it('serializes empty utilityRate and empty utilityRateOptions when defined', async () => {
        const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
            new Response('null', { status: 200, headers: { 'Content-Type': 'application/json' } })
        );

        const periods = await fetchUtilityPeriods('site1', 'comed', '', {});

        expect(fetchSpy).toHaveBeenCalledWith(
            '/api/utility/periods?siteID=site1&utilityProvider=comed&utilityRate=&utilityRateOptions=%7B%7D'
        );
        expect(periods).toBeNull();
    });

    it('omits optional parameters when undefined', async () => {
        const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
            new Response(JSON.stringify([{ name: 'Peak' }]), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );

        const periods = await fetchUtilityPeriods('site1');

        expect(fetchSpy).toHaveBeenCalledWith('/api/utility/periods?siteID=site1');
        expect(periods).toEqual([{ name: 'Peak' }]);
    });

    it('serializes populated utilityRateOptions and throws on non-OK responses', async () => {
        const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
            new Response(JSON.stringify({ error: 'invalid utility provider settings' }), {
                status: 400,
                headers: { 'Content-Type': 'application/json' },
            })
        );

        await expect(
            fetchUtilityPeriods('site1', 'tesla_electric_tx', 'tesla_dynamic', { location: 'invalid' })
        ).rejects.toThrow('invalid utility provider settings');

        expect(fetchSpy).toHaveBeenCalledWith(
            '/api/utility/periods?siteID=site1&utilityProvider=tesla_electric_tx&utilityRate=tesla_dynamic&utilityRateOptions=%7B%22location%22%3A%22invalid%22%7D'
        );
    });
});
