import { describe, it, expect } from 'vitest';
import {
    getBatteryModeLabel,
    getBatteryPillLabel,
    getActionTitle,
    formatSOCRange,
    hasSolarActivity,
    buildDaySummary,
    formatPrice,
    formatCurrency,
    gridChargeCost,
    getReasonText,
    formatTime,
    extractOffsetMinutes,
    formatTimeInOffset,
    getActionTimestamp,
    isZeroTime,
    formatHour12,
    getCurrencySymbol,
    getPlanStatusSubvalue,
    type ActionSummary
} from './dashboardUtils';
import { BatteryMode, SolarMode, ActionReason, type Action } from '../api';

describe('dashboardUtils', () => {
    describe('getCurrencySymbol', () => {
        it('returns £ for octopus', () => {
            expect(getCurrencySymbol('octopus')).toBe('£');
        });
        it('returns $ for comed, tesla, empty, or undefined', () => {
            expect(getCurrencySymbol('comed')).toBe('$');
            expect(getCurrencySymbol('')).toBe('$');
            expect(getCurrencySymbol(null)).toBe('$');
            expect(getCurrencySymbol(undefined)).toBe('$');
        });
    });

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
        it('formats with custom currency symbol', () => {
            expect(formatPrice(0.1234, '£')).toBe('£ 0.123/kWh');
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
        it('formats with custom currency symbol', () => {
            expect(formatCurrency(10.5, false, '£')).toBe('£ 10.50');
            expect(formatCurrency(-5.25, false, '£')).toBe('- £ 5.25');
            expect(formatCurrency(3.21, true, '£')).toBe('+ £ 3.21');
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
            expect(text).toContain('If we rely on the battery, it would deplete');
            expect(text).toContain('cheaper charging window is coming up');
            expect(text).toContain('$ 0.050');
            expect(text).toContain('$ 0.150');
            expect(text).toContain('savings: $ 0.100/kWh.');
        });

        it('handles SufficientBatteryTillCharge with custom currency symbol', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.SufficientBatteryTillCharge,
                deficitAt: '2026-05-20T19:24:00-05:00',
                currentPrice: { dollarsPerKWH: 0.15, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action, '£');
            expect(text).toContain('£ 0.050');
            expect(text).toContain('£ 0.150');
            expect(text).toContain('savings: £ 0.100/kWh.');
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

        it('handles HoldSimilarPrice', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.HoldSimilarPrice,
                currentPrice: { dollarsPerKWH: 0.21, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('Current electricity price ($ 0.210/kWh) is comparable to the expected export credit');
            expect(text).toContain('export surplus solar to the grid');
        });

        it('handles GridUnavailable', () => {
            const action = { ...baseAction, reason: ActionReason.GridUnavailable };
            expect(getReasonText(action)).toContain('Grid is currently unavailable');
        });

        it('handles VPPActive', () => {
            const action = { ...baseAction, reason: ActionReason.VPPActive };
            expect(getReasonText(action)).toContain('Virtual Power Plant (VPP) event is currently active');
        });

        it('handles VPPPrep', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.VPPPrep,
                currentPrice: { dollarsPerKWH: 0.08, gridUseDollarsPerKWH: 0.02, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.25, gridUseDollarsPerKWH: 0.05, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('Preparing for upcoming Virtual Power Plant (VPP) event.');
            expect(text).toContain('Charging the battery now at $ 0.100/kWh is cheaper than charging later before the event at $ 0.300/kWh');
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
            expect(text).toContain('If we rely on the battery, it would deplete around');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
        });

        it('handles DeficitSaveForPeak falling back to hitBufferedDeficitAt when deficitAt is zero', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '0001-01-01T00:00:00Z',
                hitBufferedDeficitAt: '2026-06-16T08:33:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).toContain('If we rely on the battery, it would deplete around');
            expect(text).toContain('Since electricity prices now ($ 0.050/kWh) are cheap');
        });

        it('handles DeficitSaveForPeak with no depletion message when all deficit times are zero', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.DeficitSaveForPeak,
                deficitAt: '0001-01-01T00:00:00Z',
                hitThresholdDeficitAt: '0001-01-01T00:00:00Z',
                hitBufferedDeficitAt: '0001-01-01T00:00:00Z',
                currentPrice: { dollarsPerKWH: 0.05, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
                futurePrice: { dollarsPerKWH: 0.10, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' }
            };
            const text = getReasonText(action);
            expect(text).not.toContain('If we rely on the battery, it would deplete');
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

        it('explains EV charging standby correctly', () => {
            const action = {
                ...baseAction,
                reason: ActionReason.EVChargingStandby,
            };
            const text = getReasonText(action);
            expect(text).toContain('EV charging detected');
            expect(text).toContain('preserving battery reserves');
        });

        it('explains DirectExport correctly', () => {
            const actionSolar = {
                ...baseAction,
                batteryMode: BatteryMode.Load,
                solarMode: SolarMode.Export,
                reason: ActionReason.DirectExport,
                currentPrice: { dollarsPerKWH: 0.35, gridUseDollarsPerKWH: 0.05, tsStart: '', tsEnd: '' },
            };
            const textSolar = getReasonText(actionSolar);
            expect(textSolar).toContain('High solar export credit window active');
            expect(textSolar).toContain('Powering the home from the battery');
            expect(textSolar).toContain('$ 0.400/kWh');

            const actionStandby = {
                ...baseAction,
                batteryMode: BatteryMode.Standby,
                reason: ActionReason.DirectExport,
                currentPrice: { dollarsPerKWH: 0.35, gridUseDollarsPerKWH: 0.05, tsStart: '', tsEnd: '' },
            };
            const textStandby = getReasonText(actionStandby);
            expect(textStandby).toContain('High electricity price window active');
            expect(textStandby).toContain('Holding remaining battery energy in standby');
            expect(textStandby).toContain('defend against peak grid imports');

            const actionBatteryExport = {
                ...baseAction,
                batteryMode: BatteryMode.Export,
                solarMode: SolarMode.Export,
                reason: ActionReason.DirectExport,
                currentPrice: { dollarsPerKWH: 0.50, gridUseDollarsPerKWH: 0, tsStart: '', tsEnd: '' },
            };
            const textBatteryExport = getReasonText(actionBatteryExport);
            expect(textBatteryExport).toContain('Discharging battery and exporting solar to the grid');
        });

        it('handles BatteryAtReserve with and without abnormal usage', () => {
            const standardReserve = {
                ...baseAction,
                reason: ActionReason.BatteryAtReserve,
                batteryMode: BatteryMode.Load,
            };
            expect(getReasonText(standardReserve)).toContain('Using remaining energy because standby is not meaningful');

            const normalReserveWithUsage = {
                ...baseAction,
                reason: ActionReason.BatteryAtReserve,
                batteryMode: BatteryMode.Load,
                recentHomeUsageAbnormal: false,
                recentHomeUsageKWH: 2.5,
                q3HomeUsageKWH: 2.0,
            };
            expect(getReasonText(normalReserveWithUsage)).toContain('Using remaining energy because standby is not meaningful');

            const abnormalReserve = {
                ...baseAction,
                reason: ActionReason.BatteryAtReserve,
                batteryMode: BatteryMode.Load,
                recentHomeUsageAbnormal: true,
                recentHomeUsageKWH: 7.5,
                q3HomeUsageKWH: 2.7,
            };
            const abnormalText = getReasonText(abnormalReserve);
            expect(abnormalText).toContain('Recent home usage (7.5 kWh) was well above your normal usage (2.7 kWh)');
            expect(abnormalText).toContain('depleting the reserve buffer');
        });
    });

    describe('formatTime & offset helpers', () => {
        it('extracts offset minutes correctly', () => {
            expect(extractOffsetMinutes('2026-07-21T20:39:04-05:00')).toBe(-300);
            expect(extractOffsetMinutes('2026-07-21T20:39:04+02:00')).toBe(120);
            expect(extractOffsetMinutes('2026-07-21T20:39:04Z')).toBeNull();
            expect(extractOffsetMinutes('')).toBeNull();
        });

        it('formats ISO string with explicit offset minutes', () => {
            expect(formatTimeInOffset('2026-07-22T01:39:04Z', -300)).toBe('8:39 PM');
            expect(formatTimeInOffset('2026-07-22T14:30:00Z', 120)).toBe('4:30 PM');
            expect(formatTimeInOffset('2026-07-22T05:05:00Z', -300)).toBe('12:05 AM');
        });

        it('formatTime uses offset embedded in timestamp or reference timestamp', () => {
            expect(formatTime('2026-07-21T20:39:04-05:00')).toBe('8:39 PM');
            expect(formatTime('2026-07-22T01:39:04Z', '2026-07-21T20:39:04-05:00')).toBe('8:39 PM');
            expect(formatTime('')).toBe('');
        });

        it('ignores zero systemTimestamp (0001-01-01) and falls back to systemStatus timestamp offset', () => {
            const action: Action = {
                timestamp: '2026-07-22T04:09:26.947167547Z',
                systemTimestamp: '0001-01-01T00:00:00Z',
                batteryMode: 1,
                solarMode: 2,
                description: 'test',
                systemStatus: {
                    timestamp: '2026-07-22T00:09:26.947167547-04:00'
                }
            };
            const refTs = (action.systemTimestamp && !isZeroTime(action.systemTimestamp)) ? action.systemTimestamp : action.systemStatus?.timestamp;
            const targetTs = getActionTimestamp(action);
            expect(targetTs).toBe('2026-07-22T04:09:26.947167547Z');
            expect(formatTime(targetTs, refTs)).toBe('12:09 AM');
        });
    });

    describe('formatHour12', () => {
        it('formats midnight and midday correctly', () => {
            expect(formatHour12(0)).toBe('12 AM');
            expect(formatHour12(12)).toBe('12 PM');
        });

        it('formats morning and evening hours in 12-hour time without leading zeros or :00 minutes', () => {
            expect(formatHour12(7)).toBe('7 AM');
            expect(formatHour12(13)).toBe('1 PM');
            expect(formatHour12(20)).toBe('8 PM');
            expect(formatHour12(23)).toBe('11 PM');
        });
    });

    describe('getPlanStatusSubvalue', () => {
        it('formats low rate price with currency symbol', () => {
            const action: Action = {
                description: 'test',
                timestamp: '2026-07-22T04:00:00Z',
                batteryMode: BatteryMode.ChargeAny,
                solarMode: SolarMode.NoExport,
                currentPrice: {
                    tsStart: '2026-07-22T04:00:00Z',
                    tsEnd: '2026-07-22T05:00:00Z',
                    dollarsPerKWH: 0.05,
                    gridUseDollarsPerKWH: 0
                },
                plan: {
                    tsCreated: '2026-07-22T04:00:00Z',
                    horizonHours: 2,
                    totalProjectedCost: 0,
                    totalExportCredits: 0,
                    netEconomicBenefit: 0,
                    periods: [
                        {
                            tsStart: '2026-07-22T04:00:00Z',
                            tsEnd: '2026-07-22T06:00:00Z',
                            durationHours: 2,
                            importDollars: 0.05,
                            batteryMode: BatteryMode.ChargeAny,
                            solarMode: SolarMode.NoExport,
                            reason: ActionReason.AlwaysChargeBelowThreshold,
                            startSoc: 50,
                            endSoc: 100,
                            gridImportKWH: 10,
                            gridExportKWH: 0,
                            costDollars: 0.5
                        }
                    ]
                }
            };
            expect(getPlanStatusSubvalue(action, undefined, '£')).toContain('• Low rate (£0.050/kWh)');
        });
    });

    describe('getBatteryPillLabel', () => {
        it('returns Power Home for Load mode instead of repeating solar text', () => {
            expect(getBatteryPillLabel(BatteryMode.Load)).toBe('Power Home');
            expect(getBatteryPillLabel(BatteryMode.Standby)).toBe('Hold Battery');
            expect(getBatteryPillLabel(BatteryMode.ChargeAny)).toBe('Charge From Solar+Grid');
            expect(getBatteryPillLabel(BatteryMode.Export)).toBe('Export to Grid');
        });
    });

    describe('getActionTitle', () => {
        it('returns distinct strategic titles based on reason', () => {
            const base: Action = {
                description: 'test',
                timestamp: '2026-07-22T04:00:00Z',
                batteryMode: BatteryMode.Load,
                solarMode: SolarMode.Any
            };
            expect(getActionTitle({ ...base, reason: ActionReason.SufficientBatteryTillCharge })).toBe('Using Battery Until Cheap Window');
            expect(getActionTitle({ ...base, reason: ActionReason.SufficientBattery })).toBe('Self-Powered');
            expect(getActionTitle({ ...base, reason: ActionReason.ArbitrageChargeExport, batteryMode: BatteryMode.ChargeAny })).toBe('Charging Before Peak');
            expect(getActionTitle({ ...base, reason: ActionReason.DeficitCharge, batteryMode: BatteryMode.ChargeAny })).toBe('Topping Up Battery');
            expect(getActionTitle({ ...base, reason: ActionReason.ArbitrageHold, batteryMode: BatteryMode.Standby })).toBe('Saving Battery for Peak');
            expect(getActionTitle({ ...base, reason: ActionReason.AlwaysChargeBelowThreshold, batteryMode: BatteryMode.ChargeAny })).toBe('Low-Rate Grid Charge');
            expect(getActionTitle({ ...base })).toBe('Solar first, then battery');
        });
    });

    describe('formatSOCRange & hasSolarActivity', () => {
        it('formats start to end SOC when difference is at least 1%', () => {
            expect(formatSOCRange(48.2, 95.8)).toBe('48% → 96%');
            expect(formatSOCRange(89.1, 51.4)).toBe('89% → 51%');
        });

        it('formats single SOC value when start and end round to the same integer', () => {
            expect(formatSOCRange(99.6, 99.9)).toBe('100%');
            expect(formatSOCRange(50, 50.3)).toBe('50%');
        });

        it('detects solar activity only when solarKW > 0', () => {
            expect(hasSolarActivity({
                description: '',
                timestamp: '',
                batteryMode: 1,
                solarMode: 2,
                systemStatus: { solarKW: 0 }
            })).toBe(false);

            expect(hasSolarActivity({
                description: '',
                timestamp: '',
                batteryMode: 1,
                solarMode: 2,
                systemStatus: { solarKW: 2.4 }
            })).toBe(true);
        });
    });

    describe('buildDaySummary', () => {
        it('orders beats newest to oldest, filters <30m blips, and hides standby when active phases exist', () => {
            const latestLoad: ActionSummary = {
                isSummary: true,
                type: 'grouped',
                reason: ActionReason.SufficientBatteryTillCharge,
                startTime: '2026-07-22T15:00:00-05:00',
                endTime: '2026-07-22T16:30:00-05:00',
                latestAction: {
                    description: '',
                    timestamp: '2026-07-22T16:30:00-05:00',
                    batteryMode: BatteryMode.Load,
                    solarMode: SolarMode.Any,
                    reason: ActionReason.SufficientBatteryTillCharge
                },
                count: 4,
                alarms: new Set(),
                storms: new Set(),
                hasPrice: true,
                avgPrice: 0.105,
                min: 0.1,
                max: 0.11,
                hasSOC: true,
                avgSOC: 70,
                minSOC: 51,
                maxSOC: 89,
                startSOC: 89,
                endSOC: 51
            };

            const shortExportBlip: Action = {
                description: '',
                timestamp: '2026-07-22T14:40:00-05:00',
                batteryMode: BatteryMode.Export,
                solarMode: SolarMode.Export,
                reason: ActionReason.DirectExport,
                systemStatus: { batterySOC: 89 }
            };

            const standbySummary: ActionSummary = {
                isSummary: true,
                type: 'grouped',
                reason: ActionReason.ArbitrageHoldExport,
                startTime: '2026-07-22T12:00:00-05:00',
                endTime: '2026-07-22T14:20:00-05:00',
                latestAction: {
                    description: '',
                    timestamp: '2026-07-22T14:20:00-05:00',
                    batteryMode: BatteryMode.Standby,
                    solarMode: SolarMode.Any,
                    reason: ActionReason.ArbitrageHoldExport
                },
                count: 6,
                alarms: new Set(),
                storms: new Set(),
                hasPrice: true,
                avgPrice: 0.06,
                min: 0.06,
                max: 0.06,
                hasSOC: true,
                avgSOC: 98,
                minSOC: 98,
                maxSOC: 98,
                startSOC: 98,
                endSOC: 98
            };

            const morningCharge: ActionSummary = {
                isSummary: true,
                type: 'grouped',
                reason: ActionReason.ArbitrageChargeExport,
                startTime: '2026-07-22T04:00:00-05:00',
                endTime: '2026-07-22T11:30:00-05:00',
                latestAction: {
                    description: '',
                    timestamp: '2026-07-22T11:30:00-05:00',
                    batteryMode: BatteryMode.ChargeAny,
                    solarMode: SolarMode.Any,
                    reason: ActionReason.ArbitrageChargeExport
                },
                count: 15,
                alarms: new Set(),
                storms: new Set(),
                hasPrice: true,
                avgPrice: 0.055,
                min: 0.04,
                max: 0.07,
                hasSOC: true,
                avgSOC: 60,
                minSOC: 24,
                maxSOC: 98,
                startSOC: 24,
                endSOC: 98
            };

            const summary = buildDaySummary([latestLoad, shortExportBlip, standbySummary, morningCharge], '$');

            expect(summary.segments.some(s => s.category === 'standby')).toBe(true);

            expect(summary.beats).toHaveLength(2);
            expect(summary.beats[0].category).toBe('load');
            expect(summary.beats[0].headline).toBe('Running on solar & battery');
            expect(summary.beats[0].socText).toBe('89% → 51%');

            expect(summary.beats[1].category).toBe('charge');
            expect(summary.beats[1].headline).toBe('Charged before peak');
            expect(summary.beats[1].socText).toBe('24% → 98%');
        });

        it('shows standby beat when the entire day is standby', () => {
            const standbyAction: Action = {
                description: 'Holding',
                timestamp: '2026-07-22T10:00:00-05:00',
                batteryMode: BatteryMode.Standby,
                solarMode: SolarMode.Any,
                systemStatus: { batterySOC: 100 }
            };
            const summary = buildDaySummary([standbyAction], '$');
            expect(summary.beats).toHaveLength(1);
            expect(summary.beats[0].category).toBe('standby');
            expect(summary.beats[0].headline).toBe('Holding battery in standby');
            expect(summary.beats[0].socText).toBe('100%');
        });
    });
});
