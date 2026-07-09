import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import AdminPage from './AdminPage';
import { listSites, listFeedback, listInterest, setSiteAlias, listUserSites } from '../api';
import { Router } from 'wouter';

vi.mock('../api', () => ({
    listSites: vi.fn(),
    listFeedback: vi.fn(),
    listInterest: vi.fn(),
    setSiteAlias: vi.fn(),
    listUserSites: vi.fn(),
}));

describe('AdminPage', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders loading state initially', () => {
        vi.mocked(listSites).mockReturnValue(new Promise(() => {}));
        vi.mocked(listFeedback).mockReturnValue(new Promise(() => {}));
        render(<AdminPage />);
        expect(screen.getByText('Loading Admin Data...')).toBeInTheDocument();
    });

    it('renders error state on API failure', async () => {
        vi.mocked(listSites).mockRejectedValue(new Error('Forbidden Access'));
        vi.mocked(listFeedback).mockResolvedValue([]);
        render(<AdminPage />);

        await waitFor(() => {
            expect(screen.getByText('Forbidden Access')).toBeInTheDocument();
        });
    });

    it('renders list of sites on API success', async () => {
        const mockSites: any = [
            {
                id: 'site1',
                lastAction: {
                    description: 'Charging battery for profit',
                    timestamp: '2025-01-01T12:00:00Z',
                    systemStatus: { batterySOC: 50.5 }
                }
            },
            { id: 'site2' }
        ];
        const mockFeedback: any = [
            {
                id: 'fb1',
                siteID: 'site1',
                userID: 'user1',
                sentiment: 'happy',
                comment: 'Test comment',
                timestamp: '2025-01-01T12:00:00Z',
                extra: { 'test': 'data' }
            }
        ];
        const mockInterest: any = [
            {
                email: 'interest@example.com',
                utility: 'other',
                utilityProviderName: 'Strange Utility',
                timestamp: '2025-01-01T12:00:00Z'
            }
        ];
        vi.mocked(listSites).mockResolvedValue(mockSites);
        vi.mocked(listFeedback).mockResolvedValue(mockFeedback);
        vi.mocked(listInterest).mockResolvedValue(mockInterest);

        render(<Router><AdminPage /></Router>);

        await waitFor(() => {
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.getByText(/Test comment/)).toBeInTheDocument();
            expect(screen.getByText('interest@example.com')).toBeInTheDocument();
            expect(screen.getByText(/Strange Utility/)).toBeInTheDocument();
            expect(screen.getByText('site2')).toBeInTheDocument();
            expect(screen.getByText(/Charging battery for profit/)).toBeInTheDocument();
            expect(screen.getByText(/50\.5%/)).toBeInTheDocument();
        });

        // Verify links are rendered correctly
        const links = screen.getAllByRole('link');
        expect(links).toHaveLength(2);
        expect(links[0]).toHaveAttribute('href', '/dashboard?viewSite=site1');
        expect(links[1]).toHaveAttribute('href', '/dashboard?viewSite=site2');
    });

    it('renders site aliases and handles editing', async () => {
        const mockSites: any = [
            {
                id: 'site1',
                alias: 'my-site-alias',
                lastAction: {
                    description: 'Charging battery for profit',
                    timestamp: '2025-01-01T12:00:00Z',
                    systemStatus: { batterySOC: 50.5 }
                }
            }
        ];
        vi.mocked(listSites).mockResolvedValue(mockSites);
        vi.mocked(listFeedback).mockResolvedValue([]);
        vi.mocked(listInterest).mockResolvedValue([]);
        vi.mocked(setSiteAlias).mockResolvedValue();

        render(<Router><AdminPage /></Router>);

        await waitFor(() => {
            expect(screen.getByText('Alias: my-site-alias')).toBeInTheDocument();
        });

        // Click the edit button
        const editBtn = screen.getByTitle('Edit Alias');
        fireEvent.click(editBtn);

        // Check that input is displayed with initial value
        const input = screen.getByPlaceholderText('Add site alias...');
        expect(input).toBeInTheDocument();
        expect(input).toHaveValue('my-site-alias');

        // Change the value
        fireEvent.change(input, { target: { value: 'new-alias-value' } });

        // Click save
        const saveBtn = screen.getByText('Save');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(setSiteAlias).toHaveBeenCalledWith('site1', 'new-alias-value');
            expect(screen.getByText('Alias: new-alias-value')).toBeInTheDocument();
        });
    });

    it('handles search input and filters the site list based on name matching', async () => {
        const mockSites: any = [
            { id: 'site1' },
            { id: 'site2' }
        ];
        const mockUserSites: any = [
            { id: 'site1', name: 'My Solar Site' },
            { id: 'site2', name: 'Other Battery Location' }
        ];
        vi.mocked(listSites).mockResolvedValue(mockSites);
        vi.mocked(listFeedback).mockResolvedValue([]);
        vi.mocked(listInterest).mockResolvedValue([]);
        vi.mocked(listUserSites).mockResolvedValue(mockUserSites);

        render(<Router><AdminPage /></Router>);

        await waitFor(() => {
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.getByText('site2')).toBeInTheDocument();
        });

        // Find the search input
        const searchInput = screen.getByPlaceholderText('Search site names, IDs or aliases...');
        expect(searchInput).toBeInTheDocument();

        // Type search query
        fireEvent.change(searchInput, { target: { value: 'solar' } });

        // Click search button
        const searchBtn = screen.getByRole('button', { name: 'Search' });
        fireEvent.click(searchBtn);

        // Verify API is called once
        await waitFor(() => {
            expect(listUserSites).toHaveBeenCalledTimes(1);
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.queryByText('site2')).not.toBeInTheDocument();
        });

        // Click clear button
        const clearBtn = screen.getByRole('button', { name: 'Clear' });
        fireEvent.click(clearBtn);

        await waitFor(() => {
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.getByText('site2')).toBeInTheDocument();
        });
    });
});
