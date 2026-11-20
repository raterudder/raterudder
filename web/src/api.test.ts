import { describe, it, expect, beforeEach, vi } from 'vitest';
import { updateSettings, UpdateSettingsError, type Settings } from './api';
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
