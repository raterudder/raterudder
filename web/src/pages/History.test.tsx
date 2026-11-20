import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import userEvent from '@testing-library/user-event';
import History from './History';
import { Router } from 'wouter';
import * as api from '../api';

const { fetchHistoryEnergy, fetchSettings } = api;

const { capturedAreaCharts } = vi.hoisted(() => ({
    capturedAreaCharts: [] as Array<{ data?: any[]; syncId?: string }>,
}));

vi.mock('../api');

vi.mock('recharts', async () => {
    const OriginalModule = await vi.importActual('recharts');
    return {
        ...OriginalModule,
        ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
            <div className="recharts-responsive-container" style={{ width: 800, height: 800 }}>
                {children}
            </div>
        ),
        AreaChart: ({
            data,
            syncId,
            children,
        }: {
            data?: any[];
            syncId?: string;
            children?: React.ReactNode;
        }) => {
            capturedAreaCharts.push({ data, syncId });
            return (
                <div data-testid="area-chart">
                    {React.Children.toArray(children).filter(
                        (child: any) => child && child.type !== 'defs'
                    )}
                </div>
            );
        },
        Area: () => <div data-testid="area" />,
        Line: () => <div data-testid="line" />,
        XAxis: () => <div data-testid="x-axis" />,
        YAxis: () => <div data-testid="y-axis" />,
        CartesianGrid: () => <div data-testid="cartesian-grid" />,
        Tooltip: () => <div data-testid="tooltip" />,
    };
});

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
        capturedAreaCharts.length = 0;
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

    it('joins weather by epoch timestamp with first-match precedence, skips NaN timestamps, and sorts unsorted energy rows', async () => {
        let firstMatchIrradianceReads = 0;
        let duplicateIrradianceReads = 0;
        let invalidWeatherReads = 0;

        const invalidWeather: Record<string, any> = {
            tsHourStart: 'invalid-weather-ts',
            improvedSolarGeneration: 9.9,
            improvedHomeLoad: 9.9,
        };
        Object.defineProperty(invalidWeather, 'irradiance', {
            get() {
                invalidWeatherReads++;
                return 999;
            },
            enumerable: true,
        });

        const firstTenAmWeather: Record<string, any> = {
            tsHourStart: '2023-10-27T10:00:00.000Z',
            improvedSolarGeneration: 2.1,
            improvedHomeLoad: 1.24,
        };
        Object.defineProperty(firstTenAmWeather, 'irradiance', {
            get() {
                firstMatchIrradianceReads++;
                return 480;
            },
            enumerable: true,
        });

        const duplicateTenAmWeather: Record<string, any> = {
            tsHourStart: '2023-10-27T10:00:00Z',
            improvedSolarGeneration: 0.5,
            improvedHomeLoad: 4.5,
        };
        Object.defineProperty(duplicateTenAmWeather, 'irradiance', {
            get() {
                duplicateIrradianceReads++;
                return 100;
            },
            enumerable: true,
        });

        const mockData = {
            energy: [
                {
                    tsHourStart: '2023-10-27T12:00:00Z',
                    maxBatterySOC: 90,
                    solarKWH: 3.1,
                    homeKWH: 1.45,
                    gridImportKWH: 0.12,
                    gridExportKWH: 0.85,
                },
                {
                    tsHourStart: 'invalid-energy-ts',
                    maxBatterySOC: 70,
                    solarKWH: 0,
                    homeKWH: 0.5,
                    gridImportKWH: 0.5,
                    gridExportKWH: 0,
                },
                {
                    tsHourStart: '2023-10-27T10:00:00Z',
                    maxBatterySOC: 75,
                    solarKWH: 2.0,
                    homeKWH: 1.15,
                    gridImportKWH: 0.4,
                    gridExportKWH: 0,
                },
            ],
            weather: [
                invalidWeather,
                firstTenAmWeather,
                duplicateTenAmWeather,
                {
                    tsHourStart: '2023-10-27T12:00:00Z',
                    irradiance: 620,
                    improvedSolarGeneration: 3.0,
                    improvedHomeLoad: 1.38,
                },
            ],
        };
        (fetchHistoryEnergy as any).mockResolvedValue(mockData);

        renderWithRouter(<History settings={{ release: 'staging' } as any} />);

        await waitFor(() => {
            expect(screen.getByText('Battery (%)')).toBeInTheDocument();
            expect(screen.getByText('Solar Generation (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Estimated Irradiance')).toBeInTheDocument();
            expect(screen.getByText('Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Grid (kWh)')).toBeInTheDocument();
        });

        expect(firstMatchIrradianceReads).toBe(1);
        expect(duplicateIrradianceReads).toBe(0);
        expect(invalidWeatherReads).toBe(0);

        expect(capturedAreaCharts).toHaveLength(5);
        const chartData = capturedAreaCharts[0].data;
        for (const chart of capturedAreaCharts) {
            expect(chart.syncId).toBe('history');
            expect(chart.data).toBe(chartData);
        }

        expect(chartData).toEqual([
            {
                tsHourStart: '2023-10-27T10:00:00Z',
                maxBatterySOC: 75,
                solarKWH: 2.0,
                homeKWH: 1.1,
                gridImportKWH: 0.4,
                gridExportKWH: 0,
                irradiance: 480,
                improvedSolarGeneration: 2.1,
                improvedHomeLoad: 1.2,
            },
            {
                tsHourStart: '2023-10-27T12:00:00Z',
                maxBatterySOC: 90,
                solarKWH: 3.1,
                homeKWH: 1.4,
                gridImportKWH: 0.1,
                gridExportKWH: 0.8,
                irradiance: 620,
                improvedSolarGeneration: 3.0,
                improvedHomeLoad: 1.3,
            },
            {
                tsHourStart: 'invalid-energy-ts',
                maxBatterySOC: 70,
                solarKWH: 0,
                homeKWH: 0.5,
                gridImportKWH: 0.5,
                gridExportKWH: 0,
                irradiance: undefined,
                improvedSolarGeneration: undefined,
                improvedHomeLoad: undefined,
            },
        ]);
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

