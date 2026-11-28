import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import App from './App';
import * as api from './api';
import { setupDefaultApiMocks, defaultAuthStatus, defaultSettings } from './test/apiMocks';

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

        const forecastLink = screen.getAllByRole('link', { name: 'Forecast' })[0];
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
            expect(api.fetchSettings).toHaveBeenCalledWith('site1');
            expect(api.fetchActionsAndSavings).toHaveBeenLastCalledWith(
                expect.any(Date),
                expect.any(Date),
                'site1',
                expect.any(Object)
            );
        });
        expect(screen.getByLabelText('Select Site')).toHaveTextContent('Site 1');

        const forecastLink = screen.getAllByRole('link', { name: 'Forecast' })[0];
        await user.click(forecastLink);

        await waitFor(() => {
            expect(window.location.pathname).toBe('/forecast');
            expect(api.fetchModeling).toHaveBeenLastCalledWith('site1', expect.anything());
        });
    });

    it('renders secondary marketing routes (/how-it-works, /getting-started, /utilities, /changelog)', async () => {
        window.history.pushState(null, '', '/how-it-works');
        const { unmount } = render(<App />);
        expect(await screen.findByText(/Why Battery Optimization Is Harder Than/i)).toBeInTheDocument();
        unmount();

        window.history.pushState(null, '', '/getting-started');
        const { unmount: unmount2 } = render(<App />);
        expect(await screen.findByText(/Up and Running in/i)).toBeInTheDocument();
        unmount2();

        window.history.pushState(null, '', '/utilities');
        const { unmount: unmount3 } = render(<App />);
        expect(await screen.findByText(/How RateRudder Models Notable Grids/i)).toBeInTheDocument();
        expect(await screen.findByText(/Commonwealth Edison \(ComEd\)/i)).toBeInTheDocument();
        unmount3();

        window.history.pushState(null, '', '/changelog');
        const { unmount: unmount4 } = render(<App />);
        expect(await screen.findByText(/What's New in/i)).toBeInTheDocument();
        expect(screen.getByText('November 2026')).toBeInTheDocument();
        expect(screen.getByText('October 2026')).toBeInTheDocument();
        unmount4();

        window.history.pushState(null, '', '/privacy');
        const { unmount: unmount5 } = render(<App />);
        expect(await screen.findByRole('heading', { level: 1, name: 'Privacy Policy' })).toBeInTheDocument();
        expect(screen.getByText(/Last Updated: November 12, 2026/i)).toBeInTheDocument();
        unmount5();

        window.history.pushState(null, '', '/terms');
        render(<App />);
        expect(await screen.findByRole('heading', { level: 1, name: 'Terms of Service' })).toBeInTheDocument();
        expect(screen.getByText(/State of Illinois/i)).toBeInTheDocument();
    });

    it('shows TermsAcceptanceModal when termsAccepted is false and closes after accepting', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/dashboard');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            termsAccepted: false,
            sites: [{ id: 'site1', name: 'Site 1' }],
        });

        render(<App />);

        expect(await screen.findByTestId('terms-acceptance-modal')).toBeInTheDocument();

        await user.click(screen.getByRole('button', { name: 'Agree & Continue' }));

        await waitFor(() => {
            expect(api.acceptTerms).toHaveBeenCalledTimes(1);
            expect(screen.queryByTestId('terms-acceptance-modal')).not.toBeInTheDocument();
        });
    });

    it('renders /utilities immediately with inline loading message while fetchUtilities is pending', async () => {
        let resolveUtilities!: (value: api.UtilityProviderInfo[]) => void;
        vi.mocked(api.fetchUtilities).mockReturnValue(
            new Promise<api.UtilityProviderInfo[]>((resolve) => {
                resolveUtilities = resolve;
            })
        );

        window.history.pushState(null, '', '/utilities');
        render(<App />);

        expect(await screen.findByText(/How RateRudder Models Notable Grids/i)).toBeInTheDocument();
        expect(screen.getByText('Loading supported utilities...')).toBeInTheDocument();
        expect(api.fetchUtilities).toHaveBeenCalledTimes(1);

        resolveUtilities([
            {
                id: 'comed',
                name: 'Commonwealth Edison (ComEd)',
                rates: [{ id: 'besh', name: 'Hourly Pricing (BESH)' }],
            } as api.UtilityProviderInfo,
        ]);

        expect(await screen.findByText(/Commonwealth Edison \(ComEd\)/i)).toBeInTheDocument();
        expect(screen.queryByText('Loading supported utilities...')).not.toBeInTheDocument();
        expect(api.fetchUtilities).toHaveBeenCalledTimes(1);
    });

    it('guards handleSiteChange on /settings before resetting settings or mutating selectedSiteID', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/settings');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [
                { id: 'site1', name: 'Site 1' },
                { id: 'site2', name: 'Site 2' },
            ],
        });

        render(<App />);

        const minSocInput = await screen.findByLabelText(/Minimum Battery %/i);
        await user.clear(minSocInput);
        await user.type(minSocInput, '45');
        expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();

        const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValueOnce(false);

        const trigger = screen.getByLabelText('Select Site');
        await user.click(trigger);
        const site2Option = await screen.findByRole('option', { name: 'Site 2' });
        await user.click(site2Option);

        expect(confirmSpy).toHaveBeenCalledTimes(1);
        expect(confirmSpy).toHaveBeenCalledWith('You have unsaved changes. Are you sure you want to leave?');
        expect(window.location.pathname).toBe('/settings');
        expect(screen.getByLabelText(/Minimum Battery %/i)).toHaveValue(45);
        expect(api.fetchSettings).not.toHaveBeenCalledWith('site2');

        confirmSpy.mockReturnValueOnce(true);
        await user.click(screen.getByLabelText('Select Site'));
        const overviewOption = await screen.findByRole('option', { name: 'Overview' });
        await user.click(overviewOption);

        expect(confirmSpy).toHaveBeenCalledTimes(2);
        await waitFor(() => {
            expect(window.location.pathname).toBe('/dashboard');
        });
        confirmSpy.mockRestore();
    });

    it('redirects /forecast, /history, and /settings to /dashboard when Overview (ALL) is selected', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/forecast');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [
                { id: 'site1', name: 'Site 1' },
                { id: 'site2', name: 'Site 2' },
            ],
        });

        render(<App />);

        const trigger = await screen.findByLabelText('Select Site');
        await user.click(trigger);

        const overviewOption = await screen.findByRole('option', { name: 'Overview' });
        await user.click(overviewOption);

        await waitFor(() => {
            expect(window.location.pathname).toBe('/dashboard');
        });

        for (const guardedPath of ['/forecast', '/history', '/settings']) {
            act(() => {
                window.history.pushState(null, '', guardedPath);
                window.dispatchEvent(new PopStateEvent('popstate'));
            });

            await waitFor(() => {
                expect(window.location.pathname).toBe('/dashboard');
            });
        }
    });

    it('resets selectedSiteID to the single user site instead of trapping in ALL when exiting ?viewSite= via Overview', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/settings?viewSite=other-site');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [{ id: 'site1', name: 'Site 1' }],
        });

        render(<App />);

        const trigger = await screen.findByLabelText('Select Site');
        await user.click(trigger);

        const overviewOption = await screen.findByRole('option', { name: 'Overview' });
        await user.click(overviewOption);

        await waitFor(() => {
            expect(window.location.pathname).toBe('/dashboard');
            expect(window.location.search).not.toContain('viewSite');
            expect(screen.getByTestId('header-site-name')).toHaveTextContent('Site 1');
        });

        // Single-site navigation links remain available (not trapped in Overview)
        expect(screen.getAllByRole('link', { name: 'Forecast' }).length).toBeGreaterThan(0);
        expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument();
        expect(api.fetchSettings).toHaveBeenCalledWith('site1');
    });

    it('retains feature flags when switching to Overview and clears them on logout', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/dashboard');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [
                { id: 'site1', name: 'Site 1' },
                { id: 'site2', name: 'Site 2' },
            ],
        });

        (api.fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'comed',
            ess: 'franklin',
            hasCredentials: { franklin: true },
            features: { notifications: true },
        });

        render(<App />);

        expect(await screen.findByTestId('header-bell-btn')).toBeInTheDocument();

        const trigger = await screen.findByLabelText('Select Site');
        await user.click(trigger);

        const overviewOption = await screen.findByRole('option', { name: 'Overview' });
        await user.click(overviewOption);

        // Notification bell stays visible in Overview because lastFeatures retains notifications: true
        await waitFor(() => {
            expect(screen.getByTestId('header-bell-btn')).toBeInTheDocument();
            expect(screen.queryByRole('link', { name: 'Settings' })).not.toBeInTheDocument();
        });

        // Logging out clears lastFeatures (and loggedIn state)
        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: false,
            sites: [],
        });

        await user.click(screen.getByRole('button', { name: 'Log Out' }));

        await waitFor(() => {
            expect(window.location.pathname).toBe('/');
            expect(screen.queryByTestId('header-bell-btn')).not.toBeInTheDocument();
        });
    });

    it('hides notification bell and modal when viewing an unowned site via ?viewSite= and restores them on owned site', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/dashboard?viewSite=victim-site');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            userID: 'admin-user-1',
            sites: [{ id: 'site1', name: 'Site 1' }],
        });

        (api.fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'comed',
            ess: 'franklin',
            hasCredentials: { franklin: true },
            features: { notifications: true },
        });

        render(<App />);

        expect(await screen.findByText('Admin Mode: Viewing Site victim-site')).toBeInTheDocument();
        await waitFor(() => {
            expect(api.fetchSettings).toHaveBeenCalledWith('victim-site');
        });
        expect(screen.queryByTestId('header-bell-btn')).not.toBeInTheDocument();
        expect(screen.queryByText(/Configuring alerts for/i)).not.toBeInTheDocument();

        // Switch back to the user's owned site from the site dropdown
        const trigger = screen.getByLabelText('Select Site');
        await user.click(trigger);
        const site1Option = await screen.findByRole('option', { name: 'Site 1' });
        await user.click(site1Option);

        const bellBtn = await screen.findByTestId('header-bell-btn');
        await user.click(bellBtn);

        expect(await screen.findByText(/Configuring alerts for Site 1/i)).toBeInTheDocument();

        // Navigating to an unowned ?viewSite= closes the modal and hides the bell
        act(() => {
            window.history.pushState(null, '', '/dashboard?viewSite=victim-site');
            window.dispatchEvent(new PopStateEvent('popstate'));
        });

        await waitFor(() => {
            expect(screen.getByText('Admin Mode: Viewing Site victim-site')).toBeInTheDocument();
            expect(screen.queryByTestId('header-bell-btn')).not.toBeInTheDocument();
            expect(screen.queryByText(/Configuring alerts for/i)).not.toBeInTheDocument();
        });
    });

    it('keeps notification bell and modal enabled when viewing an owned site via ?viewSite=', async () => {
        const user = userEvent.setup();
        window.history.pushState(null, '', '/dashboard?viewSite=site1');

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            userID: 'admin-user-1',
            sites: [
                { id: 'site1', name: 'Site 1' },
                { id: 'site2', name: 'Site 2' },
            ],
        });

        (api.fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'comed',
            ess: 'franklin',
            hasCredentials: { franklin: true },
            features: { notifications: true },
        });

        render(<App />);

        expect(await screen.findByText('Admin Mode: Viewing Site site1')).toBeInTheDocument();
        const bellBtn = await screen.findByTestId('header-bell-btn');
        await user.click(bellBtn);

        expect(await screen.findByText(/Configuring alerts for Site 1/i)).toBeInTheDocument();
    });
});
