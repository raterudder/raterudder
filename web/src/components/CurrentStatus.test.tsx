import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import CurrentStatus from './CurrentStatus';
import { BatteryMode, SolarMode, type Action } from '../api';

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
        expect(screen.getByText('Use Battery')).toBeInTheDocument();
    });

    it('renders charging state when batteryPower is positive', () => {
        const action: Action = {
            ...defaultAction,
            batteryMode: BatteryMode.ChargeAny,
            systemStatus: {
                ...defaultAction.systemStatus!,
                batteryPower: 2000
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
        expect(screen.getByText('Capacity in 5 hours')).toBeInTheDocument();

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
        expect(screen.getByText('Deficit in 45 minutes')).toBeInTheDocument();

        vi.useRealTimers();
    });

    it('renders sooner of capacityAt or deficitAt when both are in the future', () => {
        const mockNow = new Date('2026-06-15T12:00:00Z');
        vi.useFakeTimers();
        vi.setSystemTime(mockNow);

        const action: Action = {
            ...defaultAction,
            capacityAt: '2026-06-15T14:00:00Z',
            deficitAt: '2026-06-15T15:00:00Z',
        };
        render(<CurrentStatus action={action} />);
        expect(screen.getByText('Capacity in 2 hours')).toBeInTheDocument();

        vi.useRealTimers();
    });
});
