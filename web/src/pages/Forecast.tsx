import React, { useEffect, useState, useMemo, useRef } from 'react';
import { fetchModeling } from '../api';
import type { ForecastResponse, Settings, PlanPeriod } from '../api';
import { Button } from '@base-ui/react/button';
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
} from 'recharts';
import { HelpButton } from '../components/HelpButton';
import './Forecast.css';

type ChartConfig = {
    title: string;
    dataKey: string;
    color: string;
    gradientId: string;
    unit: string;
    height?: number;
    helpDescription?: React.ReactNode;
    areaType?: 'monotone' | 'step' | 'stepAfter' | 'stepBefore';
    additionalAreas?: { dataKey: string; label?: string; color: string; gradientId: string; type?: 'monotone' | 'step' | 'stepAfter' | 'stepBefore' }[];
    additionalLines?: { dataKey: string; label?: string; color: string; strokeDasharray?: string; type?: 'monotone' | 'step' | 'stepAfter' | 'stepBefore' }[];
};

import {
    ALL_ZONES,
    classifyPlanPeriod,
    buildPlanSolarLoadSeries,
    buildPlanPriceSeries,
    computePlanSummaryStats,
    buildPlanSummaryBeats,
    type PlanSummaryStats,
} from '../utils/forecastUtils';
import { formatTime, getCurrencySymbol, getHourInTimeZone, type DaySummaryBeat } from '../utils/dashboardUtils';
import { SummaryBeatsList } from '../components/ActionTimeline';

const planCharts: ChartConfig[] = [
    {
        title: 'Battery Schedule & SOC (%)',
        dataKey: 'plannedSOC',
        color: '#38bdf8',
        gradientId: 'plannedBatteryGrad',
        unit: '%',
        height: 200,
        helpDescription: (
            <p>
                Displays the planned State of Charge (SOC) and upcoming dispatch schedule of your battery as optimized by RateRudder&apos;s SmartPlan engine.
                The colored line segments highlight scheduled operations (charging from solar, powering home, grid charging, and standby holds).
                The dashed red line represents your Minimum Reserve SOC threshold.
            </p>
        ),
    },
    {
        title: 'Predicted Solar & Home Load (kWh)',
        dataKey: 'predictedSolarKWH',
        color: 'var(--warning)',
        gradientId: 'solarGrad',
        unit: ' kWh',
        height: 180,
        helpDescription: (
            <p>
                Compares expected hourly solar generation against predicted home electricity usage (in kWh).
                When solar rises above home load, surplus solar energy recharges your battery or exports to the grid.
            </p>
        ),
        additionalAreas: [
            {
                dataKey: 'avgHomeLoadKWH',
                label: 'Home Load',
                color: '#a855f7',
                gradientId: 'loadGrad',
                type: 'monotone',
            },
        ],
    },
    {
        title: 'Grid Charge Cost ($/kWh)',
        dataKey: 'gridChargeDollarsPerKWH',
        color: '#10b981',
        gradientId: 'priceGrad',
        unit: ' $/kWh',
        areaType: 'stepAfter',
        height: 140,
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

function formatHour(ts: string, referenceTs?: string, timeZone?: string): string {
    if (!ts) return '';
    const formatted = formatTime(ts, referenceTs, timeZone);
    return formatted.replace(':00 ', ' ');
}

function syncByTimestamp(tooltipTicks: readonly any[], syncState: any): number {
    if (!tooltipTicks || tooltipTicks.length === 0) return -1;
    const activeLabel = syncState?.activeLabel;
    if (activeLabel !== undefined && activeLabel !== null) {
        const exactIdx = tooltipTicks.findIndex((t) => t.value === activeLabel);
        if (exactIdx !== -1) return exactIdx;
        const targetMs = new Date(String(activeLabel)).getTime();
        if (!isNaN(targetMs)) {
            let bestIdx = 0;
            for (let i = 0; i < tooltipTicks.length; i++) {
                const tickMs = new Date(String(tooltipTicks[i].value)).getTime();
                if (!isNaN(tickMs) && tickMs <= targetMs) {
                    bestIdx = i;
                }
            }
            return bestIdx;
        }
    }
    return syncState?.activeTooltipIndex ?? -1;
}

function ForecastChart({
    data,
    config,
    isMobile,
    showCurrentTime,
    nowMs,
    planPeriods,
    planStats,
    scheduleBeats,
    currencySymbol = '$',
    timeZone,
}: {
    data: any[];
    config: ChartConfig;
    isMobile: boolean;
    showCurrentTime: boolean;
    nowMs: number;
    planPeriods?: PlanPeriod[];
    planStats?: PlanSummaryStats | null;
    scheduleBeats?: DaySummaryBeat[];
    currencySymbol?: string;
    timeZone?: string;
}) {
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

    const chartHeight = config.height ?? 200;

    return (
        <div className="chart-card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
                <h3 style={{ margin: 0, display: 'flex', alignItems: 'center' }}>
                    {config.title}
                    {config.helpDescription && (
                        <HelpButton title={config.title} description={config.helpDescription} />
                    )}
                </h3>
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
            {config.dataKey === 'predictedSolarKWH' && config.additionalAreas && config.additionalAreas.length > 0 && (
                <div className="mode-legend" aria-label="Solar and Load Legend">
                    <div className="mode-legend-item">
                        <span className="mode-legend-line" style={{ backgroundColor: config.color }} />
                        <span>Solar</span>
                        {planStats && (
                            <span className="chart-stat-badge" data-testid="solar-total-badge">
                                {planStats.totalSolarKWH.toFixed(1)} kWh
                            </span>
                        )}
                    </div>
                    {config.additionalAreas.map((area) => (
                        <div key={area.dataKey} className="mode-legend-item">
                            <span className="mode-legend-line" style={{ backgroundColor: area.color }} />
                            <span>{area.label || area.dataKey}</span>
                            {planStats && area.dataKey === 'avgHomeLoadKWH' && (
                                <span className="chart-stat-badge" data-testid="load-total-badge">
                                    {planStats.totalLoadKWH.toFixed(1)} kWh
                                </span>
                            )}
                        </div>
                    ))}
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
                    {planStats && (
                        <div className="chart-rate-callouts">
                            <span className="chart-stat-badge" data-testid="rate-low-badge">
                                Low: {currencySymbol}{planStats.minImportDollars.toFixed(2)}/kWh
                            </span>
                            <span className="chart-stat-badge" data-testid="rate-peak-badge">
                                Peak: {currencySymbol}{planStats.maxImportDollars.toFixed(2)}/kWh
                            </span>
                        </div>
                    )}
                </div>
            )}
            <ResponsiveContainer width="100%" height={chartHeight}>
                <AreaChart
                    data={data}
                    syncId="forecast"
                    syncMethod={syncByTimestamp}
                    margin={{ top: 5, right: isMobile ? 0 : 20, left: 0, bottom: 5 }}
                >
                    <defs>
                        <linearGradient id={config.gradientId} x1="0" y1="0" x2="0" y2="1">
                            <stop offset="5%" stopColor={config.color} stopOpacity={0.3} />
                            <stop offset="95%" stopColor={config.color} stopOpacity={0.02} />
                        </linearGradient>
                        {config.additionalAreas?.map((area) => (
                            <linearGradient key={area.gradientId} id={area.gradientId} x1="0" y1="0" x2="0" y2="1">
                                <stop offset="5%" stopColor={area.color} stopOpacity={0.28} />
                                <stop offset="95%" stopColor={area.color} stopOpacity={0.02} />
                            </linearGradient>
                        ))}
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--outline-variant)" vertical={false} opacity={0.4} />
                    <XAxis
                        dataKey="ts"
                        tickFormatter={(ts) => formatHour(ts, data[0]?.ts, timeZone)}
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
                            const timeStr = formatHour(String(label || pt.ts), data[0]?.ts, timeZone);

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
                                const soc = pt.plannedSOC;
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
                            {config.additionalAreas?.map((area) => (
                                <Area
                                    key={area.dataKey}
                                    type={area.type || 'monotone'}
                                    dataKey={area.dataKey}
                                    stroke={area.color}
                                    strokeWidth={2.5}
                                    fill={`url(#${area.gradientId})`}
                                    isAnimationActive={true}
                                />
                            ))}
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
                        </>
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
                </AreaChart>
            </ResponsiveContainer>
            {scheduleBeats && scheduleBeats.length > 0 && (
                <SummaryBeatsList
                    beats={scheduleBeats}
                    className="forecast-plan-beats"
                    data-testid="plan-schedule-beats"
                />
            )}
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
    const loadStrategy = settings?.homeLoadPredictionStrategy === 'conservative' ? 'conservative' : 'default';
    const [loading, setLoading] = useState(true);
    const [regenerating, setRegenerating] = useState(false);
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
    const loadStrategyRef = useRef(loadStrategy);
    loadStrategyRef.current = loadStrategy;
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

    // Initial load on siteID change or settings strategy change: fetch matching forecast.
    useEffect(() => {
        let isCancelled = false;
        const loadInitialData = async () => {
            setLoading(true);
            try {
                const mod = await fetchModeling(siteID, loadStrategy);

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
        loadInitialData();
        return () => {
            isCancelled = true;
        };
    }, [siteID, loadStrategy]);

    // Refresh forecast when returning to page after timeout
    useEffect(() => {
        if (refreshTrigger === 0) return;
        let isCancelled = false;
        isRefreshRef.current = false;

        const refreshData = async () => {
            setLoading(true);
            setError(null);
            try {
                const mod = await fetchModeling(siteIDRef.current, loadStrategyRef.current);
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

    const handleRegeneratePlan = async () => {
        setRegenerating(true);
        setError(null);
        try {
            const mod = await fetchModeling(siteID, loadStrategy, true);
            setRawModelingData(mod);
            setNowMs(Date.now());
            lastFetchTimeRef.current = Date.now();
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Unknown error');
        } finally {
            setRegenerating(false);
        }
    };

    const siteTimeZone =
        rawModelingData?.latestAction?.systemStatus?.timeLocation ||
        settings?.location?.timeZone;

    const data = useMemo(() => {
        if (!rawModelingData) return [];

        const periods = rawModelingData.plan?.periods || [];
        const refTs = periods[0]?.tsStart || rawModelingData.energyHistory?.[0]?.tsHourStart;

        let planData: any[] = [];
        if (includeHistory && rawModelingData.energyHistory && rawModelingData.priceHistory) {
            const energyHist = rawModelingData.energyHistory || [];
            const priceHist = rawModelingData.priceHistory || [];
            planData = energyHist.map((h: any) => {
                const hTime = new Date(h.tsHourStart).getTime();
                const price = priceHist.find((p: any) => new Date(p.tsHourStart).getTime() === hTime);
                const exportPrice = price
                    ? (price.exportDollarsPerKWH ?? price.dollarsPerKWH ?? 0)
                    : 0;
                return {
                    ts: h.tsHourStart,
                    hour: getHourInTimeZone(h.tsHourStart, refTs, siteTimeZone),
                    plannedSOC: h.avgBatterySOC,
                    batteryReserveSOC: periods[0]?.reserveSOC ?? 0,
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
        }

        if (periods.length === 0) {
            return planData;
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
            const msIntoHour = ((tsMs % 3600000) + 3600000) % 3600000;
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
                hour: getHourInTimeZone(p.tsStart, refTs, siteTimeZone),
                plannedSOC: p.startSoc,
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
                    hour: getHourInTimeZone(p.tsEnd, refTs, siteTimeZone),
                    plannedSOC: p.endSoc,
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
    }, [rawModelingData, includeHistory, siteTimeZone]);

    const currencySymbol = getCurrencySymbol(settings?.utilityProvider);

    const solarLoadData = useMemo(() => {
        if (!rawModelingData?.plan) return [];
        return buildPlanSolarLoadSeries(
            rawModelingData.plan.periods,
            rawModelingData.energyHistory,
            includeHistory,
            siteTimeZone
        );
    }, [rawModelingData, includeHistory, siteTimeZone]);

    const priceData = useMemo(() => {
        if (!rawModelingData?.plan) return [];
        return buildPlanPriceSeries(
            rawModelingData.plan.periods,
            rawModelingData.futurePrices,
            rawModelingData.priceHistory,
            includeHistory,
            siteTimeZone
        );
    }, [rawModelingData, includeHistory, siteTimeZone]);

    const planStats = useMemo(() => {
        if (!rawModelingData?.plan) return null;
        return computePlanSummaryStats(
            rawModelingData.plan.periods,
            rawModelingData.futurePrices
        );
    }, [rawModelingData]);

    const planScheduleBeats = useMemo(() => {
        if (!rawModelingData?.plan) return [];
        return buildPlanSummaryBeats(
            rawModelingData.plan.periods,
            currencySymbol,
            siteTimeZone
        );
    }, [rawModelingData, currencySymbol, siteTimeZone]);

    if (loading) return (
        <div className="loading-screen">
            <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
            Loading forecast…
        </div>
    );
    if (error) return <div className="error">Error: {error}</div>;

    const hasFault = Boolean(rawModelingData?.latestAction?.fault);
    const faultDescription = rawModelingData?.latestAction?.description || 'A system fault is currently active.';
    const isStagingSite = typeof window !== 'undefined' && window.location.hostname.includes('staging');

    return (
        <div className="content-container forecast-page">
            <div className="forecast-header">
                <h2>24-Hour Energy Plan</h2>
                <div className="forecast-header-actions">
                    {isStagingSite && (
                        <Button
                            type="button"
                            className="btn btn-secondary"
                            onClick={handleRegeneratePlan}
                            disabled={regenerating}
                        >
                            {regenerating ? 'Regenerating…' : 'Regenerate Plan'}
                        </Button>
                    )}
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
                        : 'No forecast data available.'}
                </div>
            ) : (
                <>
                    {rawModelingData?.plan && (
                        <>
                            <p className="forecast-subtitle">
                                Optimal <strong>SmartPlan</strong> battery dispatch schedule generated {(() => {
                                    const startTs = rawModelingData?.plan?.tsCreated || rawModelingData?.updated;
                                    return startTs ? formatTime(startTs, data[0]?.ts, siteTimeZone) : '';
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
                    )}
                    <div className="forecast-charts">
                        {planCharts.map((config) => {
                            let chartData = data;
                            if (config.dataKey === 'predictedSolarKWH' && solarLoadData.length > 0) {
                                chartData = solarLoadData;
                            } else if (config.dataKey === 'gridChargeDollarsPerKWH' && priceData.length > 0) {
                                chartData = priceData;
                            }

                            return (
                                <ForecastChart
                                    key={config.dataKey}
                                    data={chartData}
                                    config={config}
                                    isMobile={isMobile}
                                    showCurrentTime={includeHistory}
                                    nowMs={nowMs}
                                    planPeriods={rawModelingData?.plan?.periods}
                                    planStats={planStats}
                                    scheduleBeats={config.dataKey === 'plannedSOC' ? planScheduleBeats : undefined}
                                    currencySymbol={currencySymbol}
                                    timeZone={siteTimeZone}
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
