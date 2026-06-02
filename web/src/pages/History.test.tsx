import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import userEvent from '@testing-library/user-event';
import History from './History';
import { Router } from 'wouter';
import * as api from '../api';

const { fetchHistoryEnergy, fetchSettings } = api;

vi.mock('../api');

const renderWithRouter = (component: React.ReactNode) => {
    return render(
        <Router>
            {component}
        </Router>
    );
};

describe('History', () => {
    beforeEach(() => {
        window.history.replaceState({}, '', '/history');
        vi.resetAllMocks();
    });

    it('renders loading state initially', () => {
        (fetchHistoryEnergy as any).mockReturnValueOnce(new Promise(() => {}));
        renderWithRouter(<History />);
        expect(screen.getByText('Loading history...')).toBeInTheDocument();
    });

    it('renders charts when data is loaded', async () => {
        const mockData = {
            energy: [
                {
                    tsHourStart: '2023-10-27T10:00:00Z',
                    maxBatterySOC: 80,
                    solarKWH: 2.5,
                    homeKWH: 1.2,
                    gridImportKWH: 0.5,
                    gridExportKWH: 0
                }
            ],
            weather: [
                {
                    tsHourStart: '2023-10-27T10:00:00Z',
                    irradiance: 500,
                    improvedSolarGeneration: 2.4
                }
            ]
        };
        (fetchHistoryEnergy as any).mockResolvedValue(mockData);

        renderWithRouter(<History />);

        await waitFor(() => {
            expect(screen.getByText('Battery (%)')).toBeInTheDocument();
            expect(screen.getByText('Solar Generation (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Grid (kWh)')).toBeInTheDocument();
        });
    });

    it('renders error message on fetch failure', async () => {
        (fetchHistoryEnergy as any).mockRejectedValue(new Error('API Error'));
        renderWithRouter(<History />);
        await waitFor(() => {
            expect(screen.getByText('Error: API Error')).toBeInTheDocument();
        });
    });

    it('renders no data message when empty', async () => {
        (fetchHistoryEnergy as any).mockResolvedValue({ energy: [], weather: [] });
        renderWithRouter(<History />);
        await waitFor(() => {
            expect(screen.getByText('No history found for this date.')).toBeInTheDocument();
        });
    });

    it('does not refetch settings when date changes', async () => {
        const user = userEvent.setup();
        (fetchHistoryEnergy as any).mockResolvedValue({ energy: [], weather: [] });
        (fetchSettings as any).mockResolvedValue({ release: 'production' });

        renderWithRouter(<History />);

        await waitFor(() => {
            expect(screen.getByText(/Prev/)).toBeInTheDocument();
        });

        const prevButton = screen.getByText(/Prev/);
        await user.click(prevButton);

        await waitFor(() => {
            expect((fetchHistoryEnergy as any).mock.calls.length).toBeGreaterThanOrEqual(2);
            expect((fetchSettings as any).mock.calls.length).toBe(1);
        });
    });
});
