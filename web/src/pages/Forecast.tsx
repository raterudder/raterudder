import React, { useEffect, useState, useMemo, useRef } from 'react';
import { fetchModeling } from '../api';
import type { ForecastResponse, ModelingHour, Settings, PlanPeriod } from '../api';
import { Switch } from '@base-ui/react/switch';
import { Field } from '@base-ui/react/field';
import {
    ResponsiveContainer,
    AreaChart,
    Area,
    XAxis,
    YAxis,
    CartesianGrid,
    Tooltip,
    ReferenceLine,
    Line,
    ReferenceArea,
} from 'recharts';
import { HelpButton } from '../components/HelpButton';
import './Forecast.css';

type ChartConfig = {
    title: string;
    dataKey: string;
    color: string;
    gradientId: string;
    unit: string;
    helpDescription?: React.ReactNode;
    areaType?: 'monotone' | 'step' | 'stepAfter' | 'stepBefore';
    referenceLine?: { dataKey: string; label: string; color: string };
    additionalLines?: { dataKey: string; label?: string; color: string; strokeDasharray?: string; type?: 'monotone' | 'step' | 'stepAfter' | 'stepBefore' }[];
};

const charts: ChartConfig[] = [
    {
        title: 'Battery (if used) (%)',
        dataKey: 'batterySOCIfUsed',
        color: 'var(--accent)',
        gradientId: 'batteryGrad',
        unit: '%',
        helpDescription: (
            <p>
                Displays the forecasted State of Charge (SOC) of your battery if RateRudder did nothing to optimize your system.
                It shows how battery levels would naturally change based solely on home load and solar generation without automated charging, discharging, or grid arbitrage interventions.
                The dashed red line represents your Minimum Reserve SOC threshold.
            </p>
        ),
        additionalLines: [
            { dataKey: 'batteryReserveSOC', color: '#ef4444', strokeDasharray: '6 4', type: 'stepAfter' },
        ],
    },
    {
        title: 'Predicted Solar (kWh)',
        dataKey: 'predictedSolarKWH',
        color: 'var(--warning)',
        gradientId: 'solarGrad',
        unit: ' kWh',
        helpDescription: (
            <p>
                Forecasts expected hourly solar generation in kilowatt-hours (kWh) for your system based on weather predictions, historical calibration, and panel orientation. RateRudder uses this forecast to optimize battery charging and grid usage.
            </p>
        ),
    },
    {
        title: 'Predicted Home Load (kWh)',
        dataKey: 'avgHomeLoadKWH',
        color: '#a855f7',
        gradientId: 'loadGrad',
        unit: ' kWh',
        helpDescription: (
            <p>
                Forecasts expected hourly electricity usage (in kWh) for your home based on historical consumption patterns, day of week, and temperature predictions.
            </p>
        ),
    },
    {
        title: 'Grid Charge Cost ($/kWh)',
        dataKey: 'gridChargeDollarsPerKWH',
        color: '#10b981',
        gradientId: 'priceGrad',
        unit: ' $/kWh',
        areaType: 'stepAfter',
        helpDescription: (
            <p>
                Displays hourly electricity import prices and export credit rates ($/kWh) according to your utility rate plan. RateRudder uses these rates to schedule low-cost grid charging, avoid expensive peak pricing, and optimize solar or battery export credits.
            </p>
        ),
        additionalLines: [
            {
                dataKey: 'gridExportDollarsPerKWH',
                label: 'Export Rate',
                color: '#f59e0b',
                strokeDasharray: '4 3',
                type: 'stepAfter',
            },
        ],
    },
];

import { ALL_ZONES, classifyPlanPeriod } from '../utils/forecastUtils';


const planCharts: ChartConfig[] = [
    {
        title: 'Planned Battery SOC (%)',
        dataKey: 'plannedSOC',
        color: '#38bdf8',
        gradientId: 'plannedBatteryGrad',
        unit: '%',
        helpDescription: (
            <p>
                Displays the planned State of Charge (SOC) of your battery as optimized by RateRudder.
                The colored line segments highlight scheduled operations (charging from solar, powering home, grid charging, and standby holds).
                The dashed red line represents your Minimum Reserve SOC threshold.
            </p>
        ),
    },
    charts[1],
    charts[2],
    charts[3],
];

import { formatTime } from '../utils/dashboardUtils';

function formatHour(ts: string, referenceTs?: string): string {
    if (!ts) return '';
    const formatted = formatTime(ts, referenceTs);
    return formatted.replace(':00 ', ' ');
}

// Extended interface adding calculated fields
interface ProcessedModelingHour extends ModelingHour {
    batterySOCIfUsed: number;
    batteryReserveSOC: number;
    plannedSOC?: number;
    gridExportDollarsPerKWH?: number;
}

function ForecastChart({ data, config, isMobile, showCurrentTime, nowMs, headerAction, planPeriods }: {
    data: any[];
    config: ChartConfig;
    isMobile: boolean;
    showCurrentTime: boolean;
    nowMs: number;
    headerAction?: React.ReactNode;
    planPeriods?: PlanPeriod[];
}) {
    // Compute reference value if applicable
    const refValue = config.referenceLine
        ? (data[0]?.[config.referenceLine.dataKey as keyof ProcessedModelingHour] as number)
        : undefined;

    const currentTimeStr = React.useMemo(() => {
        if (!showCurrentTime || data.length === 0) return undefined;
        // find closest hour in data
        let closest = data[0].ts;
        let minDiff = Infinity;
        for (const d of data) {
            const diff = Math.abs(new Date(d.ts).getTime() - nowMs);
            if (diff < minDiff) {
                minDiff = diff;
                closest = d.ts;
            }
        }
        return closest;
    }, [data, showCurrentTime, nowMs]);

    const vppTicks = React.useMemo(() => {
        if (config.dataKey !== 'batterySOCIfUsed' && config.dataKey !== 'plannedSOC') return null;

        const isZeroTime = (ts: string | undefined | null) => {
            if (!ts) return true;
            const date = new Date(ts);
            return isNaN(date.getTime()) || date.getFullYear() <= 1;
        };

        let startIdx = -1;
        let endIdx = -1;
        for (let i = 0; i < data.length; i++) {
            const start = data[i].vppStandbyAt;
            const end = data[i].vppEndAt;
            if (start && !isZeroTime(start)) {
                if (startIdx === -1) {
                    startIdx = i;
                }
            }
            if (end && !isZeroTime(end)) {
                endIdx = i;
            }
        }
        if (startIdx !== -1 && endIdx !== -1) {
            const endHourIdx = Math.min(endIdx + 1, data.length - 1);
            return {
                start: data[startIdx].ts,
                end: data[endHourIdx].ts
            };
        }
        return null;
    }, [data, config.dataKey]);

    const activeZones = React.useMemo(() => {
        if (config.dataKey !== 'plannedSOC' || !planPeriods || planPeriods.length === 0) {
            return [];
        }
        const usedKeys = new Set<string>();
        for (const p of planPeriods) {
            const z = classifyPlanPeriod(p);
            usedKeys.add(z.key);
        }
        return ALL_ZONES.filter((z) => usedKeys.has(z.key));
    }, [config.dataKey, planPeriods]);

    const hasReserveLine = React.useMemo(() => {
        if (config.dataKey === 'plannedSOC') {
            return data.some((d) => d.batteryReserveSOC !== undefined && d.batteryReserveSOC > 0);
        }
        return false;
    }, [config.dataKey, data]);

    const lastReserveVal = React.useMemo(() => {
        for (let i = data.length - 1; i >= 0; i--) {
            const val = data[i].batteryReserveSOC;
            if (val !== undefined && val !== null && !isNaN(val) && val > 0) {
                return val;
            }
        }
        return undefined;
    }, [data]);

    return (
        <div className="chart-card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
                <h3 style={{ margin: 0, display: 'flex', alignItems: 'center' }}>
                    {config.title}
                    {config.helpDescription && (
                        <HelpButton title={config.title} description={config.helpDescription} />
                    )}
                </h3>
                {headerAction}
            </div>
            {config.dataKey === 'plannedSOC' && activeZones.length > 0 && (
                <div className="mode-legend" aria-label="Battery Modes Legend">
                    <span className="mode-legend-title">Battery Modes:</span>
                    {activeZones.map((zone) => (
                        <div key={zone.key} className="mode-legend-item">
                            <span className="mode-legend-line" style={{ backgroundColor: zone.color }} />
                            <span>{zone.label}</span>
                        </div>
                    ))}
                    {showCurrentTime && (
                        <div className="mode-legend-item">
                            <span className="mode-legend-line" style={{ backgroundColor: 'var(--outline-variant)' }} />
                            <span>History</span>
                        </div>
                    )}
                </div>
            )}
            {config.dataKey === 'gridChargeDollarsPerKWH' && (
                <div className="mode-legend" aria-label="Rate Legend">
                    <div className="mode-legend-item">
                        <span className="mode-legend-line" style={{ backgroundColor: config.color }} />
                        <span>Import Rate</span>
                    </div>
                    <div className="mode-legend-item">
                        <span
                            className="mode-legend-line"
                            style={{
                                background: 'repeating-linear-gradient(90deg, #f59e0b, #f59e0b 4px, transparent 4px, transparent 7px)',
                            }}
                        />
                        <span>Export Rate</span>
                    </div>
                </div>
            )}
            <ResponsiveContainer width="100%" height={200}>
                <AreaChart data={data} syncId="forecast" margin={{ top: 5, right: isMobile ? 0 : 20, left: 0, bottom: 5 }}>
                    <defs>
                        <linearGradient id={config.gradientId} x1="0" y1="0" x2="0" y2="1">
                            <stop offset="5%" stopColor={config.color} stopOpacity={0.3} />
                            <stop offset="95%" stopColor={config.color} stopOpacity={0.02} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--outline-variant)" vertical={false} opacity={0.4} />
                    <XAxis
                        dataKey="ts"
                        tickFormatter={(ts) => formatHour(ts, data[0]?.ts)}
                        tick={{ fontSize: isMobile ? 10 : 12 }}
                        stroke="var(--outline-variant)"
                        axisLine={false}
                        tickLine={false}
                    />
                    <YAxis
                        tick={{ fontSize: isMobile ? 10 : 12 }}
                        stroke="var(--outline-variant)"
                        width={isMobile ? 35 : 50}
                        axisLine={false}
                        tickLine={false}
                        tickFormatter={(v: number) =>
                            config.unit.includes('$') ? `$${v.toFixed(2)}` : v.toFixed(1)
                        }
                    />
                    <Tooltip
                        content={({ active, payload, label }) => {
                            if (!active || !payload || !payload.length) return null;
                            const pt = payload[0].payload;
                            const timeStr = formatHour(String(label || pt.ts), data[0]?.ts);

                            const containerStyle: React.CSSProperties = {
                                backgroundColor: 'var(--surface-container-high)',
                                border: '1px solid var(--border)',
                                borderRadius: '8px',
                                boxShadow: 'var(--shadow-lg)',
                                padding: '8px 12px',
                                color: 'var(--on-surface)',
                                backdropFilter: 'blur(10px)',
                                fontSize: isMobile ? '0.75rem' : '0.85rem',
                                minWidth: '140px',
                            };

                            if (config.dataKey === 'plannedSOC') {
                                const soc = pt.plannedSOC ?? pt.batterySOCIfUsed;
                                return (
                                    <div style={containerStyle}>
                                        <div style={{ color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 700 }}>
                                            {timeStr}
                                        </div>
                                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '12px', marginBottom: '4px' }}>
                                            <span style={{ color: 'var(--text-muted)' }}>Battery SOC:</span>
                                            <span style={{ fontWeight: 600 }}>{soc !== undefined ? `${Number(soc).toFixed(1)}%` : '--'}</span>
                                        </div>
                                        {pt.zone && (
                                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: '12px', alignItems: 'center' }}>
                                                <span style={{ color: 'var(--text-muted)' }}>Mode:</span>
                                                <span style={{ color: pt.zoneColor, fontWeight: 700 }}>{pt.zone}</span>
                                            </div>
                                        )}
                                    </div>
                                );
                            }

                            return (
                                <div style={containerStyle}>
                                    <div style={{ color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 700 }}>
                                        {timeStr}
                                    </div>
                                    {payload.map((entry: any, i: number) => {
                                        let displayName = config.title;
                                        if (entry.dataKey === 'batteryReserveSOC') {
                                            displayName = 'Reserve SOC';
                                        } else if (entry.dataKey === 'batterySOCIfUsed') {
                                            displayName = 'Battery SOC';
                                        } else if (entry.dataKey === 'predictedSolarKWH') {
                                            displayName = 'Solar';
                                        } else if (entry.dataKey === 'avgHomeLoadKWH') {
                                            displayName = 'Home Load';
                                        } else if (entry.dataKey === 'gridChargeDollarsPerKWH') {
                                            displayName = 'Import Rate';
                                        } else if (entry.dataKey === 'gridExportDollarsPerKWH' || entry.dataKey === 'solarOppDollarsPerKWH') {
                                            displayName = 'Export Rate';
                                        }

                                        const val = Number(entry.value ?? 0);
                                        const formattedVal = config.unit.includes('$')
                                            ? (val < 0 ? `-$${Math.abs(val).toFixed(4)}` : `$${val.toFixed(4)}`)
                                            : `${val.toFixed(1)}${config.unit.trim()}`;

                                        return (
                                            <div
                                                key={i}
                                                style={{
                                                    display: 'flex',
                                                    justifyContent: 'space-between',
                                                    gap: '12px',
                                                    marginBottom: i < payload.length - 1 ? '4px' : 0,
                                                }}
                                            >
                                                <span style={{ color: 'var(--text-muted)' }}>{displayName}:</span>
                                                <span style={{ fontWeight: 600 }}>{formattedVal}</span>
                                            </div>
                                        );
                                    })}
                                </div>
                            );
                        }}
                    />
                    {config.dataKey === 'plannedSOC' ? (
                        <>
                            <Area
                                type="monotone"
                                dataKey="plannedSOC"
                                stroke="none"
                                fill={`url(#${config.gradientId})`}
                                isAnimationActive={false}
                            />
                            {activeZones.map((z) => (
                                <Line
                                    key={z.key}
                                    type="monotone"
                                    dataKey={`${z.key}SOC`}
                                    stroke={z.color}
                                    strokeWidth={3}
                                    dot={false}
                                    connectNulls={false}
                                    isAnimationActive={false}
                                />
                            ))}
                            {showCurrentTime && (
                                <Line
                                    key="historySOC"
                                    type="monotone"
                                    dataKey="historySOC"
                                    stroke="var(--outline-variant)"
                                    strokeDasharray="4 4"
                                    strokeWidth={2}
                                    dot={false}
                                    connectNulls={false}
                                    isAnimationActive={false}
                                />
                            )}
                            {hasReserveLine && (
                                <Line
                                    key="batteryReserveSOC"
                                    type="stepAfter"
                                    dataKey="batteryReserveSOC"
                                    stroke="#ef4444"
                                    strokeWidth={2}
                                    strokeDasharray="6 4"
                                    dot={false}
                                    activeDot={false}
                                    isAnimationActive={false}
                                />
                            )}
                            {hasReserveLine && lastReserveVal !== undefined && (
                                <ReferenceLine
                                    y={lastReserveVal}
                                    stroke="none"
                                    label={{
                                        value: 'Reserve',
                                        fill: '#ef4444',
                                        fontSize: 11,
                                        position: 'insideTopRight',
                                    }}
                                />
                            )}
                        </>
                    ) : (
                        <>
                            <Area
                                type={config.areaType || 'monotone'}
                                dataKey={config.dataKey}
                                stroke={config.color}
                                strokeWidth={3}
                                fill={`url(#${config.gradientId})`}
                                isAnimationActive={true}
                            />
                            {config.additionalLines?.map((line) => (
                                <Line
                                    key={line.dataKey}
                                    type={line.type || 'monotone'}
                                    dataKey={line.dataKey}
                                    stroke={line.color}
                                    strokeWidth={2}
                                    strokeDasharray={line.strokeDasharray}
                                    dot={false}
                                    activeDot={line.dataKey === 'batteryReserveSOC' ? false : undefined}
                                />
                            ))}
                            {config.dataKey === 'batterySOCIfUsed' && lastReserveVal !== undefined && (
                                <ReferenceLine
                                    y={lastReserveVal}
                                    stroke="none"
                                    label={{
                                        value: 'Reserve',
                                        fill: '#ef4444',
                                        fontSize: 11,
                                        position: 'insideTopRight',
                                    }}
                                />
                            )}
                        </>
                    )}
                    {config.referenceLine && refValue !== undefined && (
                        <ReferenceLine
                            y={refValue}
                            stroke={config.referenceLine.color}
                            strokeDasharray="6 4"
                            label={{
                                value: config.referenceLine.label,
                                fill: config.referenceLine.color,
                                fontSize: 11,
                                position: 'insideTopRight',
                            }}
                        />
                    )}
                    {showCurrentTime && currentTimeStr && (
                        <ReferenceLine
                            x={currentTimeStr}
                            stroke="var(--primary)"
                            strokeDasharray="3 3"
                            label={{
                                value: 'Now',
                                position: 'insideTopLeft',
                                fill: 'var(--primary)',
                                fontSize: 11,
                            }}
                        />
                    )}
                    {vppTicks && (
                        <ReferenceArea
                            x1={vppTicks.start}
                            x2={vppTicks.end}
                            fill="rgba(59, 130, 246, 0.08)"
                            label={{
                                value: 'VPP Event',
                                position: 'insideTopLeft',
                                fill: '#3b82f6',
                                fontSize: 10,
                                fontWeight: 'bold',
                            }}
                        />
                    )}
                    {vppTicks && (
                        <ReferenceLine
                            x={vppTicks.start}
                            stroke="#3b82f6"
                            strokeDasharray="3 3"
                        />
                    )}
                    {vppTicks && (
                        <ReferenceLine
                            x={vppTicks.end}
                            stroke="#3b82f6"
                            strokeDasharray="3 3"
                        />
                    )}
                </AreaChart>
            </ResponsiveContainer>
        </div>
    );
}

interface ForecastProps {
    siteID?: string;
    settings?: Settings | null;
}

export const STALE_FORECAST_TIMEOUT_MS = 10 * 60 * 1000;
export const STALE_DASHBOARD_TIMEOUT_MS = STALE_FORECAST_TIMEOUT_MS;

const Forecast: React.FC<ForecastProps> = ({ siteID, settings = null }) => {
    const [rawModelingData, setRawModelingData] = useState<ForecastResponse | null>(null);
    const initialStrategy = settings?.homeLoadPredictionStrategy === 'conservative' ? 'conservative' : 'default';
    const [loadPredictionMode, setLoadPredictionMode] = useState<'default' | 'conservative'>(initialStrategy);
    const [initialized, setInitialized] = useState(false);
    const lastFetchedStrategyRef = useRef<string | null>(null);
    const [loading, setLoading] = useState(true);
    const [nowMs, setNowMs] = useState(() => Date.now());
    const [error, setError] = useState<string | null>(null);
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);
    const [includeHistory, setIncludeHistory] = useState(false);

    const [refreshTrigger, setRefreshTrigger] = useState(0);
    const lastHiddenTimeRef = useRef<number | null>(
        typeof document !== 'undefined' && document.visibilityState === 'hidden' ? Date.now() : null
    );
    const lastFetchTimeRef = useRef<number>(Date.now());
    const isRefreshRef = useRef(false);
    const loadPredictionModeRef = useRef(loadPredictionMode);
    loadPredictionModeRef.current = loadPredictionMode;
    const siteIDRef = useRef(siteID);
    siteIDRef.current = siteID;

    useEffect(() => {
        if (typeof document === 'undefined') return;

        const handleVisibilityChange = () => {
            if (document.visibilityState === 'hidden') {
                lastHiddenTimeRef.current = Date.now();
            } else if (document.visibilityState === 'visible') {
                const lastHidden = lastHiddenTimeRef.current;
                if (lastHidden !== null) {
                    const now = Date.now();
                    const timeAway = now - lastHidden;
                    const timeSinceLastFetch = now - lastFetchTimeRef.current;
                    lastHiddenTimeRef.current = null;

                    if (timeAway >= STALE_FORECAST_TIMEOUT_MS || timeSinceLastFetch >= STALE_FORECAST_TIMEOUT_MS) {
                        isRefreshRef.current = true;
                        setRefreshTrigger(prev => prev + 1);
                    }
                }
            }
        };

        document.addEventListener('visibilitychange', handleVisibilityChange);
        return () => {
            document.removeEventListener('visibilitychange', handleVisibilityChange);
        };
    }, []);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    // Initial load on siteID change or settings strategy change: fetch matching modeling.
    useEffect(() => {
        let isCancelled = false;
        const loadInitialData = async () => {
            setLoading(true);
            setInitialized(false);
            try {
                const strategy = settings?.homeLoadPredictionStrategy === 'conservative' ? 'conservative' : 'default';

                // Fetch modeling using the resolved settings strategy
                const mod = await fetchModeling(siteID, strategy);

                if (!isCancelled) {
                    // Update states together
                    setLoadPredictionMode(strategy);
                    lastFetchedStrategyRef.current = strategy;
                    setRawModelingData(mod);
                    setInitialized(true);
                    lastFetchTimeRef.current = Date.now();
                }
            } catch (error) {
                if (!isCancelled) {
                    setError(error instanceof Error ? error.message : 'Unknown error');
                }
            } finally {
                if (!isCancelled) {
                    setLoading(false);
                    lastFetchTimeRef.current = Date.now();
                }
            }
        };
        loadInitialData();
        return () => {
            isCancelled = true;
        };
    }, [siteID, settings?.homeLoadPredictionStrategy]);

    // Re-fetch modeling only when toggle is manually flipped by user after initialization
    useEffect(() => {
        if (!initialized) return;
        if (lastFetchedStrategyRef.current === loadPredictionMode) return;
        let isCancelled = false;

        const loadModelingOverride = async () => {
            setLoading(true);
            try {
                lastFetchedStrategyRef.current = loadPredictionMode;
                const mod = await fetchModeling(siteID, loadPredictionMode);
                if (!isCancelled) {
                    setRawModelingData(mod);
                    lastFetchTimeRef.current = Date.now();
                }
            } catch (error) {
                if (!isCancelled) {
                    setError(error instanceof Error ? error.message : 'Unknown error');
                }
            } finally {
                if (!isCancelled) {
                    setLoading(false);
                    lastFetchTimeRef.current = Date.now();
                }
            }
        };
        loadModelingOverride();
        return () => {
            isCancelled = true;
        };
    }, [loadPredictionMode, initialized, siteID]);

    // Refresh modeling when returning to page after timeout
    useEffect(() => {
        if (refreshTrigger === 0) return;
        let isCancelled = false;
        isRefreshRef.current = false;

        const refreshData = async () => {
            setLoading(true);
            setError(null);
            try {
                const mod = await fetchModeling(siteIDRef.current, loadPredictionModeRef.current);
                if (!isCancelled) {
                    setRawModelingData(mod);
                    setNowMs(Date.now());
                    lastFetchTimeRef.current = Date.now();
                }
            } catch (error) {
                if (!isCancelled) {
                    setError(error instanceof Error ? error.message : 'Unknown error');
                }
            } finally {
                if (!isCancelled) {
                    setLoading(false);
                    lastFetchTimeRef.current = Date.now();
                }
            }
        };
        refreshData();
        return () => {
            isCancelled = true;
        };
    }, [refreshTrigger]);

    const isPlanActive = Boolean(
        rawModelingData?.plan &&
        rawModelingData.plan.periods &&
        rawModelingData.plan.periods.length > 0
    );

    const data = useMemo(() => {
        if (!rawModelingData) return [];

        if (isPlanActive) {
            const plan = rawModelingData.plan!;
            const periods = plan.periods;

            let planData: any[] = [];
            if (includeHistory && rawModelingData.energyHistory && rawModelingData.priceHistory) {
                const energyHist = rawModelingData.energyHistory || [];
                const priceHist = rawModelingData.priceHistory || [];
                const historyMapped = energyHist.map((h: any) => {
                    const hTime = new Date(h.tsHourStart).getTime();
                    const price = priceHist.find((p: any) => new Date(p.tsHourStart).getTime() === hTime);
                    const exportPrice = price
                        ? (price.exportDollarsPerKWH ?? price.dollarsPerKWH ?? 0)
                        : 0;
                    return {
                        ts: h.tsHourStart,
                        hour: new Date(h.tsHourStart).getHours(),
                        plannedSOC: h.avgBatterySOC,
                        batterySOCIfUsed: h.avgBatterySOC,
                        batteryReserveSOC: periods[0]?.reserveSOC,
                        historySOC: h.avgBatterySOC,
                        predictedSolarKWH: h.solarKWH || 0,
                        avgHomeLoadKWH: Math.floor((h.homeLoadKWH || 0) * 10) / 10,
                        gridChargeDollarsPerKWH: price ? price.dollarsPerKWH + (price.gridUseDollarsPerKWH || 0) : 0,
                        gridExportDollarsPerKWH: exportPrice,
                        isHistory: true,
                        zone: 'Historical Actual',
                        zoneColor: 'var(--text-muted)',
                    };
                });
                planData = [...historyMapped];
            }

            const periodZones = periods.map((p) => classifyPlanPeriod(p));

            // Base hourly rates for each period (used for solar and home load interpolation)
            const periodRates = periods.map((p) => {
                const solarRate = p.durationHours > 0 ? (p.solarKWH || 0) / p.durationHours : (p.solarKWH || 0);
                const loadRate = p.durationHours > 0 ? (p.loadKWH || 0) / p.durationHours : (p.loadKWH || 0);
                return {
                    startTimeMs: new Date(p.tsStart).getTime(),
                    endTimeMs: new Date(p.tsEnd).getTime(),
                    solarRate,
                    loadRate,
                };
            });

            const getBaseRateAt = (targetMs: number) => {
                if (targetMs <= periodRates[0].startTimeMs) {
                    return periodRates[0];
                }
                const lastRate = periodRates[periodRates.length - 1];
                if (targetMs >= lastRate.endTimeMs) {
                    return lastRate;
                }
                for (let i = 0; i < periodRates.length; i++) {
                    if (targetMs >= periodRates[i].startTimeMs && targetMs < periodRates[i].endTimeMs) {
                        return periodRates[i];
                    }
                }
                return lastRate;
            };

            const getInterpolatedRates = (ts: string | Date) => {
                const d = new Date(ts);
                const tsMs = d.getTime();
                const msIntoHour = (d.getMinutes() * 60 + d.getSeconds()) * 1000 + d.getMilliseconds();
                if (msIntoHour === 0) {
                    return getBaseRateAt(tsMs);
                }
                const hourStartMs = tsMs - msIntoHour;
                const nextHourMs = hourStartMs + 3600000;
                const fraction = msIntoHour / 3600000;

                const currentRate = getBaseRateAt(hourStartMs);
                const nextRate = getBaseRateAt(nextHourMs);

                return {
                    solarRate: (1 - fraction) * currentRate.solarRate + fraction * nextRate.solarRate,
                    loadRate: (1 - fraction) * currentRate.loadRate + fraction * nextRate.loadRate,
                };
            };

            periods.forEach((p, idx) => {
                const z = periodZones[idx];
                const rates = getInterpolatedRates(p.tsStart);

                const pt: any = {
                    ts: p.tsStart,
                    hour: new Date(p.tsStart).getHours(),
                    plannedSOC: p.startSoc,
                    batterySOCIfUsed: p.startSoc,
                    batteryReserveSOC: p.reserveSOC,
                    predictedSolarKWH: rates.solarRate,
                    avgHomeLoadKWH: Math.floor(rates.loadRate * 10) / 10,
                    gridChargeDollarsPerKWH: p.importDollars ?? 0,
                    gridExportDollarsPerKWH: p.exportDollars ?? 0,
                    isHistory: false,
                    period: p,
                    zone: z.label,
                    zoneColor: z.color,
                    [`${z.key}SOC`]: p.startSoc,
                };

                // Connect previous period's line to this start point for seamless transition
                if (idx > 0) {
                    const prevZ = periodZones[idx - 1];
                    if (prevZ.key !== z.key) {
                        pt[`${prevZ.key}SOC`] = p.startSoc;
                    }
                } else if (planData.length > 0) {
                    // Connect history line to plan start
                    pt.historySOC = p.startSoc;
                }

                planData.push(pt);

                if (idx === periods.length - 1) {
                    const endRates = getInterpolatedRates(p.tsEnd);
                    const endPt: any = {
                        ts: p.tsEnd,
                        hour: new Date(p.tsEnd).getHours(),
                        plannedSOC: p.endSoc,
                        batterySOCIfUsed: p.endSoc,
                        batteryReserveSOC: p.reserveSOC,
                        predictedSolarKWH: endRates.solarRate,
                        avgHomeLoadKWH: Math.floor(endRates.loadRate * 10) / 10,
                        gridChargeDollarsPerKWH: p.importDollars ?? 0,
                        gridExportDollarsPerKWH: p.exportDollars ?? 0,
                        isHistory: false,
                        zone: z.label,
                        zoneColor: z.color,
                        [`${z.key}SOC`]: p.endSoc,
                    };
                    planData.push(endPt);
                }
            });

            return planData;
        }

        let modelingData: any[] = rawModelingData.simulation || [];

        if (includeHistory && rawModelingData.energyHistory && rawModelingData.priceHistory) {
            const energyHist = rawModelingData.energyHistory || [];
            const priceHist = rawModelingData.priceHistory || [];

            // The battery capacity for history is best estimated from the first simulation hour
            // or assumed from the context, here we use the first sim hour's capacity.
            const firstSim = modelingData[0];
            const capacity = firstSim ? firstSim.batteryCapacityKWH : 10;
            const reserve = firstSim ? firstSim.batteryReserveKWH : 0;

            const historyMapped = energyHist.map((h: any) => {
                const hTime = new Date(h.tsHourStart).getTime();
                const price = priceHist.find((p: any) => new Date(p.tsHourStart).getTime() === hTime);
                const exportPrice = price
                    ? (price.exportDollarsPerKWH ?? price.dollarsPerKWH ?? 0)
                    : 0;
                return {
                    ts: h.tsHourStart,
                    hour: new Date(h.tsHourStart).getHours(),
                    batteryKWH: (h.avgBatterySOC / 100) * capacity,
                    batteryCapacityKWH: capacity,
                    batteryReserveSOC: reserve,
                    predictedSolarKWH: h.solarKWH,
                    todaySolarTrend: 1.0, // Used for raw solar calc below
                    avgHomeLoadKWH: h.homeLoadKWH || 0,
                    gridChargeDollarsPerKWH: price ? price.dollarsPerKWH + (price.gridUseDollarsPerKWH || 0) : 0,
                    gridExportDollarsPerKWH: exportPrice,
                    netLoadSolarKWH: -h.solarKWH,
                    solarOppDollarsPerKWH: exportPrice,
                    isHistory: true,
                };
            });

            modelingData = [...historyMapped, ...modelingData];
        }

        // Pre-process data
        return modelingData.map((h: any, idx: number) => {
            const prevH = idx > 0 ? modelingData[idx - 1] : null;
            const predictedSolarKWH = prevH ? prevH.predictedSolarKWH : 0;
            const avgHomeLoadKWH = prevH ? prevH.avgHomeLoadKWH : 0;
            const todaySolarTrend = prevH ? prevH.todaySolarTrend : 1.0;

            let displayBatteryKWH = h.batteryKWH;
            if (!h.isHistory) {
                displayBatteryKWH = h.startBatteryKWH !== undefined ? h.startBatteryKWH : h.batteryKWH;
            }
            return {
                ...h,
                batterySOCIfUsed: (displayBatteryKWH / h.batteryCapacityKWH) * 100,
                batteryReserveSOC: (h.batteryReserveKWH / h.batteryCapacityKWH) * 100,
                predictedSolarKWH,
                solarTrendRatio: todaySolarTrend > 0 && todaySolarTrend !== 1.0
                    ? todaySolarTrend
                    : 0,
                avgHomeLoadKWH: Math.floor((avgHomeLoadKWH || 0) * 10) / 10,
                gridExportDollarsPerKWH: h.gridExportDollarsPerKWH ?? h.solarOppDollarsPerKWH ?? 0,
            };
        });
    }, [rawModelingData, includeHistory, isPlanActive]);

    if (loading) return (
        <div className="loading-screen">
            <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
            Loading simulation…
        </div>
    );
    if (error) return <div className="error">Error: {error}</div>;

    const hasFault = Boolean(rawModelingData?.latestAction?.fault);
    const faultDescription = rawModelingData?.latestAction?.description || 'A system fault is currently active.';

    const activeCharts = isPlanActive ? planCharts : charts;

    return (
        <div className="content-container forecast-page">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <h2>{isPlanActive ? '24-Hour Energy Plan' : '24-Hour Simulation'}</h2>
                <Field.Root className="form-group switch-group compact" style={{ margin: 0 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem', color: '#4b5563' }}>
                        <Switch.Root
                             id="showHistoryToggle"
                             checked={includeHistory}
                             onCheckedChange={(checked) => setIncludeHistory(checked)}
                             className="switch-root"
                        >
                            <Switch.Thumb className="switch-thumb" />
                        </Switch.Root>
                        <Field.Label htmlFor="showHistoryToggle" style={{ cursor: 'pointer' }}>Show Previous 24 Hours</Field.Label>
                    </div>
                </Field.Root>
            </div>
            {hasFault && (
                <div className="banner error-banner" data-testid="forecast-fault-alert" style={{ marginTop: '1rem', marginBottom: '1rem' }}>
                    <p>
                        <strong>System Fault:</strong> {faultDescription}. Automated planning is suspended until the fault is resolved.
                    </p>
                </div>
            )}
            {!data.length ? (
                <div className="no-actions">
                    {hasFault
                        ? 'No forecast data available while system is in a fault state.'
                        : 'No simulation data available.'}
                </div>
            ) : (
                <>
                    {isPlanActive ? (
                        <>
                            <p className="forecast-subtitle">
                                Optimal battery dispatch schedule generated {(() => {
                                    const startTs = rawModelingData?.plan?.tsCreated || rawModelingData?.updated;
                                    return startTs ? formatTime(startTs, data[0]?.ts) : '';
                                })()} ({rawModelingData?.plan?.horizonHours ?? 24}-Hour Horizon)
                            </p>
                            <div className="forecast-hero-grid">
                                <div className="forecast-stat-card">
                                    <span className="forecast-stat-label">Projected Grid Cost</span>
                                    <span className="forecast-stat-value">
                                        ${(rawModelingData?.plan?.totalProjectedCost ?? 0).toFixed(2)}
                                    </span>
                                    <span className="forecast-stat-sublabel">Anticipated grid electricity cost</span>
                                </div>
                                <div className="forecast-stat-card">
                                    <span className="forecast-stat-label">Projected Export Credits</span>
                                    <span className="forecast-stat-value">
                                        ${(rawModelingData?.plan?.totalExportCredits ?? 0).toFixed(2)}
                                    </span>
                                    <span className="forecast-stat-sublabel">Anticipated export credits</span>
                                </div>
                            </div>
                        </>
                    ) : (
                        <p className="forecast-subtitle">
                            Predicted energy state <strong>assuming no action is taken</strong> starting from{' '}
                            {(() => {
                                const startTs = rawModelingData?.updated || rawModelingData?.simulation?.[0]?.ts;
                                return startTs ? formatTime(startTs, data[0]?.ts) : '';
                            })()}
                        </p>
                    )}
                    <div className="forecast-charts">
                        {activeCharts.map((config) => {
                            const headerAction = (!isPlanActive && config.dataKey === 'avgHomeLoadKWH') ? (
                                <Field.Root className="form-group switch-group compact" style={{ margin: 0 }}>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                                        <Switch.Root
                                             id="loadPredictionModeToggle"
                                             aria-label="Conservative"
                                             checked={loadPredictionMode === 'conservative'}
                                             onCheckedChange={(checked) => setLoadPredictionMode(checked ? 'conservative' : 'default')}
                                             className="switch-root"
                                        >
                                            <Switch.Thumb className="switch-thumb" />
                                        </Switch.Root>
                                        <span style={{ color: loadPredictionMode === 'conservative' ? 'var(--primary)' : 'inherit', fontWeight: loadPredictionMode === 'conservative' ? 600 : 400 }}>Conservative</span>
                                    </div>
                                </Field.Root>
                            ) : undefined;
                            return (
                                <ForecastChart
                                    key={config.dataKey}
                                    data={data}
                                    config={config}
                                    isMobile={isMobile}
                                    showCurrentTime={includeHistory}
                                    nowMs={nowMs}
                                    headerAction={headerAction}
                                    planPeriods={rawModelingData?.plan?.periods}
                                />
                            );
                        })}
                    </div>
                </>
            )}
        </div>
    );
}

export default Forecast;
