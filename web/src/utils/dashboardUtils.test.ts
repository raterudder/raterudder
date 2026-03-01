import { describe, it, expect } from 'vitest';
import {
    getBatteryModeLabel,
    formatPrice,
    formatCurrency,
    gridChargeCost,
    getReasonText
} from './dashboardUtils';
import { BatteryMode, SolarMode, ActionReason, type Action } from '../api';

describe('dashboardUtils', () => {
    describe('getBatteryModeLabel', () => {
        it('returns correct label for standby', () => {
            expect(getBatteryModeLabel(BatteryMode.Standby)).toBe('Hold Battery');
        });
        it('returns Unknown for invalid mode', () => {
            expect(getBatteryModeLabel(999)).toBe('Unknown');
        });
    });

    describe('formatPrice', () => {
        it('formats dollars to price string', () => {
            expect(formatPrice(0.1234)).toBe('$ 0.123/kWh');
        });
    });

    describe('formatCurrency', () => {
        it('formats positive amount', () => {
            expect(formatCurrency(10.5)).toBe('$ 10.50');
        });
        it('formats negative amount', () => {
            expect(formatCurrency(-5.25)).toBe('- $ 5.25');
        });
        it('formats with forceSign', () => {
            expect(formatCurrency(3.21, true)).toBe('+ $ 3.21');
        });
    });

    describe('gridChargeCost', () => {
        it('sums base price and grid use adder', () => {
            expect(gridChargeCost({ dollarsPerKWH: 0.1, gridUseDollarsPerKWH: 0.05 })).toBeCloseTo(0.15);
        });
        it('handles missing grid use adder', () => {
            expect(gridChargeCost({ dollarsPerKWH: 0.2 })).toBeCloseTo(0.2);
        });
    });

    describe('getReasonText', () => {
        const baseAction: Action = {
            description: 'Fallback',
            timestamp: '',
            batteryMode: BatteryMode.Standby,
            solarMode: SolarMode.NoExport
        };

        it('returns description if no reason is present', () => {
            expect(getReasonText(baseAction)).toBe('Fallback');
        });

        it('handles SufficientBattery', () => {
             const action = { ...baseAction, reason: ActionReason.SufficientBattery };
             expect(getReasonText(action)).toContain('battery has enough stored energy');
        });

        it('handles DeficitCharge with prices and savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitCharge,
                currentPrice: { dollarsPerKWH: 0.1, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.5, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('Charging now');
            expect(text).toContain('$ 0.100');
            expect(text).toContain('savings: $ 0.400/kWh');
        });

        it('handles PreventSolarCurtailment', () => {
            const action = { ...baseAction, reason: ActionReason.PreventSolarCurtailment };
            expect(getReasonText(action)).toContain('exceed battery capacity');
        });


        it('handles WaitingToCharge with significant savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.WaitingToCharge,
                currentPrice: { dollarsPerKWH: 0.15, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('A cheaper charging window is coming up');
            expect(text).toContain('savings: $ 0.100/kWh');
        });

        it('handles WaitingToCharge with < $0.01 savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.WaitingToCharge,
                currentPrice: { dollarsPerKWH: 0.092, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.091, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('A charging window is coming up which is similar in price or cheaper than now');
            expect(text).not.toContain('savings:');
            expect(text).not.toContain('$ 0.091');
            expect(text).not.toContain('$ 0.092');
        });

        it('appends NoExport suffix for arbitrage', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.ArbitrageCharge,
                solarMode: SolarMode.NoExport,
                currentPrice: { dollarsPerKWH: -0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            expect(getReasonText(action)).toContain('Disabled solar export');
        });
    });
});
