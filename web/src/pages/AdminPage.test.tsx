import { render, screen, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import AdminPage from './AdminPage';
import { listSites, listFeedback } from '../api';
import { Router } from 'wouter';

vi.mock('../api', () => ({
    listSites: vi.fn(),
    listFeedback: vi.fn(),
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
        vi.mocked(listSites).mockResolvedValue(mockSites);
        vi.mocked(listFeedback).mockResolvedValue(mockFeedback);

        render(<Router><AdminPage /></Router>);

        await waitFor(() => {
            expect(screen.getByText('site1')).toBeInTheDocument();
            expect(screen.getByText(/Test comment/)).toBeInTheDocument();
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
});
