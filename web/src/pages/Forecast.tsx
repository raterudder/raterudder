import React, { useEffect, useState } from 'react';
import { fetchModeling } from '../api';
import type { ModelingHour } from '../api';
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
        color: '#3b82f6',
        gradientId: 'batteryGrad',
        unit: '%',
        referenceLine: { dataKey: 'batteryReserveSOC', label: 'Reserve', color: '#ef4444' },
    },
    {
        title: 'Battery (if standby) (%)',
        dataKey: 'batterySOCIfStandby',
        color: '#3b82f6',
        gradientId: 'batteryGrad',
        unit: '%',
        referenceLine: { dataKey: 'batteryReserveSOC', label: 'Reserve', color: '#ef4444' },
    },
    {
        title: 'Predicted Solar (kWh)',
        dataKey: 'predictedSolarKWH',
        color: '#f59e0b',
        gradientId: 'solarGrad',
        unit: ' kWh',
        additionalLines: [
            { dataKey: 'rawSolarKWH', color: '#9ca3af', strokeDasharray: '4 4' },
        ],
    },
    {
        title: 'Forecasted Solar Radiation (W/m²)',
        dataKey: 'forecastGHI',
        color: '#fbbf24', // lighter orange/yellow
        gradientId: 'ghiGrad',
        unit: ' W/m²',
        additionalLines: [
            { dataKey: 'actualGHI', color: '#ea580c', strokeDasharray: '0' }, // darker orange for actuals
        ],
    },
    {
        title: 'Avg Home Load (kWh)',
        dataKey: 'avgHomeLoadKWH',
        color: '#8b5cf6',
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
}

function ForecastChart({ data, config, isMobile, showCurrentTime }: { data: ProcessedModelingHour[]; config: ChartConfig; isMobile: boolean; showCurrentTime: boolean }) {
    // Compute reference value if applicable
    const refValue = config.referenceLine
        ? (data[0]?.[config.referenceLine.dataKey as keyof ProcessedModelingHour] as number)
        : undefined;

    const currentTimeStr = React.useMemo(() => {
        if (!showCurrentTime || data.length === 0) return undefined;
        // The last history element is typically right before the forecast starts,
        // or we can find the exact transition point if we had a flag.
        // We can just use "now" rounded to the current hour as an approximation,
        // but it's more accurate to find the first item where it's a forecast.
        // Since we prepend 24 items of history, let's just use the timestamp
        // of the item that corresponds to `now` (the 24th or 25th item).
        const nowMs = Date.now();
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
    }, [data, showCurrentTime]);

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
                    <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                    <XAxis
                        dataKey="ts"
                        tickFormatter={formatHour}
                        tick={{ fontSize: isMobile ? 10 : 12 }}
                        stroke="#9ca3af"
                    />
                    <YAxis
                        tick={{ fontSize: isMobile ? 10 : 12 }}
                        stroke="#9ca3af"
                        width={isMobile ? 35 : 50}
                        tickFormatter={(v: number) =>
                            config.unit.includes('$') ? `$${v.toFixed(2)}` : v.toFixed(1)
                        }
                    />
                    <Tooltip
                        labelFormatter={(label) => formatHour(String(label))}
                        formatter={(value: number | string | undefined, name: string | number | undefined) => {
                            const v = Number(value ?? 0);
                            // Determine which config/line this is
                            const lineUnit = config.unit;
                            // If it's the solar raw line, it uses the same unit

                            return [
                                config.unit.includes('$')
                                    ? `$${v.toFixed(4)}`
                                    : v.toFixed(2) + lineUnit.trim(),
                                name === 'rawSolarKWH' ? 'Raw Model' : (name === 'actualGHI' ? 'Actual' : config.title), // Simple label mapping
                            ];
                        }}
                        contentStyle={{
                            backgroundColor: '#fff',
                            border: '1px solid #e5e7eb',
                            borderRadius: '8px',
                            boxShadow: '0 2px 8px rgba(0,0,0,0.08)',
                        }}
                    />
                    <Area
                        type="monotone"
                        dataKey={config.dataKey}
                        stroke={config.color}
                        strokeWidth={2}
                        fill={`url(#${config.gradientId})`}
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
                            stroke="#4b5563"
                            strokeDasharray="3 3"
                            label={{
                                value: 'Now',
                                position: 'insideTopLeft',
                                fill: '#4b5563',
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
    const [data, setData] = useState<ProcessedModelingHour[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);
    const [includeHistory, setIncludeHistory] = useState(false);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    useEffect(() => {
        const loadData = async () => {
            try {
                setLoading(true);
                const forecastData = await fetchModeling(siteID, includeHistory);

                // Combine history and simulation if includeHistory is true
                let modelingData = forecastData.simulation || forecastData || [];

                const weatherHist = forecastData.weather || [];

                // We also need to map the weather to the simulation hours
                modelingData = modelingData.map((sim: any) => {
                    const weather = weatherHist.find((w: any) => w.tsHourStart === sim.ts);
                    if (weather) {
                        return {
                            ...sim,
                            actualGHI: weather.actualGHI,
                            forecastGHI: weather.forecastGHI,
                        };
                    }
                    return sim;
                });

                if (includeHistory && forecastData.energyHistory && forecastData.priceHistory) {
                    const energyHist = forecastData.energyHistory || [];
                    const priceHist = forecastData.priceHistory || [];

                    // The battery capacity for history is best estimated from the first simulation hour
                    // or assumed from the context, here we use the first sim hour's capacity.
                    const firstSim = modelingData[0];
                    const capacity = firstSim ? firstSim.batteryCapacityKWH : 10;
                    const reserve = firstSim ? firstSim.batteryReserveKWH : 0;

                    const historyMapped = energyHist.map((h: any) => {
                        const price = priceHist.find((p: any) => p.tsHourStart === h.tsHourStart);
                        const weather = weatherHist.find((w: any) => w.tsHourStart === h.tsHourStart);
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
                            gridChargeDollarsPerKWH: price ? price.gridUseDollarsPerKWH || price.dollarsPerKWH : 0,
                            netLoadSolarKWH: -h.solarKWH,
                            solarOppDollarsPerKWH: 0,
                            actualGHI: weather?.actualGHI,
                            forecastGHI: weather?.forecastGHI,
                        };
                    });

                    modelingData = [...historyMapped, ...modelingData];
                }

                // Pre-process data
                const processed = modelingData.map((h: any) => ({
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
                setData(processed);
            } catch (err: any) {
                setError(err.message || 'Unknown error');
            } finally {
                setLoading(false);
            }
        };

        loadData();
    }, [siteID, includeHistory]);

    if (loading) return <div className="forecast-loading">Loading simulation…</div>;
    if (error) return <div className="error">Error: {error}</div>;
    if (!data.length) return <div className="no-actions">No simulation data available.</div>;

    return (
        <div className="content-container forecast-page">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <h2>24-Hour Simulation</h2>
                <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem', color: '#4b5563', cursor: 'pointer' }}>
                    <input
                        type="checkbox"
                        checked={includeHistory}
                        onChange={(e) => setIncludeHistory(e.target.checked)}
                        style={{ accentColor: '#3b82f6', width: '16px', height: '16px', cursor: 'pointer' }}
                    />
                    Show Previous 24 Hours
                </label>
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
                {charts.map((c) => (
                    <ForecastChart key={c.dataKey} data={data} config={c} isMobile={isMobile} showCurrentTime={includeHistory} />
                ))}
            </div>
        </div>
    );
}

export default Forecast;
