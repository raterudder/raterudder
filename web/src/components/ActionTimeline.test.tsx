import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import ActionTimeline from './ActionTimeline';
import { BatteryMode, SolarMode, ActionReason, type Action } from '../api';
import { type ActionSummary } from '../utils/dashboardUtils';

describe('ActionTimeline', () => {
    it('renders regular action items', () => {
        const actions: Action[] = [{
            timestamp: new Date().toISOString(),
            batteryMode: BatteryMode.Standby,
            solarMode: SolarMode.NoExport,
            description: 'Test action'
        }];
        render(<ActionTimeline groupedActions={actions} />);
        expect(screen.getAllByText('Hold Battery').length).toBeGreaterThan(0);
        expect(screen.getByText('Test action')).toBeInTheDocument();
    });

    it('renders action summaries with time range if start and end times differ', () => {
        const startTime = new Date('2026-06-25T12:00:00Z');
        const endTime = new Date('2026-06-25T12:30:00Z');
        const summary: ActionSummary = {
            isSummary: true,
            type: 'no_change',
            startTime: startTime.toISOString(),
            endTime: endTime.toISOString(),
            latestAction: {
                timestamp: endTime.toISOString(),
                batteryMode: BatteryMode.NoChange,
                solarMode: SolarMode.NoChange,
                reason: ActionReason.SufficientBattery
            } as Action,
            count: 5,
            alarms: new Set(),
            storms: new Set(),
            hasPrice: false,
            hasSOC: false,
            avgPrice: 0,
            min: 0,
            max: 0,
            avgSOC: 0,
            minSOC: 0,
            maxSOC: 0
        };
        render(<ActionTimeline groupedActions={[summary]} />);
        expect(screen.getByText('No Change')).toBeInTheDocument();
        expect(screen.getByText(/battery has enough stored energy/)).toBeInTheDocument();

        const startFormatted = startTime.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
        const endFormatted = endTime.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
        expect(screen.getByText(startFormatted)).toBeInTheDocument();
        expect(screen.getByText(endFormatted)).toBeInTheDocument();
    });

    it('renders VPP Prep action items correctly', () => {
        const actions: Action[] = [{
            timestamp: new Date().toISOString(),
            batteryMode: BatteryMode.ChargeAny,
            solarMode: SolarMode.NoExport,
            reason: ActionReason.VPPPrep,
            description: 'Upcoming VPP Event prep charging'
        }];
        const { container } = render(<ActionTimeline groupedActions={actions} />);
        expect(screen.getByText('VPP Pre-Charging')).toBeInTheDocument();
        expect(screen.getByText(/Virtual Power Plant/)).toBeInTheDocument();
        const li = container.querySelector('li');
        expect(li).toHaveClass('mode-charge_any');
    });

    it('renders VPP Active action items correctly', () => {
        const actions: Action[] = [{
            timestamp: new Date().toISOString(),
            batteryMode: BatteryMode.Standby,
            solarMode: SolarMode.NoExport,
            reason: ActionReason.VPPActive,
            description: 'Active VPP Event'
        }];
        const { container } = render(<ActionTimeline groupedActions={actions} />);
        expect(screen.getByText('VPP Event Active')).toBeInTheDocument();
        expect(screen.getByText(/Virtual Power Plant/)).toBeInTheDocument();
        const li = container.querySelector('li');
        expect(li).toHaveClass('mode-vpp');
    });

    it('renders Standby for Similar Price action items correctly', () => {
        const actions: Action[] = [{
            timestamp: new Date().toISOString(),
            batteryMode: BatteryMode.Standby,
            solarMode: SolarMode.Any,
            reason: ActionReason.HoldSimilarPrice,
            description: 'Holding battery for solar export',
            currentPrice: { dollarsPerKWH: 0.21, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
        }];
        const { container } = render(<ActionTimeline groupedActions={actions} />);
        expect(screen.getByText('Standby for Similar Price')).toBeInTheDocument();
        expect(screen.getByText(/comparable to the expected export credit/)).toBeInTheDocument();
        const li = container.querySelector('li');
        expect(li).toHaveClass('mode-standby');
    });

    it('renders DirectExport action items for battery export, solar export, and standby', () => {
        const actions: Action[] = [
            {
                timestamp: new Date().toISOString(),
                batteryMode: BatteryMode.Export,
                solarMode: SolarMode.Export,
                reason: ActionReason.DirectExport,
                description: 'Battery & Solar Grid Export',
                currentPrice: { dollarsPerKWH: 0.50, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            },
            {
                timestamp: new Date().toISOString(),
                batteryMode: BatteryMode.Export,
                solarMode: SolarMode.NoExport,
                reason: ActionReason.DirectExport,
                description: 'Direct Battery Export',
                currentPrice: { dollarsPerKWH: 0.50, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            },
            {
                timestamp: new Date().toISOString(),
                batteryMode: BatteryMode.Standby,
                solarMode: SolarMode.Export,
                reason: ActionReason.DirectExport,
                description: 'Peak Defense Standby',
                currentPrice: { dollarsPerKWH: 0.40, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            },
            {
                timestamp: new Date().toISOString(),
                batteryMode: BatteryMode.Load,
                solarMode: SolarMode.Export,
                reason: ActionReason.DirectExport,
                description: 'Direct Solar Export',
                currentPrice: { dollarsPerKWH: 0.35, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            }
        ];
        render(<ActionTimeline groupedActions={actions} />);
        expect(screen.getByText('Battery & Solar Grid Export')).toBeInTheDocument();
        expect(screen.getByText('Direct Battery Export')).toBeInTheDocument();
        expect(screen.getByText('Peak Defense Standby')).toBeInTheDocument();
        expect(screen.getByText('Direct Solar Export')).toBeInTheDocument();
    });

    it('suppresses Full tag for DirectExport actions even when capacityAt is set', () => {
        const actions: Action[] = [
            {
                timestamp: new Date().toISOString(),
                batteryMode: BatteryMode.Export,
                solarMode: SolarMode.Export,
                reason: ActionReason.DirectExport,
                capacityAt: new Date(Date.now() + 3600000).toISOString(),
                description: 'Direct Export'
            }
        ];
        render(<ActionTimeline groupedActions={actions} />);
        expect(screen.queryByText(/Full:/)).not.toBeInTheDocument();
    });

    it('renders price and summary range with custom currency symbol', () => {
        const summary: ActionSummary = {
            isSummary: true,
            type: 'grouped',
            startTime: new Date('2026-06-25T12:00:00Z').toISOString(),
            latestAction: {
                timestamp: new Date('2026-06-25T12:00:00Z').toISOString(),
                batteryMode: BatteryMode.Standby,
                solarMode: SolarMode.NoExport,
                reason: ActionReason.SufficientBattery
            } as Action,
            count: 3,
            alarms: new Set(),
            storms: new Set(),
            hasPrice: true,
            hasSOC: false,
            avgPrice: 0.15,
            min: 0.10,
            max: 0.20,
            avgSOC: 0,
            minSOC: 0,
            maxSOC: 0
        };
        render(<ActionTimeline groupedActions={[summary]} currencySymbol="£" />);
        expect(screen.getByText('Avg Price:')).toBeInTheDocument();
        expect(screen.getByText(/£ 0\.150\/kWh/)).toBeInTheDocument();
        expect(screen.getByText(/\(Range: £ 0\.100 - £ 0\.200\)/)).toBeInTheDocument();
    });
});

