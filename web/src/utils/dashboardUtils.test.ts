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

        it('handles SufficientBatteryTillCharge with prices and savings', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.SufficientBatteryTillCharge,
                deficitAt: '2026-05-20T19:24:00-05:00',
                currentPrice: { dollarsPerKWH: 0.15, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('If discharged, the battery would deplete');
            expect(text).toContain('cheaper charging window is coming up');
            expect(text).toContain('$ 0.050');
            expect(text).toContain('$ 0.150');
            expect(text).toContain('savings: $ 0.100/kWh.');
        });

        it('handles SufficientBatteryTillCharge with identical prices or less than 1 cent margin', () => {
            const action1 = {
                ...baseAction,
                reason: ActionReason.SufficientBatteryTillCharge,
                deficitAt: '2026-05-20T19:24:00-05:00',
                currentPrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text1 = getReasonText(action1);
            expect(text1).toContain('charging window with the same price is coming up');
            expect(text1).toContain('waiting to refill it during that window');
            expect(text1).not.toContain('savings:');

            const action2 = {
                ...baseAction,
                reason: ActionReason.SufficientBatteryTillCharge,
                deficitAt: '2026-05-20T19:24:00-05:00',
                currentPrice: { dollarsPerKWH: 0.060, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text2 = getReasonText(action2);
            expect(text2).toContain('charging window with the same price is coming up');
            expect(text2).toContain('waiting to refill it during that window');
            expect(text2).not.toContain('savings:');
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
            expect(text).toContain('savings: $ 0.400/kWh.');
        });

        it('handles DeficitCharge with identical prices or less than 1 cent margin', () => {
            const action1 = {
                ...baseAction,
                reason: ActionReason.DeficitCharge,
                currentPrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text1 = getReasonText(action1);
            expect(text1).toContain('same price as');
            expect(text1).not.toContain('cheaper than');
            expect(text1).not.toContain('savings:');

            const action2 = {
                ...baseAction,
                reason: ActionReason.DeficitCharge,
                currentPrice: { dollarsPerKWH: 0.055, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.060, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text2 = getReasonText(action2);
            expect(text2).toContain('same price as');
            expect(text2).not.toContain('cheaper than');
            expect(text2).not.toContain('savings:');
        });

        it('handles PreventSolarCurtailment', () => {
            const action = { ...baseAction, reason: ActionReason.PreventSolarCurtailment };
            expect(getReasonText(action)).toContain('exceed battery capacity');
        });

        it('handles GridUnavailable', () => {
            const action = { ...baseAction, reason: ActionReason.GridUnavailable };
            expect(getReasonText(action)).toContain('Grid is currently unavailable');
        });

        it('handles DeficitSaveForPeak with valid deficitAt', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '2026-06-16T08:33:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('If discharged, the battery would deplete around');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
        });

        it('handles DeficitSaveForPeak falling back to hitAboveDeficitAt when deficitAt is zero', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '0001-01-01T00:00:00Z',
                hitAboveDeficitAt: '2026-06-16T08:33:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('If discharged, the battery would deplete around');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
        });

        it('handles DeficitSaveForPeak with no depletion message when all deficit times are zero', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '0001-01-01T00:00:00Z',
                hitBelowDeficitAt: '0001-01-01T00:00:00Z',
                hitAboveDeficitAt: '0001-01-01T00:00:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).not.toContain('If discharged, the battery would deplete');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
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
            expect(text).toContain('savings: $ 0.100/kWh.');
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
                reason: ActionReason.ArbitrageChargeSave,
                solarMode: SolarMode.NoExport,
                currentPrice: { dollarsPerKWH: -0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            expect(getReasonText(action)).toContain('Disabled solar export');
        });
    });
});
