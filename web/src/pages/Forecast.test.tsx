import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Router } from 'wouter';
import Forecast from './Forecast';
import * as api from '../api';
import { setupDefaultApiMocks } from '../test/apiMocks';
import type { ModelingHour } from '../api';

const { fetchModeling, fetchSettings } = api;

vi.mock('../api');



function makeSimHours(): ModelingHour[] {
    const hours: ModelingHour[] = [];
    const base = new Date('2026-02-11T14:00:00Z');
    for (let i = 0; i < 24; i++) {
        const ts = new Date(base);
        ts.setHours(ts.getHours() + i);
        hours.push({
            ts: ts.toISOString(),
            hour: ts.getHours(),
            netLoadSolarKWH: 1.0 - i * 0.05,
            gridChargeDollarsPerKWH: 0.10 + i * 0.005,
            solarOppDollarsPerKWH: 0.08,
            avgHomeLoadKWH: 1.5,
            predictedSolarKWH: Math.max(0, 3.0 * Math.sin((i / 24) * Math.PI)),
            batteryKWH: 5.0 - i * 0.2,
            startBatteryKWH: 5.0 - i * 0.2,
            batteryCapacityKWH: 10.0,
            batteryReserveKWH: 0.5,
            todaySolarTrend: 1.0,
        });
    }
    return hours;
}

const renderForecast = () => render(<Router><Forecast /></Router>);

describe('Forecast Page', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('shows loading state initially', () => {
        (fetchModeling as any).mockReturnValue(new Promise(() => {}));
        renderForecast();
        expect(screen.getByText(/Loading simulation/)).toBeInTheDocument();
    });

    it('calls fetchModeling and renders 5 charts when no weather data is present', async () => {
        const data = makeSimHours();
        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast();

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(1);
        });

        await waitFor(() => {
            expect(screen.getByText('Battery (if used) (%)')).toBeInTheDocument();
            expect(screen.getByText('Predicted Solar (kWh)')).toBeInTheDocument();
            expect(screen.queryByText('Estimated Irradiance (W/m²)')).not.toBeInTheDocument();
            expect(screen.getByText('Avg Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
        });
    });

    it('calls fetchModeling and renders charts when weather data is present', async () => {
        const data = makeSimHours();
        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: [
                {
                    tsHourStart: data[0].ts,
                    irradiance: 350,
                    improvedSolarGeneration: 2.5,
                    unclippedSolarGeneration: 2.8,
                }
            ]
        });

        renderForecast();

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(1);
        });

        await waitFor(() => {
            expect(screen.getByText('Battery (if used) (%)')).toBeInTheDocument();
            expect(screen.getByText('Avg Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
        });
    });

    it('shows page heading and subtitle', async () => {
        (fetchModeling as any).mockResolvedValue({
            simulation: makeSimHours(),
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Simulation')).toBeInTheDocument();
            expect(screen.getByText(/Predicted energy state starting from/)).toBeInTheDocument();
        });
    });

    it('shows page subtitle with updated timestamp from backend if present', async () => {
        const updatedTimeStr = '2026-02-11T12:00:00Z';
        (fetchModeling as any).mockResolvedValue({
            simulation: makeSimHours(),
            energyHistory: [],
            priceHistory: [],
            weather: [],
            updated: updatedTimeStr,
        });

        renderForecast();

        await waitFor(() => {
            const expectedTime = new Date(updatedTimeStr).toLocaleTimeString([], {
                hour: 'numeric',
                minute: '2-digit',
                hour12: true,
            });
            expect(screen.getByText(new RegExp("starting from.*" + expectedTime))).toBeInTheDocument();
        });
    });

    it('shows error state when fetch fails', async () => {
        (fetchModeling as any).mockRejectedValue(new Error('Network error'));

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText(/Error: Network error/)).toBeInTheDocument();
        });
    });

    it('shows empty state when no data', async () => {
        (fetchModeling as any).mockResolvedValue([]);

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('No simulation data available.')).toBeInTheDocument();
        });
    });

    it('shows the include previous 24 hours checkbox', async () => {
        (fetchModeling as any).mockResolvedValue({
            simulation: makeSimHours(),
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByRole('switch', { name: /Show Previous 24 Hours/i })).toBeInTheDocument();
        });
    });

    it('renders tomorrow comparison charts when solar1hForecast is present', async () => {
        (fetchSettings as any).mockResolvedValue({ release: 'staging' });
        const data = makeSimHours();
        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: [],
            solar1hForecast: [
                {
                    tsHourStart: '2026-02-12T12:00:00Z',
                    improvedSolarGeneration: 1.5,
                    unclippedSolarGeneration: 1.8,
                }
            ]
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText("Tomorrow's Solar Forecast Comparison")).toBeInTheDocument();
            expect(screen.getByText('Hourly Mean Self-Calculated Solar (1h Forecast) (kWh)')).toBeInTheDocument();
        });
    });

    it('renders VPP event reference area and reference lines when VPP data is present in the simulation', async () => {
        const data = makeSimHours();
        // Set VPP data on some hours
        data[2].vppStandbyAt = data[2].ts;
        data[3].vppStandbyAt = data[2].ts;
        data[4].vppStandbyAt = data[2].ts;
        data[2].vppEndAt = data[5].ts;
        data[3].vppEndAt = data[5].ts;
        data[4].vppEndAt = data[5].ts;

        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast();

        await waitFor(() => {
            const vppLabels = screen.getAllByText('VPP Event');
            expect(vppLabels.length).toBe(1);
        });
    });
});
