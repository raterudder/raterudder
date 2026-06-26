import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import App from './App';
import * as api from './api';
import { setupDefaultApiMocks, defaultAuthStatus } from './test/apiMocks';

vi.mock('./api');

describe('App routing and viewSite parameter preservation', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('preserves viewSite query param during page transitions', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/dashboard?viewSite=test-site-id');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [
                { id: 'site1', name: 'Site 1' },
                { id: 'site2', name: 'Site 2' }
            ]
        });

        render(<App />);

        await waitFor(() => {
            expect(screen.getByText('Dashboard')).toBeInTheDocument();
        });

        expect(window.location.search).toContain('viewSite=test-site-id');

        const forecastLink = screen.getByRole('link', { name: 'Forecast' });
        await user.click(forecastLink);

        await waitFor(() => {
            expect(window.location.pathname).toBe('/forecast');
            expect(window.location.search).toContain('viewSite=test-site-id');
        });

        const settingsLink = screen.getByRole('link', { name: 'Settings' });
        await user.click(settingsLink);

        await waitFor(() => {
            expect(window.location.pathname).toBe('/settings');
            expect(window.location.search).toContain('viewSite=test-site-id');
        });
    });

    it('clears viewSite query param when user changes site from dropdown', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/dashboard?viewSite=test-site-id');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [
                { id: 'site1', name: 'Site 1' },
                { id: 'site2', name: 'Site 2' }
            ]
        });

        render(<App />);

        await waitFor(() => {
            expect(screen.getByText('Dashboard')).toBeInTheDocument();
        });

        const trigger = screen.getByLabelText('Select Site');
        await user.click(trigger);

        const site1Option = await screen.findByRole('option', { name: 'Site 1' });
        await user.click(site1Option);

        await waitFor(() => {
            expect(window.location.search).not.toContain('viewSite');
        });
    });
});
