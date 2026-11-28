export interface ZoneConfig {
    key: string;
    label: string;
    color: string;
}

export const ALL_ZONES: ZoneConfig[] = [
    { key: 'solarCharge', label: 'Solar Charge', color: '#22c55e' },
    { key: 'poweringHome', label: 'Powering Home', color: '#3b82f6' },
    { key: 'solarExport', label: 'Solar Export', color: '#f97316' },
    { key: 'standby', label: 'Standby', color: '#64748b' },
    { key: 'gridCharge', label: 'Grid Charge', color: '#8b5cf6' },
    { key: 'gridExport', label: 'Grid Export', color: '#f59e0b' },
];
