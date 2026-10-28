import { describe, it, expect } from 'vitest';
import {
    classifyPlanPeriod,
    ALL_ZONES,
    reconstructFuturePricesFromPlan,
    buildPlanSolarLoadSeries,
    buildPlanPriceSeries,
    computePlanSummaryStats,
    buildPlanSummaryBeats,
} from './forecastUtils';
import { BatteryMode, ActionReason } from '../api';
import type { PlanPeriod } from '../api';

function makePeriod(overrides: Partial<PlanPeriod> = {}): PlanPeriod {
    return {
        tsStart: '2026-09-30T10:00:00-05:00',
        tsEnd: '2026-09-30T11:00:00-05:00',
        durationHours: 1,
        importDollars: 0.15,
        batteryMode: BatteryMode.Load,
        solarMode: 2,
        reason: ActionReason.SufficientBattery,
        startSoc: 50,
        endSoc: 50,
        gridImportKWH: 0,
        gridExportKWH: 0,
        costDollars: 0,
        ...overrides,
    };
}

describe('classifyPlanPeriod', () => {
    it('defines standard ALL_ZONES configurations', () => {
        expect(ALL_ZONES).toHaveLength(6);
        expect(ALL_ZONES.map((z) => z.key)).toContain('solarCharge');
        expect(ALL_ZONES.map((z) => z.key)).toContain('poweringHome');
        expect(ALL_ZONES.map((z) => z.key)).toContain('solarExport');
        expect(ALL_ZONES.map((z) => z.key)).toContain('standby');
        expect(ALL_ZONES.map((z) => z.key)).toContain('gridCharge');
        expect(ALL_ZONES.map((z) => z.key)).toContain('gridExport');
        expect(ALL_ZONES.map((z) => z.key)).not.toContain('atReserve');
        expect(ALL_ZONES.map((z) => z.key)).not.toContain('peakDischarge');
    });

    it('classifies forced grid charging as Grid Charge with purple color', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.ChargeAny,
            startSoc: 20,
            endSoc: 60,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('gridCharge');
        expect(result.label).toBe('Grid Charge');
        expect(result.color).toBe('#8b5cf6');
    });

    it('classifies grid export as Grid Export', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Export,
            startSoc: 70,
            endSoc: 30,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('gridExport');
        expect(result.label).toBe('Grid Export');
        expect(result.color).toBe('#f59e0b');
    });

    it('classifies direct solar export as Solar Export', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Load,
            solarMode: 3,
            reason: ActionReason.DirectExport,
            startSoc: 70,
            endSoc: 65,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('solarExport');
        expect(result.label).toBe('Solar Export');
        expect(result.color).toBe('#f97316');
    });

    it('classifies discharging during peak rate as Powering Home', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Load,
            reason: ActionReason.DischargeAtPeak,
            startSoc: 65,
            endSoc: 50,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('poweringHome');
        expect(result.label).toBe('Powering Home');
        expect(result.color).toBe('#38bdf8');
    });

    it('classifies rising SOC during self-consumption as Solar Charge', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Load,
            reason: ActionReason.SufficientBattery,
            startSoc: 21.8,
            endSoc: 25.2,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('solarCharge');
        expect(result.label).toBe('Solar Charge');
        expect(result.color).toBe('#10b981');
    });

    it('classifies falling SOC outside of peak as Powering Home', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Load,
            reason: ActionReason.SufficientBattery,
            startSoc: 70,
            endSoc: 65,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('poweringHome');
        expect(result.label).toBe('Powering Home');
        expect(result.color).toBe('#38bdf8');
    });

    it('classifies period at reserve limit as Standby', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Load,
            reason: ActionReason.BatteryAtReserve,
            startSoc: 20,
            endSoc: 20,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('standby');
        expect(result.label).toBe('Standby');
        expect(result.color).toBe('#64748b');
    });

    it('classifies flat SOC near reserve as Standby', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Load,
            startSoc: 20.5,
            endSoc: 20.5,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('standby');
        expect(result.label).toBe('Standby');
        expect(result.color).toBe('#64748b');
    });

    it('classifies flat SOC holding charge away from reserve as Standby', () => {
        const period = makePeriod({
            batteryMode: BatteryMode.Standby,
            startSoc: 75,
            endSoc: 75,
        });
        const result = classifyPlanPeriod(period);
        expect(result.key).toBe('standby');
        expect(result.label).toBe('Standby');
        expect(result.color).toBe('#64748b');
    });
});

describe('reconstructFuturePricesFromPlan & buildPlanPriceSeries', () => {
    it('collapses 20-minute intervals within the same clock hour while preserving mid-hour rate changes', () => {
        const periods: PlanPeriod[] = [
            makePeriod({
                tsStart: '2026-09-30T10:20:00-05:00',
                tsEnd: '2026-09-30T10:40:00-05:00',
                durationHours: 1 / 3,
                importDollars: 0.12,
                exportDollars: 0.05,
            }),
            makePeriod({
                tsStart: '2026-09-30T10:40:00-05:00',
                tsEnd: '2026-09-30T11:00:00-05:00',
                durationHours: 1 / 3,
                importDollars: 0.12,
                exportDollars: 0.05,
            }),
            makePeriod({
                tsStart: '2026-09-30T11:00:00-05:00',
                tsEnd: '2026-09-30T11:30:00-05:00',
                durationHours: 0.5,
                importDollars: 0.15,
                exportDollars: 0.06,
            }),
            makePeriod({
                tsStart: '2026-09-30T11:30:00-05:00',
                tsEnd: '2026-09-30T12:00:00-05:00',
                durationHours: 0.5,
                importDollars: 0.30,
                exportDollars: 0.12,
            }),
        ];

        const reconstructed = reconstructFuturePricesFromPlan(periods);
        expect(reconstructed).toHaveLength(3);
        expect(reconstructed[0]).toMatchObject({
            tsHourStart: '2026-09-30T10:00:00-05:00',
            dollarsPerKWH: 0.12,
            exportDollarsPerKWH: 0.05,
        });
        expect(reconstructed[1]).toMatchObject({
            tsHourStart: '2026-09-30T11:00:00-05:00',
            dollarsPerKWH: 0.15,
            exportDollarsPerKWH: 0.06,
        });
        expect(reconstructed[2]).toMatchObject({
            tsHourStart: '2026-09-30T11:30:00-05:00',
            dollarsPerKWH: 0.30,
            exportDollarsPerKWH: 0.12,
        });

        const priceSeries = buildPlanPriceSeries(periods, reconstructed);
        // 3 price points + 1 closing step point at 12:00
        expect(priceSeries).toHaveLength(4);
        expect(priceSeries[3].ts).toBe('2026-09-30T12:00:00-05:00');
        expect(priceSeries[3].gridChargeDollarsPerKWH).toBe(0.30);
    });
});

describe('buildPlanSolarLoadSeries & computePlanSummaryStats', () => {
    it('aggregates 20-minute periods into hourly kWh rates and computes 24h totals and min/peak rates', () => {
        const periods: PlanPeriod[] = [
            makePeriod({
                tsStart: '2026-09-30T12:00:00Z',
                tsEnd: '2026-09-30T12:20:00Z',
                durationHours: 1 / 3,
                solarKWH: 1.5,
                loadKWH: 0.4,
                importDollars: 0.10,
            }),
            makePeriod({
                tsStart: '2026-09-30T12:20:00Z',
                tsEnd: '2026-09-30T12:40:00Z',
                durationHours: 1 / 3,
                solarKWH: 1.5,
                loadKWH: 0.4,
                importDollars: 0.10,
            }),
            makePeriod({
                tsStart: '2026-09-30T12:40:00Z',
                tsEnd: '2026-09-30T13:00:00Z',
                durationHours: 1 / 3,
                solarKWH: 1.5,
                loadKWH: 0.4,
                importDollars: 0.10,
            }),
            makePeriod({
                tsStart: '2026-09-30T13:00:00Z',
                tsEnd: '2026-09-30T14:00:00Z',
                durationHours: 1,
                solarKWH: 3.0,
                loadKWH: 1.8,
                importDollars: 0.28,
            }),
        ];

        const solarLoad = buildPlanSolarLoadSeries(periods);
        expect(solarLoad).toHaveLength(2);
        expect(solarLoad[0].ts).toBe('2026-09-30T12:00:00Z');
        expect(solarLoad[0].predictedSolarKWH).toBeCloseTo(4.5, 4);
        expect(solarLoad[0].avgHomeLoadKWH).toBeCloseTo(1.2, 4);
        expect(solarLoad[1].ts).toBe('2026-09-30T13:00:00Z');
        expect(solarLoad[1].predictedSolarKWH).toBeCloseTo(3.0, 4);
        expect(solarLoad[1].avgHomeLoadKWH).toBeCloseTo(1.8, 4);

        const stats = computePlanSummaryStats(periods);
        expect(stats.totalSolarKWH).toBeCloseTo(7.5, 4);
        expect(stats.totalLoadKWH).toBeCloseTo(3.0, 4);
        expect(stats.minImportDollars).toBeCloseTo(0.10, 4);
        expect(stats.maxImportDollars).toBeCloseTo(0.28, 4);
    });
});

describe('buildPlanSummaryBeats', () => {
    it('merges contiguous periods, bridges brief standby blips, and omits standby when active beats exist', () => {
        const periods: PlanPeriod[] = [
            // Initial standby hold that should be omitted from beats
            makePeriod({
                tsStart: '2026-09-30T08:00:00-05:00',
                tsEnd: '2026-09-30T09:00:00-05:00',
                durationHours: 1,
                batteryMode: BatteryMode.Standby,
                startSoc: 20,
                endSoc: 20,
                importDollars: 0.10,
            }),
            makePeriod({
                tsStart: '2026-09-30T09:00:00-05:00',
                tsEnd: '2026-09-30T11:00:00-05:00',
                durationHours: 2,
                startSoc: 20,
                endSoc: 60,
                solarKWH: 5.0,
                importDollars: 0.10,
            }),
            // Brief 20m standby blip bridged across solarCharge
            makePeriod({
                tsStart: '2026-09-30T11:00:00-05:00',
                tsEnd: '2026-09-30T11:20:00-05:00',
                durationHours: 1 / 3,
                startSoc: 60,
                endSoc: 60,
                solarKWH: 0.2,
                importDollars: 0.10,
            }),
            makePeriod({
                tsStart: '2026-09-30T11:20:00-05:00',
                tsEnd: '2026-09-30T14:00:00-05:00',
                durationHours: 8 / 3,
                startSoc: 60,
                endSoc: 95,
                solarKWH: 6.8,
                importDollars: 0.12,
            }),
            makePeriod({
                tsStart: '2026-09-30T14:00:00-05:00',
                tsEnd: '2026-09-30T18:00:00-05:00',
                durationHours: 4,
                batteryMode: BatteryMode.Load,
                reason: ActionReason.DischargeAtPeak,
                startSoc: 95,
                endSoc: 40,
                loadKWH: 6.4,
                importDollars: 0.32,
            }),
        ];

        const beats = buildPlanSummaryBeats(periods);
        expect(beats).toHaveLength(2);
        expect(beats[0]).toMatchObject({
            category: 'solarCharge',
            headline: 'Charging from solar',
            socText: '20% → 95%',
        });
        expect(beats[1]).toMatchObject({
            category: 'load',
            headline: 'Powering home at peak',
            socText: '95% → 40%',
            priceText: '$ 0.320/kWh',
        });
    });
});
