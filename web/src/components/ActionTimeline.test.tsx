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
        expect(screen.getByText('(5x)')).toBeInTheDocument();
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
});

