import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Router } from 'wouter';
import Forecast from './Forecast';
import * as api from '../api';
import { setupDefaultApiMocks } from '../test/apiMocks';
import type { ModelingHour, Plan, PlanPeriod } from '../api';
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
            expect(screen.getByText('Predicted Home Load (kWh)')).toBeInTheDocument();
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
            expect(screen.getByText('Predicted Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
        });
    });

    it('allows toggling between Default and Conservative home load prediction strategy', async () => {
        const user = userEvent.setup();
        const data = makeSimHours();
        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast();

        // Check toggle switch is present
        const toggle = await screen.findByRole('switch', { name: /Conservative/i });
        expect(toggle).toBeInTheDocument();
        expect(toggle).not.toBeChecked();

        // Toggle to conservative
        await user.click(toggle);

        // Verify fetchModeling was called with override 'conservative'
        await waitFor(() => {
            expect(fetchModeling).toHaveBeenLastCalledWith(undefined, 'conservative');
        });
    });

    it('defaults the toggle to checked if settings strategy is conservative', async () => {
        const data = makeSimHours();
        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast({ settings: { homeLoadPredictionStrategy: 'conservative' } });

        // Check toggle switch is present and checked
        const toggle = await screen.findByRole('switch', { name: /Conservative/i });
        expect(toggle).toBeChecked();
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

    it('does not make duplicate fetchModeling calls on initial load or toggle', async () => {
        const user = userEvent.setup();
        const data = makeSimHours();
        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: []
        });

        renderForecast({ settings: { homeLoadPredictionStrategy: 'conservative' } });

        // 1. Initial load should happen once. Wait for the loading screen to disappear.
        await waitFor(() => {
            expect(screen.getByText('Battery (if used) (%)')).toBeInTheDocument();
        });

        // The initial request to modeling should be exactly 1, and fetchSettings should not be called.
        expect(fetchSettings).not.toHaveBeenCalled();
        expect(fetchModeling).toHaveBeenCalledTimes(1);
        expect(fetchModeling).toHaveBeenLastCalledWith(undefined, 'conservative');

        // Check toggle switch is checked by default
        const toggle = screen.getByRole('switch', { name: /Conservative/i });
        expect(toggle).toBeChecked();

        // 2. Toggle to default strategy
        await user.click(toggle);

        // Fetch modeling should be called a second time (total 2 times) with 'default'.
        await waitFor(() => {
            expect(fetchModeling).toHaveBeenCalledTimes(2);
            expect(fetchModeling).toHaveBeenLastCalledWith(undefined, 'default');
        });
    });

    it('opens help dialog when help button on a forecast chart is clicked', async () => {
        const data = makeSimHours();
        (fetchModeling as any).mockResolvedValue({
            simulation: data,
            energyHistory: [],
            priceHistory: [],
            weather: [],
        });

        const user = userEvent.setup();
        renderForecast();

        await waitFor(() => {
            expect(screen.getByText('Battery (if used) (%)')).toBeInTheDocument();
        });

        const helpBtns = screen.getAllByRole('button', { name: /more info/i });
        const helpBtn = helpBtns[0];
        expect(helpBtn).toBeInTheDocument();

        await user.click(helpBtn);

        expect(await screen.findByRole('dialog')).toBeInTheDocument();
        expect(screen.getByText(/if RateRudder did nothing to optimize your system/i)).toBeInTheDocument();
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
            expect(screen.getByText('Planned Battery SOC (%)')).toBeInTheDocument();
            expect(screen.queryByText('Projected Benefit')).not.toBeInTheDocument();
            expect(screen.getByText('Projected Grid Cost')).toBeInTheDocument();
            expect(screen.getByText('$3.45')).toBeInTheDocument();
            expect(screen.getByText('Projected Export Credits')).toBeInTheDocument();
            expect(screen.getByText('$0.60')).toBeInTheDocument();
            expect(screen.getByText('Predicted Solar (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Predicted Home Load (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Grid Charge Cost ($/kWh)')).toBeInTheDocument();
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
        expect(screen.queryByText('Peak Discharge')).not.toBeInTheDocument();

        // Reserve reference line is rendered on chart
        expect(screen.getByText('Reserve')).toBeInTheDocument();
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
            expect(screen.getByText('Predicted Solar (kWh)')).toBeInTheDocument();
            expect(screen.getByText('Predicted Home Load (kWh)')).toBeInTheDocument();
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
});

