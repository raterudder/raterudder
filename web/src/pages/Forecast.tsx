import React, { useEffect, useState, useMemo } from 'react';
import { fetchModeling } from '../api';
import type { ForecastResponse, ModelingHour } from '../api';
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
import './Forecast.css';

type ChartConfig = {
    title: string;
    dataKey: string;
    color: string;
    gradientId: string;
    unit: string;
    referenceLine?: { dataKey: string; label: string; color: string };
    additionalLines?: { dataKey: string; color: string; strokeDasharray?: string }[];
};

const charts: ChartConfig[] = [
    {
        title: 'Battery (if used) (%)',
        dataKey: 'batterySOCIfUsed',
        color: 'var(--accent)',
        gradientId: 'batteryGrad',
        unit: '%',
        referenceLine: { dataKey: 'batteryReserveSOC', label: 'Reserve', color: '#ef4444' },
    },
    {
        title: 'Battery (if standby) (%)',
        dataKey: 'batterySOCIfStandby',
        color: 'var(--accent)',
        gradientId: 'batteryGrad',
        unit: '%',
        referenceLine: { dataKey: 'batteryReserveSOC', label: 'Reserve', color: '#ef4444' },
    },
    {
        title: 'Predicted Solar (kWh)',
        dataKey: 'predictedSolarKWH',
        color: 'var(--warning)',
        gradientId: 'solarGrad',
        unit: ' kWh',
        additionalLines: [
            { dataKey: 'rawSolarKWH', color: 'var(--text-muted)', strokeDasharray: '4 4' },
        ],
    },
    {
        title: 'Improved Predicted Solar (kWh)',
        dataKey: 'improvedSolarGeneration',
        color: '#ff8a00',
        gradientId: 'improvedSolarGrad',
        unit: ' kWh',
        additionalLines: [
            { dataKey: 'unclippedSolarGeneration', color: '#ffb800', strokeDasharray: '4 4' },
        ],
    },
    {
        title: 'Estimated Irradiance (W/m²)',
        dataKey: 'irradiance',
        color: '#60a5fa',
        gradientId: 'irradianceGrad',
        unit: ' W/m²',
    },
    {
        title: 'Avg Home Load (kWh)',
        dataKey: 'avgHomeLoadKWH',
        color: '#a855f7',
        gradientId: 'loadGrad',
        unit: ' kWh',
    },
    {
        title: 'Grid Charge Cost ($/kWh)',
        dataKey: 'gridChargeDollarsPerKWH',
        color: '#10b981',
        gradientId: 'priceGrad',
        unit: ' $/kWh',
    },
];

function formatHour(ts: string): string {
    const d = new Date(ts);
    return d.toLocaleTimeString([], { hour: 'numeric', hour12: true });
}

// Extended interface adding calculated fields
interface ProcessedModelingHour extends ModelingHour {
    batterySOCIfUsed: number;
    batterySOCIfStandby: number;
    batteryReserveSOC: number;
    rawSolarKWH: number;
    irradiance?: number;
    improvedSolarGeneration?: number;
    unclippedSolarGeneration?: number;
    temperatureC?: number;
    snowfallCM?: number;
}

function ForecastChart({ data, config, isMobile, showCurrentTime, nowMs }: { data: ProcessedModelingHour[]; config: ChartConfig; isMobile: boolean; showCurrentTime: boolean; nowMs: number }) {
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

    return (
        <div className="forecast-chart-card">
            <h3>{config.title}</h3>
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
                        tickFormatter={formatHour}
                        tick={{ fontSize: isMobile ? 10 : 12, fill: 'var(--text-muted)' }}
                        stroke="var(--outline-variant)"
                        axisLine={false}
                        tickLine={false}
                    />
                    <YAxis
                        tick={{ fontSize: isMobile ? 10 : 12, fill: 'var(--text-muted)' }}
                        stroke="var(--outline-variant)"
                        width={isMobile ? 35 : 50}
                        axisLine={false}
                        tickLine={false}
                        tickFormatter={(v: number) =>
                            config.unit.includes('$') ? `$${v.toFixed(2)}` : v.toFixed(1)
                        }
                    />
                    <Tooltip
                        labelFormatter={(label) => formatHour(String(label))}
                        formatter={(value: number | string | undefined, name: string | number | undefined) => {
                            const v = Number(value ?? 0);
                            const lineUnit = config.unit;
                            return [
                                config.unit.includes('$')
                                    ? `$${v.toFixed(4)}`
                                    : v.toFixed(2) + lineUnit.trim(),
                                name === 'rawSolarKWH' ? 'Raw Model' : name === 'unclippedSolarGeneration' ? 'Unclipped Potential' : name === 'irradiance' ? 'Irradiance' : config.title,
                            ];
                        }}
                        contentStyle={{
                            backgroundColor: 'var(--surface-container-high)',
                            border: '1px solid var(--border)',
                            borderRadius: '8px',
                            boxShadow: 'var(--shadow-lg)',
                            color: 'var(--on-surface)',
                            backdropFilter: 'blur(10px)',
                        }}
                        itemStyle={{ color: 'var(--on-surface)' }}
                        labelStyle={{ color: 'var(--text-muted)', marginBottom: '4px', fontWeight: 700 }}
                    />
                    <Area
                        type="monotone"
                        dataKey={config.dataKey}
                        stroke={config.color}
                        strokeWidth={3}
                        fill={`url(#${config.gradientId})`}
                        isAnimationActive={true}
                    />
                    {config.additionalLines?.map((line) => (
                        <Line
                            key={line.dataKey}
                            type="monotone"
                            dataKey={line.dataKey}
                            stroke={line.color}
                            strokeWidth={2}
                            strokeDasharray={line.strokeDasharray}
                            dot={false}
                        />
                    ))}
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
                </AreaChart>
            </ResponsiveContainer>
        </div>
    );
}

const Forecast: React.FC<{ siteID?: string }> = ({ siteID }) => {
    const [rawModelingData, setRawModelingData] = useState<ForecastResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [nowMs] = useState(() => Date.now());
    const [error, setError] = useState<string | null>(null);
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);
    const [includeHistory, setIncludeHistory] = useState(false);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    useEffect(() => {
        const loadRawData = async () => {
            setLoading(true);
            try {
                setRawModelingData(await fetchModeling(siteID));
            } catch (error) {
                setError(error instanceof Error ? error.message : 'Unknown error');
            } finally {
                setLoading(false);
            }
        };
        loadRawData();
    }, [siteID]);

    const data = useMemo(() => {
        if (!rawModelingData) return [];

        // Combine history and simulation if includeHistory is true
        // Based on ForecastResponse, simulation is an array.
        let modelingData: any[] = rawModelingData.simulation || [];
        const weatherHist = rawModelingData.weather || [];

        // We also need to map the weather to the simulation hours
        modelingData = modelingData.map((sim: any) => {
            const simDate = new Date(sim.ts);
            simDate.setMinutes(0, 0, 0);
            const simTimeHour = simDate.getTime();
            const weather = weatherHist.find((w: any) => new Date(w.tsHourStart).getTime() === simTimeHour);
            if (weather) {
                return {
                    ...sim,
                    irradiance: weather.irradiance,
                    temperatureC: weather.temperatureC,
                    snowfallCM: weather.snowfallCM,
                    improvedSolarGeneration: weather.improvedSolarGeneration,
                    unclippedSolarGeneration: weather.unclippedSolarGeneration,
                };
            }
            return sim;
        });

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
                const weather = weatherHist.find((w: any) => new Date(w.tsHourStart).getTime() === hTime);
                return {
                    ts: h.tsHourStart,
                    hour: new Date(h.tsHourStart).getHours(),
                    batteryKWH: (h.avgBatterySOC / 100) * capacity,
                    batteryKWHIfStandby: (h.avgBatterySOC / 100) * capacity, // Historic actuals
                    batteryCapacityKWH: capacity,
                    batteryReserveKWH: reserve,
                    predictedSolarKWH: h.solarKWH,
                    todaySolarTrend: 1.0, // Used for raw solar calc below
                    avgHomeLoadKWH: h.homeLoadKWH || 0,
                    gridChargeDollarsPerKWH: price ? price.dollarsPerKWH + (price.gridUseDollarsPerKWH || 0) : 0,
                    netLoadSolarKWH: -h.solarKWH,
                    solarOppDollarsPerKWH: 0,
                    irradiance: weather?.irradiance,
                    temperatureC: weather?.temperatureC,
                    snowfallCM: weather?.snowfallCM,
                    improvedSolarGeneration: weather?.improvedSolarGeneration,
                    unclippedSolarGeneration: weather?.unclippedSolarGeneration,
                };
            });

            modelingData = [...historyMapped, ...modelingData];
        }

        // Pre-process data
        return modelingData.map((h: any) => ({
            ...h,
            batterySOCIfUsed: (h.batteryKWH / h.batteryCapacityKWH) * 100,
            batterySOCIfStandby: (h.batteryKWHIfStandby / h.batteryCapacityKWH) * 100,
            batteryReserveSOC: (h.batteryReserveKWH / h.batteryCapacityKWH) * 100,
            // Avoid division by zero
            rawSolarKWH: h.todaySolarTrend > 0.001
                ? h.predictedSolarKWH / h.todaySolarTrend
                : 0,
            solarTrendRatio: h.todaySolarTrend > 0 && h.todaySolarTrend !== 1.0
                ? h.todaySolarTrend
                : 0,
        }));
    }, [rawModelingData, includeHistory]);

    if (loading) return <div className="forecast-loading">Loading simulation…</div>;
    if (error) return <div className="error">Error: {error}</div>;
    if (!data.length) return <div className="no-actions">No simulation data available.</div>;

    const hasForecastData = data.some(d =>
        (d.irradiance !== undefined && d.irradiance !== null)
    );

    const activeCharts = charts.filter(c => {
        if (c.dataKey === 'irradiance' && !hasForecastData) {
            return false;
        }
        if (c.dataKey === 'improvedSolarGeneration' && !data.some(d => d.improvedSolarGeneration !== undefined)) {
            return false;
        }
        return true;
    });

    return (
        <div className="content-container forecast-page">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <h2>24-Hour Simulation</h2>
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
            <p className="forecast-subtitle">
                Predicted energy state <strong>assuming no action is taken</strong> starting from{' '}
                {new Date(data[0].ts).toLocaleTimeString([], {
                    hour: 'numeric',
                    minute: '2-digit',
                    hour12: true,
                })}
            </p>
            <div className="modeling-charts">
                {activeCharts.map((c) => (
                    <ForecastChart key={c.dataKey} data={data} config={c} isMobile={isMobile} showCurrentTime={includeHistory} nowMs={nowMs} />
                ))}
            </div>
        </div>
    );
}

export default Forecast;
