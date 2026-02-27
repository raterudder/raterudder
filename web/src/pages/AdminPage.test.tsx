import { render, screen, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import AdminPage from './AdminPage';
import { listSites } from '../api';
import { Router } from 'wouter';

vi.mock('../api', () => ({
    listSites: vi.fn(),
}));

describe('AdminPage', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders loading state initially', () => {
        vi.mocked(listSites).mockReturnValue(new Promise(() => {}));
        render(<AdminPage />);
        expect(screen.getByText('Loading Sites...')).toBeInTheDocument();
    });

    it('renders error state on API failure', async () => {
        vi.mocked(listSites).mockRejectedValue(new Error('Forbidden Access'));
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
        vi.mocked(listSites).mockResolvedValue(mockSites);

        render(<Router><AdminPage /></Router>);

        await waitFor(() => {
            expect(screen.getByText('site1')).toBeInTheDocument();
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
