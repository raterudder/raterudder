import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Router } from 'wouter';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import SupportedUtilitiesPage from './SupportedUtilitiesPage';
import { fetchUtilities, type UtilityProviderInfo } from '../api';
import { prefetchSupportedUtilities, resetUtilitiesPrefetch } from '../utils/utilitiesPrefetch';
import { routeImporters } from '../utils/routePreload';

vi.mock('../api', () => ({
    fetchUtilities: vi.fn(),
}));

const sampleUtilities: UtilityProviderInfo[] = [
    {
        id: 'comed',
        name: 'Commonwealth Edison (ComEd)',
        rates: [
            { id: 'besh', name: 'Hourly Pricing (BESH)' },
        ],
    },
    {
        id: 'pg_e',
        name: 'Pacific Gas & Electric (PG&E)',
        rates: [
            { id: 'e_tou_c', name: 'Time-of-Use (E-TOU-C)' },
            { id: 'ev2_a', name: 'Home Charging (EV2-A)' },
        ],
    },
    {
        id: 'tesla_electric_tx',
        name: 'Tesla Electric (Texas)',
        rates: [
            { id: 'dynamic', name: 'Tesla Dynamic Rate' },
        ],
    },
    {
        id: 'tou_example',
        name: 'Example Utility',
        hidden: true,
        rates: [],
    },
] as UtilityProviderInfo[];

describe('SupportedUtilitiesPage Component', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        resetUtilitiesPrefetch();
        vi.mocked(fetchUtilities).mockResolvedValue(sampleUtilities);
    });

    it('renders fetched utilities and filters out hidden/example entries', async () => {
        render(
            <Router>
                <SupportedUtilitiesPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Commonwealth Edison (ComEd)')).toBeInTheDocument();
        });

        expect(screen.getByText('Pacific Gas & Electric (PG&E)')).toBeInTheDocument();
        expect(screen.getByText('Tesla Electric (Texas)')).toBeInTheDocument();
        expect(screen.queryByText('Example Utility')).not.toBeInTheDocument();
        expect(screen.getByText('Time-of-Use (E-TOU-C)')).toBeInTheDocument();
    });

    it('filters utilities by search query across utility name, region, and rate plan', async () => {
        const user = userEvent.setup();
        render(
            <Router>
                <SupportedUtilitiesPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Commonwealth Edison (ComEd)')).toBeInTheDocument();
        });

        const searchInput = screen.getByRole('searchbox', {
            name: /Search supported utilities and rate plans/i,
        });

        // Search by rate plan code
        await user.type(searchInput, 'EV2-A');
        expect(screen.getByText('Pacific Gas & Electric (PG&E)')).toBeInTheDocument();
        expect(screen.queryByText('Commonwealth Edison (ComEd)')).not.toBeInTheDocument();
        expect(screen.queryByText('Tesla Electric (Texas)')).not.toBeInTheDocument();

        // Clear search button restores all utilities
        await user.click(screen.getByRole('button', { name: /Clear search/i }));
        expect(screen.getByText('Commonwealth Edison (ComEd)')).toBeInTheDocument();
        expect(screen.getByText('Tesla Electric (Texas)')).toBeInTheDocument();
    });

    it('filters utilities by category tab', async () => {
        const user = userEvent.setup();
        render(
            <Router>
                <SupportedUtilitiesPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Tesla Electric (Texas)')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('tab', { name: /Texas \/ ERCOT/i }));
        expect(screen.getByText('Tesla Electric (Texas)')).toBeInTheDocument();
        expect(screen.queryByText('Commonwealth Edison (ComEd)')).not.toBeInTheDocument();
        expect(screen.queryByText('Pacific Gas & Electric (PG&E)')).not.toBeInTheDocument();
    });

    it('shows empty state with request utility link when search returns no matches', async () => {
        const user = userEvent.setup();
        render(
            <Router>
                <SupportedUtilitiesPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Commonwealth Edison (ComEd)')).toBeInTheDocument();
        });

        const searchInput = screen.getByRole('searchbox', {
            name: /Search supported utilities and rate plans/i,
        });
        await user.type(searchInput, 'NonExistentGridPower999');

        expect(screen.getByText('No matching utilities or rate plans found')).toBeInTheDocument();
        const requestLinks = screen.getAllByRole('link', { name: /Request Your Utility/i });
        expect(requestLinks.length).toBeGreaterThanOrEqual(2);
        expect(requestLinks[0]).toHaveAttribute('href', '/welcome?requestUtility=1');

        // Reset button clears search and restores list
        await user.click(screen.getByRole('button', { name: /Show All Utilities/i }));
        expect(screen.getByText('Commonwealth Edison (ComEd)')).toBeInTheDocument();
    });

    it('initializes synchronously without refetching when prefetched utilities have already resolved', async () => {
        await prefetchSupportedUtilities();
        expect(fetchUtilities).toHaveBeenCalledTimes(1);

        render(
            <Router>
                <SupportedUtilitiesPage />
            </Router>
        );

        expect(screen.queryByText('Loading supported utilities...')).not.toBeInTheDocument();
        expect(screen.getByText('Commonwealth Edison (ComEd)')).toBeInTheDocument();
        expect(screen.queryByText('Example Utility')).not.toBeInTheDocument();
        expect(fetchUtilities).toHaveBeenCalledTimes(1);
    });

    it('resolves route importer immediately and renders inline loading state while in-flight prefetch is pending', async () => {
        let resolveUtilities!: (value: UtilityProviderInfo[]) => void;
        vi.mocked(fetchUtilities).mockReturnValue(
            new Promise<UtilityProviderInfo[]>((resolve) => {
                resolveUtilities = resolve;
            })
        );

        const mod = await routeImporters['/utilities']();
        expect(mod.default).toBe(SupportedUtilitiesPage);

        // Allow dynamic import('./utilitiesPrefetch') inside routeImporters['/utilities'] to start the prefetch
        await act(async () => {
            await Promise.resolve();
        });

        render(
            <Router>
                <SupportedUtilitiesPage />
            </Router>
        );

        // Static page copy and inline loading message render immediately while API request is still pending
        expect(screen.getByText(/How RateRudder Models Notable Grids/i)).toBeInTheDocument();
        expect(screen.getByText('Loading supported utilities...')).toBeInTheDocument();
        expect(fetchUtilities).toHaveBeenCalledTimes(1);

        await act(async () => {
            resolveUtilities(sampleUtilities);
        });

        await waitFor(() => {
            expect(screen.getByText('Commonwealth Edison (ComEd)')).toBeInTheDocument();
        });
        expect(screen.queryByText('Loading supported utilities...')).not.toBeInTheDocument();
        expect(fetchUtilities).toHaveBeenCalledTimes(1);
    });
});
