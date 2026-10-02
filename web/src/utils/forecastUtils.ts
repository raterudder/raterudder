import { BatteryMode, ActionReason, SolarMode } from '../api';
import type { PlanPeriod } from '../api';

export interface ZoneConfig {
    key: string;
    label: string;
    color: string;
}

export const ALL_ZONES: ZoneConfig[] = [
    { key: 'solarCharge', label: 'Solar Charge', color: '#10b981' },
    { key: 'poweringHome', label: 'Powering Home', color: '#38bdf8' },
    { key: 'solarExport', label: 'Solar Export', color: '#f97316' },
    { key: 'standby', label: 'Standby', color: '#64748b' },
    { key: 'gridCharge', label: 'Grid Charge', color: '#8b5cf6' },
    { key: 'gridExport', label: 'Grid Export', color: '#f59e0b' },
];

export function classifyPlanPeriod(p: PlanPeriod): ZoneConfig {
    // 1. Forced grid charge
    if (p.batteryMode === BatteryMode.ChargeAny) {
        return ALL_ZONES.find((z) => z.key === 'gridCharge')!;
    }
    // 2. Forced grid export
    if (p.batteryMode === BatteryMode.Export) {
        return ALL_ZONES.find((z) => z.key === 'gridExport')!;
    }
    // 3. Solar charging: SOC is rising
    if (p.endSoc > p.startSoc + 0.1) {
        return ALL_ZONES.find((z) => z.key === 'solarCharge')!;
    }
    // 4. Solar export: solar exporting while battery covers load (direct export)
    const isSolarExport =
        (p.solarMode === SolarMode.Export || p.reason === ActionReason.DirectExport) &&
        (p.batteryMode === BatteryMode.Load || p.endSoc < p.startSoc - 0.1);
    if (isSolarExport) {
        return ALL_ZONES.find((z) => z.key === 'solarExport')!;
    }
    // 5. Discharging to power home: SOC is falling
    if (p.endSoc < p.startSoc - 0.1) {
        return ALL_ZONES.find((z) => z.key === 'poweringHome')!;
    }
    // 6. Standby (includes holding at reserve or flat SOC)
    return ALL_ZONES.find((z) => z.key === 'standby')!;
}
