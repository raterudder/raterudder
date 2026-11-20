import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Router } from 'wouter';
import Forecast, { STALE_FORECAST_TIMEOUT_MS } from './Forecast';
import { syncByTimestamp } from '../utils/forecastUtils';
import * as api from '../api';
import { setupDefaultApiMocks } from '../test/apiMocks';
import { BatteryMode, ActionReason, type Plan, type PlanPeriod } from '../api';
const { fetchModeling, fetchSettings } = api;

vi.mock('../api');

function makeTestPlan(): Plan {
    const base = new Date('2026-02-11T14:00:00Z');
    const periods: PlanPeriod[] = [];
    for (let i = 0; i < 24; i++) {
        const start = new Date(base.getTime() + i * 3600000);
        const end = new Date(base.getTime() + (i + 1) * 3600000);
        periods.push({
            tsStart: start.toISOString(),
            tsEnd: end.toISOString(),
            durationHours: 1,
            startSoc: 50 + (i % 5) * 10,
            endSoc: 50 + ((i + 1) % 5) * 10,
            solarKWH: Math.max(0, 2.0 * Math.sin((i / 24) * Math.PI)),
            loadKWH: 1.2,
            importDollars: 0.17 + (i === 18 ? 0.30 : 0),
            exportDollars: 0.08,
            reserveSOC: 20,
            batteryMode: i === 18 ? api.BatteryMode.Load : (i < 5 ? api.BatteryMode.ChargeAny : api.BatteryMode.Standby),
            solarMode: 0 as any,
            reason: i === 18 ? api.ActionReason.ArbitrageSave : api.ActionReason.ArbitrageChargeSave,
            gridImportKWH: i < 5 ? 1.5 : 0,
            gridExportKWH: 0,
            costDollars: 0.15,
        });
    }
    return {
        tsCreated: base.toISOString(),
        horizonHours: 24,
        totalProjectedCost: 3.45,
        totalExportCredits: 0.60,
        netEconomicBenefit: 1.85,
        periods,
    };
}

const renderForecast = (props?: { siteID?: string; settings?: any }) =>
    render(<Router><Forecast {...props} /></Router>);

describe('Forecast Page', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('shows loading state initially', () => {
        (fetchModeling as any).mockReturnValue(new Promise(() => {}));
        renderForecast();
        expect(screen.getByText(/Loading forecast/)).toBeInTheDocument();
    });

    it('calls fetchModeling and renders plan charts when no weather data is present', async () => {
        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast();

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(1);
        });

        await waitFor(() => {
            expect(screen.getByText('Battery Schedule & SOC (%)')).toBeInTheDocument();
            expect(screen.getByText('Predicted Solar & Home Load (kWh)')).toBeInTheDocument();
            expect(screen.queryByText('Estimated Irradiance (W/m²)')).not.toBeInTheDocument();
            expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
        });
    });

    it('calls fetchModeling and renders charts when weather data is present', async () => {
        const plan = makeTestPlan();
        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [
                {
                    tsHourStart: plan.periods[0].tsStart,
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
            expect(screen.getByText('Battery Schedule & SOC (%)')).toBeInTheDocument();
            expect(screen.getByText('Predicted Solar & Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
        });
    });

    it('shows page heading and subtitle with plan timestamp', async () => {
        const plan = makeTestPlan();
        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
            expect(screen.getByText(/Optimal/i)).toBeInTheDocument();
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
        (fetchModeling as any).mockResolvedValue({});

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('No forecast data available.')).toBeInTheDocument();
        });
    });

    it('shows the include previous 24 hours checkbox', async () => {
        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByRole('switch', { name: /Show Previous 24 Hours/i })).toBeInTheDocument();
        });
    });

    it('does not make duplicate fetchModeling calls on initial load', async () => {
        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast({ settings: { homeLoadPredictionStrategy: 'conservative' } });

        await waitFor(() => {
            expect(screen.getByText('Battery Schedule & SOC (%)')).toBeInTheDocument();
        });

        expect(fetchSettings).not.toHaveBeenCalled();
        expect(fetchModeling).toHaveBeenCalledTimes(1);
        expect(fetchModeling).toHaveBeenLastCalledWith(undefined, 'conservative');
    });

    it('opens help dialog when help button on a forecast chart is clicked', async () => {
        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        const user = userEvent.setup();
        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('Battery Schedule & SOC (%)')).toBeInTheDocument();
        });

        const helpBtns = screen.getAllByRole('button', { name: /more info/i });
        const helpBtn = helpBtns[0];
        expect(helpBtn).toBeInTheDocument();

        await user.click(helpBtn);

        expect(await screen.findByRole('dialog')).toBeInTheDocument();
        expect(screen.getByText(/SmartPlan engine/i)).toBeInTheDocument();
    });

    it('renders 24-Hour Energy Plan and hero metrics when plan is present', async () => {
        const plan = makeTestPlan();
        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
            expect(screen.getByText('SmartPlan')).toBeInTheDocument();
            expect(screen.getByText('Battery Schedule & SOC (%)')).toBeInTheDocument();
            expect(screen.queryByText('Projected Benefit')).not.toBeInTheDocument();
            expect(screen.getByText('Projected Grid Cost')).toBeInTheDocument();
            expect(screen.getByText('$3.45')).toBeInTheDocument();
            expect(screen.getByText('Projected Export Credits')).toBeInTheDocument();
            expect(screen.getByText('$0.60')).toBeInTheDocument();
            expect(screen.getByTestId('plan-schedule-beats')).toBeInTheDocument();
            expect(screen.getByText('Predicted Solar & Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByTestId('solar-total-badge')).toBeInTheDocument();
            expect(screen.getByTestId('load-total-badge')).toBeInTheDocument();
            expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
            expect(screen.getByTestId('rate-low-badge')).toHaveTextContent('Low: $0.17/kWh');
            expect(screen.getByTestId('rate-peak-badge')).toHaveTextContent('Peak: $0.47/kWh');
        });
    });

    it('supports toggling show previous 24 hours with plan data', async () => {
        const user = userEvent.setup();
        const plan = makeTestPlan();
        const base = new Date('2026-02-11T14:00:00Z');
        const histTs = new Date(base.getTime() - 3600000).toISOString();
        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [
                {
                    timestamp: histTs,
                    batterySOC: 45,
                    solarKW: 1.0,
                    homeKW: 2.0,
                },
            ],
            priceHistory: [
                {
                    timestamp: histTs,
                    priceDollarsPerKWH: 0.15,
                },
            ],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        const toggle = screen.getByRole('switch', { name: /Show Previous 24 Hours/i });
        expect(toggle).not.toBeChecked();

        await user.click(toggle);
        expect(toggle).toBeChecked();
    });

    it('joins historical energy and price series via first-match Map lookup while skipping invalid timestamps', async () => {
        const user = userEvent.setup();
        const plan = makeTestPlan();
        const base = new Date('2026-02-11T14:00:00Z');
        const h1 = new Date(base.getTime() - 2 * 3600000).toISOString();
        const h2 = new Date(base.getTime() - 3600000).toISOString();

        let firstMatchReads = 0;
        let duplicateMatchReads = 0;
        let invalidPriceReads = 0;

        const invalidPriceEntry: Record<string, any> = {
            tsHourStart: 'invalid-price-ts',
            gridUseDollarsPerKWH: 0,
        };
        Object.defineProperty(invalidPriceEntry, 'dollarsPerKWH', {
            get() {
                invalidPriceReads++;
                return 9.99;
            },
            enumerable: true,
        });

        const firstH1Price: Record<string, any> = {
            tsHourStart: h1,
            gridUseDollarsPerKWH: 0.04,
            exportDollarsPerKWH: 0.06,
        };
        Object.defineProperty(firstH1Price, 'dollarsPerKWH', {
            get() {
                firstMatchReads++;
                return 0.11;
            },
            enumerable: true,
        });

        const duplicateH1Price: Record<string, any> = {
            tsHourStart: h1,
            gridUseDollarsPerKWH: 0.10,
            exportDollarsPerKWH: 0.50,
        };
        Object.defineProperty(duplicateH1Price, 'dollarsPerKWH', {
            get() {
                duplicateMatchReads++;
                return 0.85;
            },
            enumerable: true,
        });

        (fetchModeling as any).mockResolvedValue({
            plan,
            futurePrices: [
                {
                    tsHourStart: new Date(base.getTime() - 3 * 3600000).toISOString(),
                    dollarsPerKWH: 0.10,
                    gridUseDollarsPerKWH: 0.02,
                    exportDollarsPerKWH: 0.05,
                },
            ],
            energyHistory: [
                {
                    tsHourStart: h1,
                    avgBatterySOC: 42,
                    solarKWH: 1.4,
                    homeLoadKWH: 0.95,
                },
                {
                    tsHourStart: h2,
                    avgBatterySOC: 48,
                    solarKWH: 2.1,
                    homeLoadKWH: 1.15,
                },
                {
                    tsHourStart: 'invalid-energy-ts',
                    avgBatterySOC: 50,
                    solarKWH: 0,
                    homeLoadKWH: 0.5,
                },
            ],
            priceHistory: [
                invalidPriceEntry,
                firstH1Price,
                duplicateH1Price,
                {
                    tsHourStart: h2,
                    dollarsPerKWH: 0.14,
                    gridUseDollarsPerKWH: 0.03,
                },
            ],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        const toggle = screen.getByRole('switch', { name: /Show Previous 24 Hours/i });
        await user.click(toggle);
        expect(toggle).toBeChecked();
        expect(screen.getByText('History')).toBeInTheDocument();
        expect(screen.getAllByText('Now')).toHaveLength(3);
        expect(firstMatchReads).toBe(1);
        expect(duplicateMatchReads).toBe(0);
        expect(invalidPriceReads).toBe(0);
    });

    it('snaps sub-hourly 20-minute hover labels to hourly ticks and caches parsed ticks in syncByTimestamp', () => {
        let valueReads = 0;
        const hourlyIsos = [
            '2026-02-11T14:00:00.000Z',
            '2026-02-11T15:00:00.000Z',
            '2026-02-11T16:00:00.000Z',
            '2026-02-11T17:00:00.000Z',
        ];
        const hourlyTicks = hourlyIsos.map((iso) => {
            const tick: Record<string, unknown> = {};
            Object.defineProperty(tick, 'value', {
                get() {
                    valueReads++;
                    return iso;
                },
                enumerable: true,
            });
            return tick;
        });

        // First call indexes tooltipTicks once
        expect(syncByTimestamp(hourlyTicks, { activeLabel: '2026-02-11T15:20:00.000Z' })).toBe(1);
        const initialReads = valueReads;
        expect(initialReads).toBe(hourlyTicks.length);

        // Subsequent sub-hourly and exact syncs reuse the WeakMap index with zero tick re-reads
        expect(syncByTimestamp(hourlyTicks, { activeLabel: '2026-02-11T15:40:00.000Z' })).toBe(1);
        expect(syncByTimestamp(hourlyTicks, { activeLabel: '2026-02-11T16:00:00.000Z' })).toBe(2);
        expect(syncByTimestamp(hourlyTicks, { activeLabel: '2026-02-11T17:40:00.000Z' })).toBe(3);
        expect(syncByTimestamp(hourlyTicks, { activeLabel: '2026-02-11T13:20:00.000Z' })).toBe(0);
        expect(valueReads).toBe(initialReads);

        // Unsorted ticks and invalid labels fall back cleanly
        const unsortedTicks = [
            { value: '2026-02-11T16:00:00.000Z' },
            { value: 'not-a-timestamp' },
            { value: '2026-02-11T14:00:00.000Z' },
            { value: '2026-02-11T15:00:00.000Z' },
        ];
        expect(syncByTimestamp(unsortedTicks, { activeLabel: '2026-02-11T15:20:00.000Z' })).toBe(3);
        expect(syncByTimestamp(unsortedTicks, { activeLabel: 'unparseable', activeTooltipIndex: 2 })).toBe(2);
        expect(syncByTimestamp([], { activeLabel: '2026-02-11T15:00:00.000Z' })).toBe(-1);
    });

    it('renders dynamic battery modes legend and colored line segments for active plan modes', async () => {
        const plan = makeTestPlan();
        plan.periods[17].batteryMode = api.BatteryMode.Load;
        plan.periods[17].reason = api.ActionReason.DischargeAtPeak;
        plan.periods[17].startSoc = 70;
        plan.periods[17].endSoc = 60;

        plan.periods[18].batteryMode = api.BatteryMode.Load;
        plan.periods[18].reason = api.ActionReason.DischargeAtPeak;
        plan.periods[18].startSoc = 60;
        plan.periods[18].endSoc = 50;

        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        // Dynamic legend shows active modes
        expect(screen.getByText('Battery Modes:')).toBeInTheDocument();
        expect(screen.getByText('Grid Charge')).toBeInTheDocument();
        expect(screen.getByText('Solar Charge')).toBeInTheDocument();
        expect(screen.getByText('Powering Home')).toBeInTheDocument();

        // Inactive modes are NOT in the legend
        expect(screen.queryByText('Grid Export')).not.toBeInTheDocument();
        expect(screen.queryByText('Solar Export')).not.toBeInTheDocument();
        expect(screen.queryByText('Peak Discharge')).not.toBeInTheDocument();

        // Reserve reference line is rendered on chart
        expect(screen.getByText('Reserve')).toBeInTheDocument();
    });

    it('renders Solar Export in dynamic legend when direct solar export is active', async () => {
        const plan = makeTestPlan();
        plan.periods.push({
            tsStart: '2026-02-11T14:00:00Z',
            tsEnd: '2026-02-11T15:00:00Z',
            durationHours: 1,
            startSoc: 80,
            endSoc: 60,
            batteryMode: BatteryMode.Load,
            solarMode: 3,
            reason: ActionReason.DirectExport,
            gridImportKWH: 0,
            gridExportKWH: 2.5,
            costDollars: 0,
            importDollars: 0.20,
        });

        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        expect(screen.getByText('Solar Export')).toBeInTheDocument();
    });

    it('normalizes sub-hourly plan intervals for predicted solar and load rates', async () => {
        const plan = makeTestPlan();
        plan.periods = [
            {
                tsStart: '2026-02-11T12:00:00Z',
                tsEnd: '2026-02-11T12:20:00Z',
                durationHours: 0.333333,
                startSoc: 50,
                endSoc: 52,
                solarKWH: 1.53,
                loadKWH: 0.6,
                importDollars: 0.15,
                batteryMode: api.BatteryMode.Standby,
                solarMode: 0 as any,
                reason: api.ActionReason.HoldSimilarPrice,
                gridImportKWH: 0,
                gridExportKWH: 0,
                costDollars: 0,
            },
        ];

        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
            expect(screen.getByText('Predicted Solar & Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByTestId('solar-total-badge')).toHaveTextContent('1.5 kWh');
            expect(screen.getByTestId('load-total-badge')).toHaveTextContent('0.6 kWh');
        });
    });

    it('renders import and export rate legend on grid charge cost chart', async () => {
        const plan = makeTestPlan();
        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
        expect(screen.getByLabelText('Rate Legend')).toBeInTheDocument();
        expect(screen.getByText('Import Rate')).toBeInTheDocument();
        expect(screen.getByText('Export Rate')).toBeInTheDocument();
    });

    it('does not render reserve line or legend if plan periods do not have reserveSOC', async () => {
        const plan = makeTestPlan();
        plan.periods.forEach((p) => {
            delete p.reserveSOC;
        });

        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        expect(screen.queryByText('Reserve')).not.toBeInTheDocument();
    });

    it('incorporates dynamic reserve periods into the reserve line', async () => {
        const plan = makeTestPlan();
        // Dynamic reserve: 20% normally, 40% during peak hours (17-21)
        plan.periods.forEach((p, idx) => {
            p.reserveSOC = idx >= 17 && idx <= 21 ? 40 : 20;
        });

        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        expect(screen.getByText('Reserve')).toBeInTheDocument();
    });

    it('renders system fault banner when latestAction has fault without plan', async () => {
        (fetchModeling as any).mockResolvedValue({
            latestAction: {
                timestamp: '2026-09-30T12:00:00Z',
                batteryMode: 0,
                solarMode: 0,
                description: 'Grid is unavailable',
                fault: true,
            },
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByTestId('forecast-fault-alert')).toBeInTheDocument();
        });

        expect(screen.getByText(/Grid is unavailable/)).toBeInTheDocument();
        expect(screen.getByText(/Automated planning is suspended until the fault is resolved/)).toBeInTheDocument();
        expect(screen.getByText('No forecast data available while system is in a fault state.')).toBeInTheDocument();
    });

    it('renders system fault banner along with historical data when includeHistory is true', async () => {
        const energyHistory = [
            {
                tsHourStart: '2026-09-30T10:00:00Z',
                avgBatterySOC: 80,
                solarKWH: 1.5,
                homeLoadKWH: 0.8,
            },
        ];
        const priceHistory = [
            {
                tsHourStart: '2026-09-30T10:00:00Z',
                dollarsPerKWH: 0.12,
                gridUseDollarsPerKWH: 0.04,
            },
        ];
        (fetchModeling as any).mockResolvedValue({
            latestAction: {
                timestamp: '2026-09-30T12:00:00Z',
                batteryMode: 0,
                solarMode: 0,
                description: '2 alarms present',
                fault: true,
            },
            energyHistory,
            priceHistory,
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByTestId('forecast-fault-alert')).toBeInTheDocument();
        });

        // Toggle include history
        const toggle = screen.getByRole('switch', { name: /Show Previous 24 Hours/i });
        fireEvent.click(toggle);

        await waitFor(() => {
            expect(screen.getByTestId('forecast-fault-alert')).toBeInTheDocument();
            expect(screen.getByText(/2 alarms present/)).toBeInTheDocument();
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });
    });

    it('automatically refreshes when switching away and returning after more than 10 minutes', async () => {
        expect(STALE_FORECAST_TIMEOUT_MS).toBe(10 * 60 * 1000);
        let mockTime = new Date('2026-06-15T12:00:00Z').getTime();
        const dateSpy = vi.spyOn(Date, 'now').mockImplementation(() => mockTime);

        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(1);
        });

        // User switches away
        act(() => {
            Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
            document.dispatchEvent(new Event('visibilitychange'));
        });

        // More than 10 minutes pass
        mockTime += STALE_FORECAST_TIMEOUT_MS + 60 * 1000;

        // User returns
        act(() => {
            Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
            document.dispatchEvent(new Event('visibilitychange'));
        });

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(2);
        });

        dateSpy.mockRestore();
    });

    it('does not refresh when switching away and returning within 10 minutes', async () => {
        let mockTime = new Date('2026-06-15T12:00:00Z').getTime();
        const dateSpy = vi.spyOn(Date, 'now').mockImplementation(() => mockTime);

        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast();

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(1);
        });

        // User switches away
        act(() => {
            Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
            document.dispatchEvent(new Event('visibilitychange'));
        });

        // 5 minutes pass
        mockTime += 5 * 60 * 1000;

        // User returns
        act(() => {
            Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
            document.dispatchEvent(new Event('visibilitychange'));
        });

        // Wait a bit to ensure no refresh call occurs
        await new Promise((resolve) => setTimeout(resolve, 50));
        expect(fetchModeling).toHaveBeenCalledTimes(1);

        dateSpy.mockRestore();
    });

    it('preserves conservative load prediction strategy when refreshing', async () => {
        let mockTime = new Date('2026-06-15T12:00:00Z').getTime();
        const dateSpy = vi.spyOn(Date, 'now').mockImplementation(() => mockTime);

        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        renderForecast({ settings: { homeLoadPredictionStrategy: 'conservative' } });

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(1);
            expect(fetchModeling).toHaveBeenLastCalledWith(undefined, 'conservative');
        });

        // User switches away
        act(() => {
            Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
            document.dispatchEvent(new Event('visibilitychange'));
        });

        // More than 10 minutes pass
        mockTime += STALE_FORECAST_TIMEOUT_MS + 60 * 1000;

        // User returns
        act(() => {
            Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
            document.dispatchEvent(new Event('visibilitychange'));
        });

        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(2);
            expect(fetchModeling).toHaveBeenLastCalledWith(undefined, 'conservative');
        });

        dateSpy.mockRestore();
    });

    it('hides Regenerate Plan button when not on staging site', async () => {
        (fetchModeling as any).mockResolvedValue({
            plan: makeTestPlan(),
            energyHistory: [],
            priceHistory: [],
        });

        renderForecast({ siteID: 'site-1', settings: { release: 'staging' } });

        await waitFor(() => {
            expect(screen.getByText('24-Hour Energy Plan')).toBeInTheDocument();
        });

        expect(screen.queryByRole('button', { name: /Regenerate Plan/i })).not.toBeInTheDocument();
    });

    it('renders Regenerate Plan button on staging site and re-renders with new plan when clicked', async () => {
        const originalLocation = window.location;
        const locationProxy = new Proxy(originalLocation, {
            get(target, prop) {
                if (prop === 'hostname') return 'staging.raterudder.com';
                return (target as any)[prop];
            },
        });
        Object.defineProperty(window, 'location', {
            configurable: true,
            value: locationProxy,
        });

        try {
            const user = userEvent.setup();
            const initialPlan = makeTestPlan();
            const regeneratedPlan: Plan = {
                ...makeTestPlan(),
                totalProjectedCost: 1.23,
                totalExportCredits: 4.56,
            };

            (fetchModeling as any)
                .mockResolvedValueOnce({
                    plan: initialPlan,
                    energyHistory: [],
                    priceHistory: [],
                })
                .mockResolvedValueOnce({
                    plan: regeneratedPlan,
                    energyHistory: [],
                    priceHistory: [],
                });

            renderForecast({ siteID: 'site-1', settings: { release: 'production' } });

            await waitFor(() => {
                expect(screen.getByText('$3.45')).toBeInTheDocument();
            });

            const regenerateBtn = screen.getByRole('button', { name: /Regenerate Plan/i });
            expect(regenerateBtn).toBeInTheDocument();

            await user.click(regenerateBtn);

            await waitFor(() => {
                expect(fetchModeling).toHaveBeenLastCalledWith('site-1', 'default', true);
                expect(screen.getByText('$1.23')).toBeInTheDocument();
                expect(screen.getByText('$4.56')).toBeInTheDocument();
            });
        } finally {
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: originalLocation,
            });
        }
    });

    it('uses backend futurePrices and renders Upcoming Schedule blocks with SOC and rate metadata', async () => {
        const plan = makeTestPlan();
        (fetchModeling as any).mockResolvedValue({
            plan,
            energyHistory: [],
            priceHistory: [],
            futurePrices: [
                {
                    tsHourStart: '2026-02-11T14:00:00Z',
                    dollarsPerKWH: 0.09,
                    gridUseDollarsPerKWH: 0,
                    exportDollarsPerKWH: 0.04,
                },
                {
                    tsHourStart: '2026-02-11T15:00:00Z',
                    dollarsPerKWH: 0.38,
                    gridUseDollarsPerKWH: 0,
                    exportDollarsPerKWH: 0.12,
                },
            ],
        });

        renderForecast();

        await waitFor(() => {
            expect(screen.getByTestId('plan-schedule-beats')).toBeInTheDocument();
        });

        expect(screen.getByText('Charging before peak')).toBeInTheDocument();
        expect(screen.getByTestId('rate-low-badge')).toHaveTextContent('Low: $0.09/kWh');
        expect(screen.getByTestId('rate-peak-badge')).toHaveTextContent('Peak: $0.38/kWh');
    });
});

