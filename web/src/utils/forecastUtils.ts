import { BatteryMode, ActionReason } from '../api';
import type { PlanPeriod } from '../api';

export interface ZoneConfig {
    key: string;
    label: string;
    color: string;
}

export const ALL_ZONES: ZoneConfig[] = [
    { key: 'solarCharge', label: 'Solar Charge', color: '#10b981' },
    { key: 'peakDischarge', label: 'Peak Discharge', color: '#a855f7' },
    { key: 'poweringHome', label: 'Powering Home', color: '#38bdf8' },
    { key: 'atReserve', label: 'At Reserve', color: '#94a3b8' },
    { key: 'standby', label: 'Standby', color: '#64748b' },
    { key: 'gridCharge', label: 'Grid Charge', color: '#06b6d4' },
    { key: 'gridExport', label: 'Grid Export', color: '#f59e0b' },
];

export function classifyPlanPeriod(p: PlanPeriod, reserveSOC: number): ZoneConfig {
    // 1. Forced grid charge
    if (p.batteryMode === BatteryMode.ChargeAny) {
        return ALL_ZONES.find((z) => z.key === 'gridCharge')!;
    }
    // 2. Forced grid export
    if (p.batteryMode === BatteryMode.Export) {
        return ALL_ZONES.find((z) => z.key === 'gridExport')!;
    }
    // 3. Peak discharge
    const isPeak =
        p.reason === ActionReason.DischargeAtPeak ||
        Boolean(p.description && p.description.toLowerCase().includes('peak'));
    if (isPeak && p.endSoc < p.startSoc + 0.1) {
        return ALL_ZONES.find((z) => z.key === 'peakDischarge')!;
    }
    // 4. Solar charging: SOC is rising
    if (p.endSoc > p.startSoc + 0.1) {
        return ALL_ZONES.find((z) => z.key === 'solarCharge')!;
    }
    // 5. Discharging to power home: SOC is falling
    if (p.endSoc < p.startSoc - 0.1) {
        return ALL_ZONES.find((z) => z.key === 'poweringHome')!;
    }
    // 6. At reserve
    if (
        p.reason === ActionReason.BatteryAtReserve ||
        (Math.abs(p.startSoc - reserveSOC) <= 1.5 && Math.abs(p.endSoc - reserveSOC) <= 1.5)
    ) {
        return ALL_ZONES.find((z) => z.key === 'atReserve')!;
    }
    // 7. Standby
    return ALL_ZONES.find((z) => z.key === 'standby')!;
}
