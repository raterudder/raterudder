import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    fetchModeling,
    fetchSettings,
    fetchUtilityPeriods,
    submitESSStage,
    updateSettings,
    UpdateSettingsError,
    HIGH_HOME_LOAD_MIN_KW_HIGH,
    HIGH_HOME_LOAD_MIN_KW_LOW,
    HIGH_HOME_LOAD_MIN_KW_MEDIUM,
    type Settings,
} from './api';
import * as apiModule from './api';
import { defaultForecast, defaultSettings } from './test/apiMocks';

describe('api client contracts', () => {
    const fetchMock = vi.fn();

    beforeEach(() => {
        fetchMock.mockReset();
        vi.stubGlobal('fetch', fetchMock);
    });

    afterEach(() => {
        vi.unstubAllGlobals();
        vi.restoreAllMocks();
    });

    it('exports generated High Home Load sensitivity thresholds matching Go constants', () => {
        expect(HIGH_HOME_LOAD_MIN_KW_LOW).toBe(6.0);
        expect(HIGH_HOME_LOAD_MIN_KW_MEDIUM).toBe(4.5);
        expect(HIGH_HOME_LOAD_MIN_KW_HIGH).toBe(3.5);
    });

    it('does not export obsolete fetchActions helper', () => {
        expect('fetchActions' in apiModule).toBe(false);
    });

    it('sends flattened SettingsUpdateRequest without response-only fields or duplicate siteID', async () => {
        fetchMock.mockResolvedValue({
            ok: true,
            status: 200,
            url: 'http://localhost/api/settings',
            json: async () => ({}),
        });

        await updateSettings(
            {
                ...defaultSettings,
                hasCredentials: { franklin: true },
                features: { notifications: true },
            },
            'site-123',
            { franklin: { username: 'user@example.com', password: 'secret' } }
        );

        expect(fetchMock).toHaveBeenCalledTimes(1);
        const [url, init] = fetchMock.mock.calls[0];
        expect(url).toBe('/api/settings');
        expect(init.method).toBe('POST');

        const parsed = JSON.parse(init.body as string);
        expect(parsed.utilityProvider).toBe('comed');
        expect(parsed.siteID).toBe('site-123');
        expect(parsed.credentials).toEqual({
            franklin: { username: 'user@example.com', password: 'secret' },
        });
        expect('hasCredentials' in parsed).toBe(false);
        expect('features' in parsed).toBe(false);
        expect('settings' in parsed).toBe(false);
    });

    it('omits siteID and credentials keys when undefined in updateSettings', async () => {
        fetchMock.mockResolvedValue({
            ok: true,
            status: 200,
            url: 'http://localhost/api/settings',
            json: async () => ({}),
        });

        await updateSettings(defaultSettings);

        const [, init] = fetchMock.mock.calls[0];
        const parsed = JSON.parse(init.body as string);
        expect('siteID' in parsed).toBe(false);
        expect('credentials' in parsed).toBe(false);
    });

    it('surfaces backend error message when updateSettings fails', async () => {
        fetchMock.mockResolvedValue({
            ok: false,
            status: 400,
            url: 'http://localhost/api/settings',
            json: async () => ({ error: 'minimum battery SOC must be between 0 and 100' }),
        });

        await expect(updateSettings(defaultSettings, 'site-123')).rejects.toThrow(
            'minimum battery SOC must be between 0 and 100'
        );
    });

    it('serializes typed UtilityRateOptions in fetchUtilityPeriods', async () => {
        fetchMock.mockResolvedValue({
            ok: true,
            status: 200,
            url: 'http://localhost/api/utility/periods',
            json: async () => [{ name: 'Peak', hours: [{ hourStart: 16, hourEnd: 21 }] }],
        });

        const periods = await fetchUtilityPeriods('site-1', 'comed', 'comed_besh', {
            rateClass: 'singleFamilyWithoutElectricHeat',
            variableDeliveryRate: true,
        });

        expect(periods).toHaveLength(1);
        const [url] = fetchMock.mock.calls[0];
        expect(url).toContain('siteID=site-1');
        expect(url).toContain('utilityProvider=comed');
        expect(url).toContain('utilityRateOptions=');
    });

    it('fetches settings and modeling responses matching generated contracts', async () => {
        fetchMock
            .mockResolvedValueOnce({
                ok: true,
                status: 200,
                url: 'http://localhost/api/settings',
                json: async () => defaultSettings,
            })
            .mockResolvedValueOnce({
                ok: true,
                status: 200,
                url: 'http://localhost/api/forecast',
                json: async () => defaultForecast,
            });

        const settings = await fetchSettings('site-1');
        expect(settings.updateGroup).toBe(1);

        const forecast = await fetchModeling('site-1', 'conservative', true);
        expect(forecast.updated).toBe('2026-01-01T00:00:00Z');
        expect('weather' in forecast).toBe(false);
    });

    it('sends typed credentials payload in submitESSStage', async () => {
        fetchMock.mockResolvedValue({
            ok: true,
            status: 200,
            url: 'http://localhost/api/ess/stage',
            json: async () => ({}),
        });

        await submitESSStage('enphase', { username: 'user@example.com' }, 'site-1');

        const [url, init] = fetchMock.mock.calls[0];
        expect(url).toBe('/api/ess/stage');
        expect(JSON.parse(init.body as string)).toEqual({
            ess: 'enphase',
            credentials: { enphase: { username: 'user@example.com' } },
            siteID: 'site-1',
        });
    });
});

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
