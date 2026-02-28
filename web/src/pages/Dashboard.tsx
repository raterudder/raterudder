import React, { useEffect, useState, useMemo } from 'react';
import { useLocation, useSearch, Link } from 'wouter';
import { BatteryMode, SolarMode, type Action, type SavingsStats, type Settings, fetchActions, fetchSavings, fetchSettings } from '../api';
import CurrentStatus from '../components/CurrentStatus';
import SavingsHero from '../components/SavingsHero';
import ActionTimeline from '../components/ActionTimeline';
import {
    gridChargeCost,
    type ActionSummary,
    type ActionSummaryAccumulator,
    type SummaryType
} from '../utils/dashboardUtils';


const Dashboard: React.FC<{ siteID?: string }> = ({ siteID }) => {
    const [location, navigate] = useLocation();
    const search = useSearch();
    const searchParams = useMemo(() => new URLSearchParams(search), [search]);

    const setSearchParams = (params: Record<string, string>) => {
        const p = new URLSearchParams(search);
        Object.entries(params).forEach(([k, v]) => p.set(k, v));
        navigate(location + "?" + p.toString());
    };

    const dateQuery = searchParams.get('date');
    const [actions, setActions] = useState<Action[]>([]);
    const [savings, setSavings] = useState<SavingsStats | null>(null);
    const [settings, setSettings] = useState<Settings | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    const currentDate = useMemo(() => {
        if (dateQuery) {
            const parts = dateQuery.split('-');
            if (parts.length === 3) {
                const year = parseInt(parts[0], 10);
                const month = parseInt(parts[1], 10) - 1;
                const day = parseInt(parts[2], 10);
                return new Date(year, month, day);
            }
        }
        return new Date();
    }, [dateQuery]);

    useEffect(() => {
        const loadData = async () => {
            setLoading(true);
            setError(null);
            try {
                // Calculate start and end of the day in local time
                const start = new Date(currentDate);
                start.setHours(0, 0, 0, 0);
                const end = new Date(currentDate);
                end.setHours(23, 59, 59, 999);

                // Fetch both actions and savings in parallel
                const [actionsData, savingsData, settingsData] = await Promise.all([
                    siteID === 'ALL' ? Promise.resolve([]) : fetchActions(start, end, siteID),
                    fetchSavings(start, end, siteID),
                    siteID === 'ALL' ? Promise.resolve(null) : fetchSettings(siteID)
                ]);

                setActions(actionsData || []);
                setSavings(savingsData);
                setSettings(settingsData);
            } catch (err) {
                console.error(err);
                setError(err instanceof Error ? err.message : 'Failed to load data');
            } finally {
                setLoading(false);
            }
        };

        loadData();
    }, [currentDate, siteID]);

    const handleDateChange = (days: number) => {
        const newDate = new Date(currentDate);
        newDate.setDate(newDate.getDate() + days);
        const year = newDate.getFullYear();
        const month = String(newDate.getMonth() + 1).padStart(2, '0');
        const day = String(newDate.getDate()).padStart(2, '0');
        setSearchParams({ date: `${year}-${month}-${day}` });
    };

    // Format date for display
    const formattedDate = currentDate.toLocaleDateString(undefined, {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric'
    });

    const isToday = currentDate.toDateString() === new Date().toDateString();
    const latestAction = actions.length > 0 ? actions[actions.length - 1] : null;
    // Filter out paused actions from the displayed timeline — they are captured for
    // status tracking only and should not appear as regular action items.
    const visibleActions = actions.filter(a => !a.paused);

    const groupedActions = useMemo(() => {
        const accumulator: (Action | ActionSummaryAccumulator)[] = [];
        let currentSummary: ActionSummaryAccumulator | null = null;

        for (const action of visibleActions) {
            const isFault = !!action.fault;
            const isNoChange = !isFault && action.batteryMode === BatteryMode.NoChange && action.solarMode === SolarMode.NoChange;
            // currentPrice wasn't always optional so check tsStart as well
            const hasPrice = !!action.currentPrice && action.currentPrice.tsStart !== "0001-01-01T00:00:00Z";
            const price = action.currentPrice ? gridChargeCost(action.currentPrice) : 0;

            const updateSummary = (summary: ActionSummaryAccumulator) => {
                summary.count++;
                if (hasPrice) {
                    summary.hasPrice = true;
                    summary.priceTotal += price;
                    summary.priceCount++;
                    if (summary.min === undefined || price < summary.min) summary.min = price;
                    if (summary.max === undefined || price > summary.max) summary.max = price;
                }
                if (action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0) {
                    summary.hasSOC = true;
                    const soc = action.systemStatus.batterySOC;
                    summary.socTotal += soc;
                    summary.socCount++;
                    if (summary.minSOC === undefined || soc < summary.minSOC) summary.minSOC = soc;
                    if (summary.maxSOC === undefined || soc > summary.maxSOC) summary.maxSOC = soc;
                }
            };

            const createSummary = (type: SummaryType): ActionSummaryAccumulator => {
                 const hasSOC = !!(action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0);
                 const soc = (action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0) ? action.systemStatus.batterySOC : 0;
                 return {
                    isSummary: true,
                    type: type,
                    reason: action.reason,
                    latestAction: action,
                    startTime: action.timestamp,
                    count: 1,
                    alarms: new Set<string>(),
                    storms: new Set<string>(),
                    hasPrice: hasPrice,
                    priceTotal: hasPrice ? price : 0,
                    priceCount: hasPrice ? 1 : 0,
                    min: hasPrice ? price : Infinity,
                    max: hasPrice ? price : -Infinity,
                    hasSOC: hasSOC,
                    socTotal: hasSOC ? soc : 0,
                    socCount: hasSOC ? 1 : 0,
                    minSOC: hasSOC ? soc : Infinity,
                    maxSOC: hasSOC ? soc : -Infinity
                };
            }

            if (isFault) {
                // fill in missing reason from before we had reason
                if (!action.reason) {
                    if (action.systemStatus && action.systemStatus.alarms) {
                        action.reason = "hasAlarms"
                    }
                    if (action.systemStatus && action.systemStatus.storms) {
                        action.reason = "emergencyMode"
                    }
                }
                if (currentSummary && currentSummary.type === 'fault' && currentSummary.reason === action.reason) {
                    updateSummary(currentSummary);
                    currentSummary.latestAction = action;
                } else {
                    if (currentSummary) {
                        accumulator.push(currentSummary);
                    }
                    currentSummary = createSummary('fault');
                }

                if (action.systemStatus && action.systemStatus.alarms) {
                    action.systemStatus.alarms.forEach(alarm => {
                       if (currentSummary) {
                        currentSummary.alarms.add(alarm.name);
                       }
                    });
                }
                if (action.systemStatus && action.systemStatus.storms) {
                    action.systemStatus.storms.forEach(storm => {
                       if (currentSummary) {
                        currentSummary.storms.add(storm.description);
                        const start = new Date(storm.tsStart);
                        const end = new Date(storm.tsEnd);
                        if (!currentSummary.stormStart || start < currentSummary.stormStart) {
                            currentSummary.stormStart = start;
                        }
                        if (!currentSummary.stormEnd || end > currentSummary.stormEnd) {
                            currentSummary.stormEnd = end;
                        }
                       }
                    });
                }
            } else if (isNoChange) {
                if (currentSummary && currentSummary.type === 'no_change') {
                    updateSummary(currentSummary);
                    currentSummary.latestAction = action;
                } else {
                    if (currentSummary) {
                        accumulator.push(currentSummary);
                    }
                    currentSummary = createSummary('no_change');
                }
            } else {
                if (currentSummary) {
                    accumulator.push(currentSummary);
                    currentSummary = null;
                }
                accumulator.push(action);
            }
        }

        if (currentSummary) {
            accumulator.push(currentSummary);
        }

        return accumulator.map(item => {
            if ('isSummary' in item) {
                const summary = item as ActionSummaryAccumulator;
                const { priceTotal, priceCount, socTotal, socCount, ...rest } = summary;
                return {
                    ...rest,
                    avgPrice: priceCount > 0 ? priceTotal / priceCount : 0,
                    avgSOC: socCount > 0 ? socTotal / socCount : 0
                } as ActionSummary;
            }
            return item;
        });
    }, [visibleActions]);

    return (
        <div className="content-container action-list-container">
            <header className="header">
                <div className="date-controls">
                    <button onClick={() => handleDateChange(-1)} disabled={loading}>&lt; Prev</button>
                    <h2>{formattedDate}</h2>
                    <button onClick={() => handleDateChange(1)} disabled={loading || isToday}>Next &gt;</button>
                </div>
            </header>

            {loading && <p>Loading day...</p>}
            {error && <p className="error">{error}</p>}

            {!loading && !error && (
                <>
                    {settings && (!settings.utilityProvider || settings.utilityProvider === "") && (
                        <div className="banner warning-banner">
                            <p>
                                <strong>Setup Required:</strong> Utility Provider is not configured. <Link href="/settings">Configure it in Settings</Link> to enable automation.
                            </p>
                        </div>
                    )}
                    {settings && (!settings.ess || !settings.hasCredentials?.[settings.ess]) && (
                        <div className="banner warning-banner">
                            <p>
                                <strong>Setup Required:</strong> Energy Storage System is not connected. <Link href="/settings">Configure it in Settings</Link> to enable automation.
                            </p>
                        </div>
                    )}
                    {settings && settings.essAuthStatus && settings.essAuthStatus.consecutiveFailures >= 3 && (
                        <div className="banner warning-banner">
                            <p>
                                <span><strong>Warning:</strong> Energy Storage System authentication failed {settings.essAuthStatus.consecutiveFailures} time(s).{' '}
                                <Link href="/settings">Update your credentials in Settings</Link> to ensure automation continues.</span>
                            </p>
                        </div>
                    )}
                    {siteID !== 'ALL' && isToday && latestAction && (
                        <CurrentStatus action={latestAction} />
                    )}

                    <SavingsHero savings={savings} />

                    {siteID !== 'ALL' && (
                        <>
                            {visibleActions.length === 0 && <p className="no-actions">No actions recorded for this day.</p>}
                            <ActionTimeline groupedActions={groupedActions} />
                        </>
                    )}
                </>
            )}
        </div>
    );
};

export default Dashboard;
