import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import AdminPage from './AdminPage';
import {
    listSites,
    listFeedback,
    listInterest,
    setSiteAlias,
    listUserSites,
    listFeatures,
    updateFeatures,
} from '../api';
import { Router } from 'wouter';

vi.mock('../api', () => ({
    listSites: vi.fn(),
    listFeedback: vi.fn(),
    listInterest: vi.fn(),
    setSiteAlias: vi.fn(),
    listUserSites: vi.fn(),
    listFeatures: vi.fn(),
    updateFeatures: vi.fn(),
}));

describe('AdminPage', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        localStorage.clear();
        vi.mocked(listFeatures).mockResolvedValue([]);
    });

    it('renders loading state initially', () => {
        vi.mocked(listFeedback).mockReturnValue(new Promise(() => {}));
        vi.mocked(listInterest).mockReturnValue(new Promise(() => {}));
        render(<AdminPage />);
        expect(screen.getByText('Loading Admin Data...')).toBeInTheDocument();
    });

    it('renders error state on API failure', async () => {
        vi.mocked(listFeedback).mockRejectedValue(new Error('Forbidden Access'));
        vi.mocked(listInterest).mockResolvedValue([]);
        render(<AdminPage />);

        await waitFor(() => {
            expect(screen.getByText('Forbidden Access')).toBeInTheDocument();
        });
    });

    it('lazy-loads sites only when Site List section is opened and renders feedback and interest', async () => {
        const recentTs = new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString();
        const mockSites: any = [
            {
                id: 'site1',
                lastAction: {
                    description: 'Charging battery for profit',
                    timestamp: '2025-01-01T12:00:00Z',
                    systemStatus: { batterySOC: 50.5 },
                },
            },
            { id: 'site2' },
        ];
        const mockFeedback: any = [
            {
                id: 'fb1',
                siteID: 'site1',
                userID: 'user1',
                sentiment: 'happy',
                comment: 'Test comment',
                timestamp: recentTs,
                extra: { test: 'data' },
            },
        ];
        const mockInterest: any = [
            {
                email: 'interest@example.com',
                utility: 'other',
                utilityProviderName: 'Strange Utility',
                timestamp: recentTs,
            },
        ];
        vi.mocked(listSites).mockResolvedValue(mockSites);
        vi.mocked(listFeedback).mockResolvedValue(mockFeedback);
        vi.mocked(listInterest).mockResolvedValue(mockInterest);

        render(
            <Router>
                <AdminPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText(/Test comment/)).toBeInTheDocument();
            expect(screen.getByText('interest@example.com')).toBeInTheDocument();
            expect(screen.getByText(/Strange Utility/)).toBeInTheDocument();
        });

        // Sites should not be fetched until the Site List section is opened
        expect(listSites).not.toHaveBeenCalled();

        // Open Site List section
        fireEvent.click(screen.getByText('Site List'));

        await waitFor(() => {
            expect(listSites).toHaveBeenCalledTimes(1);
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.getByText('site2')).toBeInTheDocument();
            expect(screen.getByText(/Charging battery for profit/)).toBeInTheDocument();
            expect(screen.getByText(/50\.5%/)).toBeInTheDocument();
        });

        const links = screen.getAllByRole('link');
        expect(links).toHaveLength(2);
        expect(links[0]).toHaveAttribute('href', '/dashboard?viewSite=site1');
        expect(links[1]).toHaveAttribute('href', '/dashboard?viewSite=site2');
    });

    it('tracks 7d and unseen badges with localStorage for feedback and interest', async () => {
        const oldTs = '2024-01-01T12:00:00Z';
        const recentTs = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();

        localStorage.setItem('admin_last_seen_feedback_ts', oldTs);

        vi.mocked(listFeedback).mockResolvedValue([
            {
                id: 'fb-new',
                siteID: 'site1',
                userID: 'u1',
                sentiment: 'happy',
                comment: 'Recent feedback',
                timestamp: recentTs,
                extra: {},
            },
            {
                id: 'fb-old',
                siteID: 'site1',
                userID: 'u1',
                sentiment: 'neutral',
                comment: 'Old feedback',
                timestamp: oldTs,
                extra: {},
            },
        ]);
        vi.mocked(listInterest).mockResolvedValue([
            {
                email: 'new@example.com',
                utility: 'comed',
                battery: 'tesla',
                utilityProviderName: '',
                state: 'IL',
                planName: '',
                batteryName: '',
                comments: '',
                timestamp: recentTs,
            },
        ]);

        render(
            <Router>
                <AdminPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getAllByText('1 in last 7d')).toHaveLength(2);
            expect(screen.getAllByText('1 new')).toHaveLength(2);
        });

        fireEvent.click(screen.getByRole('button', { name: 'Mark Feedback Seen' }));
        expect(localStorage.getItem('admin_last_seen_feedback_ts')).toBe(recentTs);

        await waitFor(() => {
            expect(
                screen.queryByRole('button', { name: 'Mark Feedback Seen' })
            ).not.toBeInTheDocument();
        });
    });

    it('defaults feedback and interest sections to closed when all items have already been seen', async () => {
        const seenTs = '2026-10-05T12:23:01.045674015Z';
        localStorage.setItem('admin_last_seen_feedback_ts', seenTs);
        localStorage.setItem('admin_last_seen_interest_ts', seenTs);

        vi.mocked(listFeedback).mockResolvedValue([
            {
                id: 'fb-seen',
                siteID: 'site1',
                userID: 'u1',
                sentiment: 'happy',
                comment: 'Already read feedback',
                timestamp: seenTs,
                extra: {},
            },
        ]);
        vi.mocked(listInterest).mockResolvedValue([
            {
                email: 'seen@example.com',
                utility: 'comed',
                battery: 'tesla',
                utilityProviderName: '',
                state: 'IL',
                planName: '',
                batteryName: '',
                comments: '',
                timestamp: seenTs,
            },
        ]);

        render(
            <Router>
                <AdminPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Feedback')).toBeInTheDocument();
            expect(screen.getByText('Interest Submissions')).toBeInTheDocument();
        });

        expect(screen.queryByText(/Already read feedback/)).not.toBeInTheDocument();
        expect(screen.queryByText('seen@example.com')).not.toBeInTheDocument();
    });

    it('renders site aliases and handles editing', async () => {
        const mockSites: any = [
            {
                id: 'site1',
                alias: 'my-site-alias',
                lastAction: {
                    description: 'Charging battery for profit',
                    timestamp: '2025-01-01T12:00:00Z',
                    systemStatus: { batterySOC: 50.5 },
                },
            },
        ];
        vi.mocked(listSites).mockResolvedValue(mockSites);
        vi.mocked(listFeedback).mockResolvedValue([]);
        vi.mocked(listInterest).mockResolvedValue([]);
        vi.mocked(setSiteAlias).mockResolvedValue();

        render(
            <Router>
                <AdminPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Site List')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Site List'));

        await waitFor(() => {
            expect(screen.getByText('Alias: my-site-alias')).toBeInTheDocument();
        });

        const editBtn = screen.getByTitle('Edit Alias');
        fireEvent.click(editBtn);

        const input = screen.getByPlaceholderText('Add site alias...');
        expect(input).toBeInTheDocument();
        expect(input).toHaveValue('my-site-alias');

        fireEvent.change(input, { target: { value: 'new-alias-value' } });

        const saveBtn = screen.getByText('Save');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(setSiteAlias).toHaveBeenCalledWith('site1', 'new-alias-value');
            expect(screen.getByText('Alias: new-alias-value')).toBeInTheDocument();
        });
    });

    it('supports client-side fuzzy autocomplete and falls back to backend search on submit', async () => {
        const mockSites: any = [
            { id: 'site1', alias: 'Main Home' },
            { id: 'site2', alias: 'Cabin Retreat' },
        ];
        const mockUserSites: any = [
            { id: 'site1', name: 'My Solar Site' },
            { id: 'site2', name: 'Other Battery Location' },
        ];
        vi.mocked(listSites).mockResolvedValue(mockSites);
        vi.mocked(listFeedback).mockResolvedValue([]);
        vi.mocked(listInterest).mockResolvedValue([]);
        vi.mocked(listUserSites).mockResolvedValue(mockUserSites);

        render(
            <Router>
                <AdminPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Site List')).toBeInTheDocument();
        });
        fireEvent.click(screen.getByText('Site List'));

        await waitFor(() => {
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.getByText('site2')).toBeInTheDocument();
        });

        const searchInput = screen.getByPlaceholderText('Search site names, IDs or aliases...');

        // 1. Fuzzy match against alias in autocomplete and click autocomplete entry (no backend search)
        const user = userEvent.setup();
        await user.type(searchInput, 'mh');

        const autocompleteOption = await screen.findByText('(Main Home)');
        await user.click(autocompleteOption);

        await waitFor(() => {
            expect(listUserSites).not.toHaveBeenCalled();
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.queryByText('site2')).not.toBeInTheDocument();
        });

        // Clear search
        fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
        await waitFor(() => {
            expect(screen.getByText('site2')).toBeInTheDocument();
        });

        // 2. Submit form without clicking autocomplete -> triggers backend listUserSites()
        fireEvent.change(searchInput, { target: { value: 'solar' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search' }));

        await waitFor(() => {
            expect(listUserSites).toHaveBeenCalledTimes(1);
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.queryByText('site2')).not.toBeInTheDocument();
        });
    });

    it('manages feature flags in the Feature Flags section and per-site card overrides', async () => {
        const initialFlags = [
            {
                name: 'solarModelV2',
                defaultValue: false,
                criteria: [{ siteIDs: ['site1'], value: true }],
            },
            {
                name: 'alphaFeature',
                defaultValue: true,
                criteria: [],
            },
        ];
        vi.mocked(listFeatures).mockResolvedValue(initialFlags);
        vi.mocked(listFeedback).mockResolvedValue([]);
        vi.mocked(listInterest).mockResolvedValue([]);
        vi.mocked(listSites).mockResolvedValue([
            {
                id: 'site1',
                features: { alphaFeature: true, solarModelV2: true },
            },
        ]);

        render(
            <Router>
                <AdminPage />
            </Router>
        );

        await waitFor(() => {
            expect(screen.getByText('Feature Flags')).toBeInTheDocument();
        });

        // Open Feature Flags section
        fireEvent.click(screen.getByText('Feature Flags'));

        await waitFor(() => {
            expect(screen.getByText('alphaFeature')).toBeInTheDocument();
            expect(screen.getByText('solarModelV2')).toBeInTheDocument();
        });

        // Update rollout percentage for solarModelV2
        vi.mocked(updateFeatures).mockResolvedValue();

        const rolloutInput = screen.getByLabelText('Rollout percentage for solarModelV2');
        fireEvent.change(rolloutInput, { target: { value: '50' } });

        const saveRolloutBtns = screen.getAllByRole('button', { name: 'Save Rollout' });
        // Flags are sorted alphabetically: alphaFeature (0), solarModelV2 (1)
        fireEvent.click(saveRolloutBtns[1]);

        await waitFor(() => {
            expect(updateFeatures).toHaveBeenCalledWith({
                solarModelV2: [
                    { siteIDs: ['site1'], value: true },
                    { sitePercent: 50, value: true },
                ],
            });
        });

        // Add multiple sites to solarModelV2 using Tab to create a pill and Enter to submit all at once
        const user = userEvent.setup();
        const addSiteInput = screen.getByLabelText('Add site ID to solarModelV2');
        await user.type(addSiteInput, 'site2{Tab}site3{Enter}');

        await waitFor(() => {
            expect(updateFeatures).toHaveBeenCalledWith({
                solarModelV2: [
                    { siteIDs: ['site1', 'site2', 'site3'], value: true },
                    { sitePercent: 50, value: true },
                ],
            });
        });

        // Open Site List and expand site1's Features collapsible
        fireEvent.click(screen.getByText('Site List'));
        await waitFor(() => {
            expect(screen.getAllByText('site1').length).toBeGreaterThan(0);
        });

        fireEvent.click(screen.getByText('Features'));

        const forceOffBtns = screen.getAllByRole('button', { name: 'Force OFF' });
        fireEvent.click(forceOffBtns[0]); // Force OFF for alphaFeature on site1

        await waitFor(() => {
            expect(updateFeatures).toHaveBeenCalledWith({
                alphaFeature: [{ siteIDs: ['site1'], value: false }],
            });
        });
    });
});
