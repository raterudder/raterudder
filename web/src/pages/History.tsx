import React, { useEffect, useState, useMemo, useRef, useCallback } from 'react';
import { useLocation, useSearch } from 'wouter';
import { fetchHistoryEnergy, fetchSettings } from '../api';
import type { EnergyStats, Settings as SettingsType, WeatherRes } from '../api';
import DateSelector from '../components/DateSelector';
import {
    ResponsiveContainer,
    AreaChart,
    Area,
    XAxis,
    YAxis,
    CartesianGrid,
    Tooltip,
    Line,
} from 'recharts';
import './History.css';

interface HistoryDataPoint extends EnergyStats {
    irradiance?: number;
    improvedSolarGeneration?: number;
    improvedHomeLoad?: number;
}

type ChartConfig = {
    title: string;
    dataKeys: { key: string; color: string; label: string; type: 'area' | 'line'; fill?: string; strokeDasharray?: string }[];
    unit: string;
};

const historyCharts: ChartConfig[] = [
    {
        title: 'Battery (%)',
        unit: '%',
        dataKeys: [
            { key: 'maxBatterySOC', color: 'var(--accent)', label: 'Max SOC', type: 'area', fill: 'var(--accent-variant)' }
        ]
    },
    {
        title: 'Solar Generation (kWh)',
        unit: ' kWh',
        dataKeys: [
            { key: 'solarKWH', color: 'var(--warning)', label: 'Actual Solar', type: 'area' },
            { key: 'improvedSolarGeneration', color: '#ff8a00', label: 'Forecast Solar', type: 'line', strokeDasharray: '4 4' }
        ]
    },
    {
        title: 'Estimated Irradiance',
        unit: ' W/m²',
        dataKeys: [
            { key: 'irradiance', color: '#60a5fa', label: 'Irradiance', type: 'area' }
        ]
    },
    {
        title: 'Home Load (kWh)',
        unit: ' kWh',
        dataKeys: [
            { key: 'homeKWH', color: '#a855f7', label: 'Home Load', type: 'area' }
        ]
    },
    {
        title: 'Grid (kWh)',
        unit: ' kWh',
        dataKeys: [
            { key: 'gridImportKWH', color: '#ef4444', label: 'Import', type: 'line' },
            { key: 'gridExportKWH', color: '#10b981', label: 'Export', type: 'line' }
        ]
    }
];

import { formatTime, getSiteTodayDate, isSameCalendarDate } from '../utils/dashboardUtils';

function formatHour(ts: string, referenceTs?: string, timeZone?: string): string {
    if (!ts) return '';
    const formatted = formatTime(ts, referenceTs, timeZone);
    return formatted.replace(':00 ', ' ');
}

const CHART_MARGIN_MOBILE = { top: 5, right: 0, left: 0, bottom: 5 };
const CHART_MARGIN_DESKTOP = { top: 5, right: 20, left: 0, bottom: 5 };
const AXIS_TICK_MOBILE = { fontSize: 10 };
const AXIS_TICK_DESKTOP = { fontSize: 12 };
const TOOLTIP_CONTENT_STYLE: React.CSSProperties = {
    backgroundColor: 'var(--surface-container-high)',
    border: '1px solid var(--border)',
    borderRadius: '8px',
    boxShadow: 'var(--shadow-lg)',
    color: 'var(--on-surface)',
    backdropFilter: 'blur(10px)',
};
const TOOLTIP_ITEM_STYLE: React.CSSProperties = { color: 'var(--on-surface)' };
const TOOLTIP_LABEL_STYLE: React.CSSProperties = {
    color: 'var(--text-muted)',
    marginBottom: '4px',
    fontWeight: 700,
};

function formatHistoryYAxisTick(v: number): string {
    return v.toFixed(1);
}

interface HistoryChartProps {
    data: HistoryDataPoint[];
    config: ChartConfig;
    isMobile: boolean;
    timeZone?: string;
}

const HistoryChart = React.memo(function HistoryChart({
    data,
    config,
    isMobile,
    timeZone,
}: HistoryChartProps) {
    const firstTs = data[0]?.tsHourStart;

    const formatXAxisTick = useCallback(
        (ts: string) => formatHour(ts, firstTs, timeZone),
        [firstTs, timeZone]
    );

    const formatTooltipLabel = useCallback(
        (label: any) => formatHour(String(label), firstTs, timeZone),
        [firstTs, timeZone]
    );

    const areaDataKeys = useMemo(
        () => config.dataKeys.filter((dk) => dk.type === 'area'),
        [config.dataKeys]
    );

    const lineDataKeys = useMemo(
        () => config.dataKeys.filter((dk) => dk.type === 'line'),
        [config.dataKeys]
    );

    return (
        <div className="chart-card">
            <h3>{config.title}</h3>
            <ResponsiveContainer width="100%" height={240}>
                <AreaChart data={data} syncId="history" margin={isMobile ? CHART_MARGIN_MOBILE : CHART_MARGIN_DESKTOP}>
                    <defs>
                        {config.dataKeys.map(dk => (
                             <linearGradient key={dk.key} id={`grad-${dk.key}`} x1="0" y1="0" x2="0" y2="1">
                                <stop offset="5%" stopColor={dk.color} stopOpacity={0.3} />
                                <stop offset="95%" stopColor={dk.color} stopOpacity={0.02} />
                            </linearGradient>
                        ))}
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--outline-variant)" vertical={false} opacity={0.4} />
                    <XAxis
                        dataKey="tsHourStart"
                        tickFormatter={formatXAxisTick}
                        tick={isMobile ? AXIS_TICK_MOBILE : AXIS_TICK_DESKTOP}
                        stroke="var(--outline-variant)"
                        axisLine={false}
                        tickLine={false}
                    />
                    <YAxis
                        tick={isMobile ? AXIS_TICK_MOBILE : AXIS_TICK_DESKTOP}
                        stroke="var(--outline-variant)"
                        width={isMobile ? 35 : 50}
                        axisLine={false}
                        tickLine={false}
                        tickFormatter={formatHistoryYAxisTick}
                    />
                    <Tooltip
                        labelFormatter={formatTooltipLabel}
                        contentStyle={TOOLTIP_CONTENT_STYLE}
                        itemStyle={TOOLTIP_ITEM_STYLE}
                        labelStyle={TOOLTIP_LABEL_STYLE}
                    />
                    {areaDataKeys.map(dk => (
                        <Area
                            key={dk.key}
                            type="monotone"
                            dataKey={dk.key}
                            stroke={dk.color}
                            name={dk.label}
                            strokeWidth={3}
                            fill={`url(#grad-${dk.key})`}
                            isAnimationActive={true}
                        />
                    ))}
                    {lineDataKeys.map(dk => (
                        <Line
                            key={dk.key}
                            type="monotone"
                            dataKey={dk.key}
                            name={dk.label}
                            stroke={dk.color}
                            strokeWidth={dk.strokeDasharray ? 2 : 3}
                            strokeDasharray={dk.strokeDasharray}
                            dot={false}
                        />
                    ))}
                </AreaChart>
            </ResponsiveContainer>
        </div>
    );
});

const History: React.FC<{ siteID?: string; settings?: SettingsType | null }> = ({ siteID, settings: settingsProp = null }) => {
    const [location, setLocation] = useLocation();
    const searchString = useSearch();
    const searchParams = new URLSearchParams(searchString);
    const dateParam = searchParams.get('date');

    const [data, setData] = useState<HistoryDataPoint[]>([]);
    const [settings, setSettings] = useState<SettingsType | null>(settingsProp);
    const settingsRef = useRef<SettingsType | null>(settingsProp);
    const loadedSiteIDRef = useRef<string | undefined>(settingsProp ? siteID : undefined);
    const isFirstMount = useRef(true);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);

    const effectiveSettings = settingsProp ?? settings;
    const siteTimeZone = data[0]?.timeLocation || effectiveSettings?.location?.timeZone;

    const todayDate = useMemo(() => getSiteTodayDate(siteTimeZone), [siteTimeZone]);

    const currentDate = useMemo(() => {
        if (!dateParam) return todayDate;
        const d = new Date(dateParam + 'T12:00:00'); // Use noon to avoid TZ issues
        return isNaN(d.getTime()) ? todayDate : d;
    }, [dateParam, todayDate]);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    useEffect(() => {
        let isCancelled = false;
        const delay = isFirstMount.current ? 0 : 200;
        isFirstMount.current = false;

        const timer = setTimeout(async () => {
            setLoading(true);
            setError(null);
            try {
                const shouldFetchSettings = settingsRef.current === null || loadedSiteIDRef.current !== siteID;
                const fetchSettingsPromise = shouldFetchSettings
                    ? fetchSettings(siteID)
                    : Promise.resolve(settingsRef.current);

                const [res, s] = await Promise.all([
                    fetchHistoryEnergy(currentDate, siteID),
                    fetchSettingsPromise
                ]);
                if (isCancelled) return;
                setSettings(s);
                settingsRef.current = s;
                loadedSiteIDRef.current = siteID;

                // Merge energy and weather
                const weatherByHourMs = new Map<number, WeatherRes>();
                for (const weather of res.weather || []) {
                    const wTime = new Date(weather.tsHourStart).getTime();
                    if (!isNaN(wTime) && !weatherByHourMs.has(wTime)) {
                        weatherByHourMs.set(wTime, weather);
                    }
                }

                const mergedWithTime = (res.energy || []).map((e) => {
                    const eTime = new Date(e.tsHourStart).getTime();
                    const w = !isNaN(eTime) ? weatherByHourMs.get(eTime) : undefined;
                    const point: HistoryDataPoint = {
                        ...e,
                        irradiance: w?.irradiance,
                        improvedSolarGeneration: w?.improvedSolarGeneration,
                        improvedHomeLoad: w?.improvedHomeLoad !== undefined ? Math.floor(w.improvedHomeLoad * 10) / 10 : undefined,
                        homeKWH: Math.floor((e.homeKWH || 0) * 10) / 10,
                        gridImportKWH: Math.floor((e.gridImportKWH || 0) * 10) / 10,
                        gridExportKWH: Math.floor((e.gridExportKWH || 0) * 10) / 10,
                    };
                    return { point, timeMs: eTime };
                });

                mergedWithTime.sort((a, b) => a.timeMs - b.timeMs);
                setData(mergedWithTime.map((entry) => entry.point));
            } catch (err) {
                if (!isCancelled) {
                    setError(err instanceof Error ? err.message : 'Failed to load history');
                }
            } finally {
                if (!isCancelled) {
                    setLoading(false);
                }
            }
        }, delay);

        return () => {
            isCancelled = true;
            clearTimeout(timer);
        };
    }, [currentDate, siteID]);

    const handleDateChange = (days: number) => {
        const newDate = new Date(currentDate);
        newDate.setDate(newDate.getDate() + days);
        const year = newDate.getFullYear();
        const month = String(newDate.getMonth() + 1).padStart(2, '0');
        const day = String(newDate.getDate()).padStart(2, '0');
        setLocation(`${location}?date=${year}-${month}-${day}`);
    };

    const chartsToRender = useMemo(() => {
        const filtered = historyCharts.map(c => {
            if (c.title === 'Home Load (kWh)' && effectiveSettings?.release === 'staging') {
                return {
                    ...c,
                    dataKeys: [
                        { key: 'homeKWH', color: '#a855f7', label: 'Home Load', type: 'area' as const },
                        { key: 'improvedHomeLoad', color: '#ec4899', label: 'Forecast Load', type: 'line' as const, strokeDasharray: '4 4' }
                    ]
                };
            }
            return c;
        }).filter(c => {
            if (c.dataKeys.some(dk => dk.key === 'irradiance') && effectiveSettings?.release !== 'staging') return false;
            return true;
        });

        return filtered;
    }, [effectiveSettings]);

    const isToday = isSameCalendarDate(currentDate, todayDate);

    return (
        <div className="content-container history-page">
            <header className="header">
                <DateSelector
                    currentDate={currentDate}
                    onDateChange={handleDateChange}
                    loading={loading}
                    isToday={isToday}
                />
            </header>

            {loading && (
                <div className="loading-screen">
                    <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
                    Loading history...
                </div>
            )}
            {error && <div className="error">Error: {error}</div>}
            {!loading && !error && data.length === 0 && (
                <div className="no-data">No history found for this date.</div>
            )}

            {!loading && !error && data.length > 0 && (
                <div className="history-charts">
                    {chartsToRender.map(config => (
                        <HistoryChart key={config.title} data={data} config={config} isMobile={isMobile} timeZone={siteTimeZone} />
                    ))}
                </div>
            )}
        </div>
    );
};

export default History;
