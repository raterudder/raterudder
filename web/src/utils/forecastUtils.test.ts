import { describe, it, expect } from 'vitest';
import { classifyPlanPeriod, ALL_ZONES } from './forecastUtils';
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
