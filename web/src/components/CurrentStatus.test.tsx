import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import CurrentStatus from './CurrentStatus';
import { BatteryMode, SolarMode, ActionReason, type Action } from '../api';

describe('CurrentStatus', () => {
    const defaultAction: Action = {
        timestamp: new Date().toISOString(),
        batteryMode: BatteryMode.Standby,
        solarMode: SolarMode.NoExport,
        description: '',
        systemStatus: {
            batterySOC: 45.5,
            batteryPower: 0,
            solarPower: 0,
            gridPower: 0,
            loadPower: 0
        }
    };

    it('renders battery SOC and mode', () => {
        render(<CurrentStatus action={defaultAction} />);
        expect(screen.getByText('45.5%')).toBeInTheDocument();
        expect(screen.getByText('Hold Battery')).toBeInTheDocument();
    });

    it('uses targetBatteryMode if batteryMode is NoChange', () => {
        const action: Action = {
            ...defaultAction,
            batteryMode: BatteryMode.NoChange,
            targetBatteryMode: BatteryMode.Load
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Self-Powered')).toBeInTheDocument();
        expect(screen.getByText('Rely on Solar & Battery')).toBeInTheDocument();
    });

    it('renders charging state when batteryMode is ChargeAny', () => {
        const action: Action = {
            ...defaultAction,
            batteryMode: BatteryMode.ChargeAny,
            systemStatus: {
                ...defaultAction.systemStatus!,
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText(/System Charging/i)).toBeInTheDocument();
    });

    it('does not render price when currentPrice is missing', () => {
        render(<CurrentStatus action={defaultAction} />);
        expect(screen.queryByText('Price')).not.toBeInTheDocument();
    });

    it('renders total price including gridUseDollarsPerKWH when currentPrice is present', () => {
        const action: Action = {
            ...defaultAction,
            currentPrice: {
                tsStart: '',
                tsEnd: '',
                dollarsPerKWH: 0.15,
                gridUseDollarsPerKWH: 0.05
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Price')).toBeInTheDocument();
        expect(screen.getByText('$ 0.200')).toBeInTheDocument();
    });

    it('renders time until capacity when capacityAt is set in the future', () => {
        const mockNow = new Date('2026-06-15T12:00:00Z');
        vi.useFakeTimers();
        vi.setSystemTime(mockNow);

        const action: Action = {
            ...defaultAction,
            capacityAt: '2026-06-15T17:00:00Z',
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Battery full in 5 hours')).toBeInTheDocument();

        vi.useRealTimers();
    });

    it('renders time until deficit when deficitAt is set in the future', () => {
        const mockNow = new Date('2026-06-15T12:00:00Z');
        vi.useFakeTimers();
        vi.setSystemTime(mockNow);

        const action: Action = {
            ...defaultAction,
            deficitAt: '2026-06-15T12:45:00Z',
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Battery empty in 45 minutes')).toBeInTheDocument();

        vi.useRealTimers();
    });

    it('prefers deficitAt over capacityAt when both are in the future', () => {
        const mockNow = new Date('2026-06-15T12:00:00Z');
        vi.useFakeTimers();
        vi.setSystemTime(mockNow);

        const action: Action = {
            ...defaultAction,
            capacityAt: '2026-06-15T14:00:00Z',
            deficitAt: '2026-06-15T15:00:00Z',
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Battery empty in 3 hours')).toBeInTheDocument();

        vi.useRealTimers();
    });

    it('renders battery at reserve status correctly', () => {
        const action: Action = {
            ...defaultAction,
            reason: ActionReason.BatteryAtReserve,
            batteryMode: BatteryMode.Load,
            systemStatus: {
                ...defaultAction.systemStatus!,
                batterySOC: 20.0
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Battery At Reserve')).toBeInTheDocument();
        expect(screen.getByText('Holding Reserve')).toBeInTheDocument();
        expect(screen.getByText('🔋')).toBeInTheDocument();
    });

    it('renders Direct Solar Export status correctly', () => {
        const action: Action = {
            ...defaultAction,
            reason: ActionReason.DirectExport,
            batteryMode: BatteryMode.Load,
            solarMode: SolarMode.Export,
            systemStatus: {
                ...defaultAction.systemStatus!,
                batterySOC: 60.0
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Direct Solar Export')).toBeInTheDocument();
        expect(screen.getByText('Exporting Solar • Home on Battery')).toBeInTheDocument();
        expect(screen.getByText('☀️')).toBeInTheDocument();
    });

    it('renders Peak Defense Standby status correctly', () => {
        const action: Action = {
            ...defaultAction,
            reason: ActionReason.DirectExport,
            batteryMode: BatteryMode.Standby,
            solarMode: SolarMode.Export,
            systemStatus: {
                ...defaultAction.systemStatus!,
                batterySOC: 25.0
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Peak Defense')).toBeInTheDocument();
        expect(screen.getByText('Holding Reserve • Home on Solar')).toBeInTheDocument();
        expect(screen.getByText('🛡️')).toBeInTheDocument();
    });

    it('does not render price when currentPrice has uninitialized 0001-01-01 timestamp', () => {
        const action: Action = {
            ...defaultAction,
            currentPrice: {
                tsStart: '0001-01-01T00:00:00Z',
                tsEnd: '0001-01-01T00:00:00Z',
                dollarsPerKWH: 0,
                gridUseDollarsPerKWH: 0
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.queryByText('Price')).not.toBeInTheDocument();
    });

    it('renders -- when batterySOC is missing/undefined', () => {
        const action: Action = {
            ...defaultAction,
            systemStatus: {
                batteryPower: 0,
                solarPower: 0,
                gridPower: 0,
                loadPower: 0
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('--')).toBeInTheDocument();
        expect(screen.queryByText('0.0%')).not.toBeInTheDocument();
    });

    it('suppresses Battery full text when action is DirectExport even with capacityAt set', () => {
        const mockNow = new Date('2026-06-15T12:00:00Z');
        vi.useFakeTimers();
        vi.setSystemTime(mockNow);

        const actionExport: Action = {
            ...defaultAction,
            reason: ActionReason.DirectExport,
            batteryMode: BatteryMode.Load,
            capacityAt: '2026-06-15T17:00:00Z',
        };
        const { rerender } = render(<CurrentStatus action={actionExport} />);
        expect(screen.queryByText(/Battery full in/)).not.toBeInTheDocument();

        const actionStandby: Action = {
            ...defaultAction,
            reason: ActionReason.DirectExport,
            batteryMode: BatteryMode.Standby,
            capacityAt: '2026-06-15T17:00:00Z',
        };
        rerender(<CurrentStatus action={actionStandby} />);
        expect(screen.queryByText(/Battery full in/)).not.toBeInTheDocument();

        vi.useRealTimers();
    });

    it('renders plan-aware subvalue when plan is in standby waiting for peak', () => {
        const action: Action = {
            ...defaultAction,
            batteryMode: BatteryMode.Standby,
            reason: ActionReason.DeficitSaveForPeak,
            plan: {
                generatedAt: '2026-06-15T12:00:00Z',
                horizonHours: 24,
                totalProjectedCost: 1.0,
                totalExportCredits: 0,
                netEconomicBenefit: 2.0,
                periods: [
                    {
                        startTime: '2026-06-15T12:00:00Z',
                        endTime: '2026-06-15T16:00:00Z',
                        durationHours: 4,
                        batteryMode: BatteryMode.Standby,
                        solarMode: SolarMode.Any,
                        reason: ActionReason.DeficitSaveForPeak,
                        description: 'Standby for peak',
                        startSoc: 80,
                        endSoc: 80,
                        price: { tsStart: '2026-06-15T12:00:00Z', tsEnd: '2026-06-15T16:00:00Z', dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0 },
                        gridImportKWH: 0,
                        gridExportKWH: 0,
                        costDollars: 0,
                    },
                    {
                        startTime: '2026-06-15T16:00:00Z',
                        endTime: '2026-06-15T21:00:00Z',
                        durationHours: 5,
                        batteryMode: BatteryMode.Load,
                        solarMode: SolarMode.Any,
                        reason: ActionReason.DischargeAtPeak,
                        description: 'Discharge at peak',
                        startSoc: 80,
                        endSoc: 20,
                        price: { tsStart: '2026-06-15T16:00:00Z', tsEnd: '2026-06-15T21:00:00Z', dollarsPerKWH: 0.38, gridUseDollarsPerKWH: 0 },
                        gridImportKWH: 0,
                        gridExportKWH: 0,
                        costDollars: 0,
                    }
                ]
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText(/Saving reserve for/)).toBeInTheDocument();
        expect(screen.getByText(/peak rate/)).toBeInTheDocument();
    });

    it('renders plan-aware subvalue when plan is charging', () => {
        const action: Action = {
            ...defaultAction,
            batteryMode: BatteryMode.ChargeAny,
            currentPrice: { tsStart: '2026-06-15T01:00:00Z', tsEnd: '2026-06-15T04:00:00Z', dollarsPerKWH: 0.02, gridUseDollarsPerKWH: 0.01 },
            plan: {
                generatedAt: '2026-06-15T01:00:00Z',
                horizonHours: 24,
                totalProjectedCost: 1.0,
                totalExportCredits: 0,
                netEconomicBenefit: 2.0,
                periods: [
                    {
                        startTime: '2026-06-15T01:00:00Z',
                        endTime: '2026-06-15T04:00:00Z',
                        durationHours: 3,
                        batteryMode: BatteryMode.ChargeAny,
                        solarMode: SolarMode.Any,
                        reason: ActionReason.DeficitCharge,
                        description: 'Grid charging',
                        startSoc: 20,
                        endSoc: 100,
                        price: { tsStart: '2026-06-15T01:00:00Z', tsEnd: '2026-06-15T04:00:00Z', dollarsPerKWH: 0.02, gridUseDollarsPerKWH: 0.01 },
                        gridImportKWH: 8,
                        gridExportKWH: 0,
                        costDollars: 0.24,
                    }
                ]
            }
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText(/Charging to 100% until/)).toBeInTheDocument();
        expect(screen.getByText(/Low rate/)).toBeInTheDocument();
    });
});
