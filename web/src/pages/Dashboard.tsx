import React, { useEffect, useState, useMemo, useCallback, useRef } from 'react';
import { useLocation, useSearch, Link } from 'wouter';
import { type Action, type SavingsStats, type Settings, fetchActionsAndSavings, BatteryMode, ActionReason } from '../api';
import CurrentStatus from '../components/CurrentStatus';
import SavingsHero from '../components/SavingsHero';
import ActionTimeline from '../components/ActionTimeline';
import DateSelector from '../components/DateSelector';
import { useFeatureFlag } from '../utils/features';
import {
    gridChargeCost,
    getActionTimestamp,
    getCurrencySymbol,
    getActionLoadSubType,
    getDateInTimeZone,
    getSiteTodayDate,
    isSameCalendarDate,
    type ActionSummary,
    type ActionSummaryAccumulator,
    type LoadSubType,
    type SummaryType
} from '../utils/dashboardUtils';

interface WhatsNewItem {
    icon: string;
    title: string;
    description: string;
    linkText?: string;
    linkHref?: string;
}

export const whatsNewVersion = 11;
export const whatsNewText = "Our biggest update yet is live with smarter full-day planning, direct grid exports, and a redesigned experience:";
export const whatsNewLinkText = "";
const whatsNewItems: WhatsNewItem[] = [
    {
        icon: "⚡",
        title: "SmartPlan Algorithm",
        description: "Full-horizon planning engine that optimizes charging, holding, and discharging across every price and weather interval.",
    },
    {
        icon: "☀️",
        title: "Direct Solar & Battery Export",
        description: "Export excess solar without charging to 100% first, and discharge battery power during high-price peaks.",
        linkText: "Configure in Settings",
        linkHref: "/settings?tou=1",
    },
    {
        icon: "🚗",
        title: "EV Standby Charging",
        description: "Automatically detects overnight EV charging and holds your battery in standby so it doesn't drain into your car.",
        linkText: "Enable in Settings",
        linkHref: "/settings?ev=1",
    },
    {
        icon: "📊",
        title: "Updated Dashboard & Forecast",
        description: "Compare 7-day and 30-day savings, view 24-hour activity rollups, and explore the new planned battery schedule.",
        linkText: "View Forecast",
        linkHref: "/forecast",
    },
];
export const STALE_DASHBOARD_TIMEOUT_MS = 10 * 60 * 1000;

const Dashboard: React.FC<{ siteID?: string, settings?: Settings | null }> = ({ siteID, settings = null }) => {
    const currencySymbol = getCurrencySymbol(settings?.utilityProvider);
    const [location, navigate] = useLocation();
    const search = useSearch();
    const searchParams = useMemo(() => new URLSearchParams(search), [search]);

    const dateQuery = searchParams.get('date');
    const [actions, setActions] = useState<Action[]>([]);
    const [savings, setSavings] = useState<SavingsStats | null>(null);
    const [savingsPeriods, setSavingsPeriods] = useState<SavingsStats[] | undefined>(undefined);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const onlyPlanMode = useFeatureFlag('onlyPlanMode');

    const [showWhatsNew, setShowWhatsNew] = useState(false);
    const [showTOUNotice, setShowTOUNotice] = useState(false);
    const [showGridWarning, setShowGridWarning] = useState(false);
    const [showLocationWarning, setShowLocationWarning] = useState(false);

    const [refreshTrigger, setRefreshTrigger] = useState(0);
    const lastHiddenTimeRef = useRef<number | null>(
        typeof document !== 'undefined' && document.visibilityState === 'hidden' ? Date.now() : null
    );
    const lastFetchTimeRef = useRef<number>(Date.now());
    const isRefreshRef = useRef(false);

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

                    if (timeAway >= STALE_DASHBOARD_TIMEOUT_MS || timeSinceLastFetch >= STALE_DASHBOARD_TIMEOUT_MS) {
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
        const storedVersion = localStorage.getItem('whats_new_banner_version');
        if (!storedVersion || parseInt(storedVersion, 10) < whatsNewVersion) {
            setShowWhatsNew(true);
        }
    }, []);

    const dismissWhatsNew = () => {
        localStorage.setItem('whats_new_banner_version', whatsNewVersion.toString());
        setShowWhatsNew(false);
    };

    useEffect(() => {
        if (settings) {
            const hasUtility = !!settings.utilityProvider && settings.utilityProvider !== "";

            const gridKey = siteID ? `grid_restrictions_warning_dismissed_${siteID}` : 'grid_restrictions_warning_dismissed';
            const isGridDismissed = localStorage.getItem(gridKey) === 'true';
            const allUnchecked = !settings.gridChargeBatteries && !settings.gridExportSolar && !settings.gridExportBatteries;
            setShowGridWarning(hasUtility && allUnchecked && !isGridDismissed);

            const locationKey = siteID ? `location_warning_dismissed_${siteID}` : 'location_warning_dismissed';
            const isLocationDismissed = localStorage.getItem(locationKey) === 'true';
            const missingLocation = !settings.countryCode || !settings.postalCode;
            setShowLocationWarning(hasUtility && missingLocation && !isLocationDismissed);

            const touKey = siteID ? `tou_notice_dismissed_${siteID}` : 'tou_notice_dismissed';
            const isTOUDismissed = localStorage.getItem(touKey) === 'true';
            const supportsTOU = ['tesla', 'franklin'].includes(settings.ess || '');
            const canExport = Boolean(settings.gridExportSolar || settings.gridExportBatteries);
            setShowTOUNotice(supportsTOU && canExport && !settings.manageTOUSchedules && !isTOUDismissed);
        } else {
            setShowGridWarning(false);
            setShowLocationWarning(false);
            setShowTOUNotice(false);
        }
    }, [settings, siteID]);

    const dismissTOUNotice = () => {
        const key = siteID ? `tou_notice_dismissed_${siteID}` : 'tou_notice_dismissed';
        localStorage.setItem(key, 'true');
        setShowTOUNotice(false);
    };

    const dismissGridWarning = () => {
        const key = siteID ? `grid_restrictions_warning_dismissed_${siteID}` : 'grid_restrictions_warning_dismissed';
        localStorage.setItem(key, 'true');
        setShowGridWarning(false);
    };

    const dismissLocationWarning = () => {
        const key = siteID ? `location_warning_dismissed_${siteID}` : 'location_warning_dismissed';
        localStorage.setItem(key, 'true');
        setShowLocationWarning(false);
    };

    const siteTimeZone =
        (actions.length > 0 ? actions[actions.length - 1]?.systemStatus?.timeLocation : undefined) ||
        settings?.location?.timeZone;

    const todayDate = useMemo(() => {
        void refreshTrigger;
        return getSiteTodayDate(siteTimeZone);
    }, [siteTimeZone, refreshTrigger]);

    const currentDate = useMemo(() => {
        if (dateQuery) {
            const parts = dateQuery.split('-');
            if (parts.length === 3) {
                const year = parseInt(parts[0], 10);
                const month = parseInt(parts[1], 10) - 1;
                const day = parseInt(parts[2], 10);
                return new Date(year, month, day, 12, 0, 0, 0);
            }
        }
        return todayDate;
    }, [dateQuery, todayDate]);

    const isFirstMount = useRef(true);

    useEffect(() => {
        let isCancelled = false;
        const delay = isFirstMount.current || isRefreshRef.current ? 0 : 200;
        isFirstMount.current = false;
        isRefreshRef.current = false;

        const timer = setTimeout(async () => {
            setLoading(true);
            setError(null);
            try {
                // Calculate start and end of the day in the site's timezone (or local time if unknown)
                const y = currentDate.getFullYear();
                const m = currentDate.getMonth();
                const d = currentDate.getDate();
                const start = getDateInTimeZone(y, m, d, 0, 0, 0, 0, siteTimeZone);
                const end = getDateInTimeZone(y, m, d, 23, 59, 59, 999, siteTimeZone);

                const weekStart = getDateInTimeZone(y, m, d - 6, 0, 0, 0, 0, siteTimeZone);
                const monthStart = getDateInTimeZone(y, m, d - 29, 0, 0, 0, 0, siteTimeZone);

                const actionsAndSavingsData = onlyPlanMode
                    ? await fetchActionsAndSavings(start, end, siteID, {
                          newCalculation: true,
                          savingsPeriods: [
                              { start, end },
                              { start: weekStart, end },
                              { start: monthStart, end },
                          ],
                      })
                    : await fetchActionsAndSavings(start, end, siteID);

                if (!isCancelled) {
                    setActions(actionsAndSavingsData.actions || []);
                    setSavings(actionsAndSavingsData.savings);
                    setSavingsPeriods(actionsAndSavingsData.savingsPeriods);
                    lastFetchTimeRef.current = Date.now();
                }
            } catch (err) {
                if (!isCancelled) {
                    console.error(err);
                    setError(err instanceof Error ? err.message : 'Failed to load data');
                }
            } finally {
                if (!isCancelled) {
                    setLoading(false);
                    lastFetchTimeRef.current = Date.now();
                }
            }
        }, delay);

        return () => {
            isCancelled = true;
            clearTimeout(timer);
        };
    }, [currentDate, siteID, refreshTrigger, onlyPlanMode, siteTimeZone]);

    const handleDateChange = useCallback((days: number) => {
        const newDate = new Date(currentDate);
        newDate.setDate(newDate.getDate() + days);
        const today = getSiteTodayDate(siteTimeZone);
        const p = new URLSearchParams(search);
        if (isSameCalendarDate(newDate, today)) {
            p.delete('date');
            const newSearch = p.toString();
            navigate(location + (newSearch ? "?" + newSearch : ""));
        } else {
            const year = newDate.getFullYear();
            const month = String(newDate.getMonth() + 1).padStart(2, '0');
            const day = String(newDate.getDate()).padStart(2, '0');
            p.set('date', `${year}-${month}-${day}`);
            navigate(location + "?" + p.toString());
        }
    }, [currentDate, siteTimeZone, search, location, navigate]);

    const isToday = isSameCalendarDate(currentDate, todayDate);


    const latestAction = actions.length > 0 ? actions[actions.length - 1] : null;
    // Filter out paused actions from the displayed timeline — they are captured for
    // status tracking only and should not appear as regular action items.
    const visibleActions = actions.filter(a => !a.paused);

    const groupedActions = useMemo(() => {
        const accumulator: (Action | ActionSummaryAccumulator)[] = [];
        let currentSummary: ActionSummaryAccumulator | null = null;
        let prevSOC: number | undefined;

        for (const action of visibleActions) {
            const isFault = !!action.fault || action.reason === ActionReason.VPPActive || !!action.systemStatus?.vppActive;
            const hasPrice = !!action.currentPrice && action.currentPrice.tsStart !== "0001-01-01T00:00:00Z";
            const price = action.currentPrice ? gridChargeCost(action.currentPrice) : 0;
            const currentSOC =
                action.systemStatus &&
                action.systemStatus.batterySOC !== undefined &&
                action.systemStatus.batterySOC !== 0
                    ? action.systemStatus.batterySOC
                    : undefined;

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
                    const soc = action.systemStatus.batterySOC;
                    if (!summary.hasSOC) {
                        summary.startSOC = soc;
                    }
                    summary.endSOC = soc;
                    summary.hasSOC = true;
                    summary.socTotal += soc;
                    summary.socCount++;
                    if (summary.minSOC === undefined || soc < summary.minSOC) summary.minSOC = soc;
                    if (summary.maxSOC === undefined || soc > summary.maxSOC) summary.maxSOC = soc;
                }
                if ((action.systemStatus?.solarKW ?? 0) > 0) {
                    summary.hasSolar = true;
                }
            };

            const createSummary = (
                type: SummaryType,
                loadSubType?: LoadSubType
            ): ActionSummaryAccumulator => {
                 const hasSOC = !!(action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0);
                 const soc = (action.systemStatus && action.systemStatus.batterySOC !== undefined && action.systemStatus.batterySOC !== 0) ? action.systemStatus.batterySOC : 0;
                 const hasSolar = (action.systemStatus?.solarKW ?? 0) > 0;
                 return {
                    isSummary: true,
                    type: type,
                    reason: action.reason,
                    loadSubType,
                    latestAction: action,
                    startTime: getActionTimestamp(action),
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
                    maxSOC: hasSOC ? soc : -Infinity,
                    startSOC: hasSOC ? soc : 0,
                    endSOC: hasSOC ? soc : 0,
                    hasSolar: hasSolar
                };
            };

            if (isFault) {
                if (!action.reason) {
                    if (action.systemStatus && action.systemStatus.alarms) {
                        action.reason = "hasAlarms" as any;
                    }
                    if (action.systemStatus && action.systemStatus.storms) {
                        action.reason = "emergencyMode" as any;
                    }
                    if (action.systemStatus && action.systemStatus.gridUnavailable) {
                        action.reason = "gridUnavailable" as any;
                    }
                    if (action.systemStatus && action.systemStatus.vppActive) {
                        action.reason = ActionReason.VPPActive;
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
            } else {
                // Group non-fault actions
                const effectiveBatteryMode = action.batteryMode === BatteryMode.NoChange && currentSummary
                    ? currentSummary.latestAction.batteryMode
                    : action.batteryMode;
                const actionLoadSubType = getActionLoadSubType(
                    { ...action, batteryMode: effectiveBatteryMode },
                    prevSOC
                );

                if (
                    currentSummary &&
                    currentSummary.type === 'grouped' &&
                    (currentSummary.loadSubType ?? 'load') === actionLoadSubType &&
                    (
                        (currentSummary.reason === action.reason && currentSummary.latestAction.batteryMode === effectiveBatteryMode) ||
                        (action.batteryMode === BatteryMode.NoChange)
                    )
                ) {
                    updateSummary(currentSummary);
                    currentSummary.latestAction = {
                        ...action,
                        batteryMode: effectiveBatteryMode
                    };
                } else {
                    if (currentSummary) {
                        accumulator.push(currentSummary);
                    }
                    currentSummary = createSummary('grouped', actionLoadSubType);
                }
            }
            if (currentSOC !== undefined) {
                prevSOC = currentSOC;
            }
        }

        if (currentSummary) {
            accumulator.push(currentSummary);
        }

        return accumulator.map(item => {
            if ('isSummary' in item) {
                const summary = item as ActionSummaryAccumulator;
                if (summary.type === 'grouped' && summary.count === 1) {
                    return summary.latestAction;
                }

                const { priceTotal, priceCount, socTotal, socCount, ...rest } = summary;
                return {
                    ...rest,
                    endTime: getActionTimestamp(summary.latestAction),
                    avgPrice: priceCount > 0 ? priceTotal / priceCount : 0,
                    avgSOC: socCount > 0 ? socTotal / socCount : 0
                } as ActionSummary;
            }
            return item;
        }).reverse();
    }, [visibleActions]);

    return (
        <div className="content-container action-list-container">
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
                    Loading day...
                </div>
            )}
            {error && <p className="error">{error}</p>}

            {!loading && !error && (
                <>
                    {showWhatsNew && (
                        <div className="banner info-banner whats-new-banner" data-testid="whats-new-banner">
                            <div className="whats-new-header">
                                <div className="whats-new-title-row">
                                    <span className="whats-new-badge">Major Update</span>
                                    <p className="whats-new-intro">
                                        <strong>What's New:</strong> {whatsNewText}{whatsNewLinkText && <>{' '}<Link href="/settings">{whatsNewLinkText}</Link></>}
                                    </p>
                                </div>
                                <button onClick={dismissWhatsNew} className="banner-dismiss-btn" aria-label="Dismiss What's New banner">
                                    <span aria-hidden="true">&times;</span>
                                </button>
                            </div>
                            <div className="whats-new-grid">
                                {whatsNewItems.map((item) => (
                                    <div key={item.title} className="whats-new-item">
                                        <div className="whats-new-item-header">
                                            <span className="whats-new-item-icon" aria-hidden="true">{item.icon}</span>
                                            <span className="whats-new-item-title">{item.title}</span>
                                        </div>
                                        <p className="whats-new-item-desc">
                                            {item.description}
                                            {item.linkText && item.linkHref && (
                                                <>
                                                    {' '}
                                                    <Link href={item.linkHref}>{item.linkText} →</Link>
                                                </>
                                            )}
                                        </p>
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}
                    {showTOUNotice && (
                        <div className="banner info-banner" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }} data-testid="tou-notice-banner">
                            <p>
                                <span>
                                    <strong>New Feature:</strong> Turn on the new Time-Of-Use (TOU) export management mode so RateRudder can export solar and battery power directly to the grid when prices are high without charging to 100% first.{' '}
                                    <Link href="/settings?tou=1">Enable it in Settings</Link>
                                </span>
                            </p>
                            <button onClick={dismissTOUNotice} className="banner-dismiss-btn" aria-label="Dismiss TOU notice">
                                <span aria-hidden="true">&times;</span>
                            </button>
                        </div>
                    )}
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
                                <Link href="/settings?ess=1">Update your credentials in Settings</Link> to ensure automation continues.</span>
                            </p>
                        </div>
                    )}
                    {settings && settings.essAuthStatus && (settings.essAuthStatus.consecutiveFailures ?? 0) < 3 && (settings.essAuthStatus.consecutiveSetFailures ?? 0) >= 3 && (
                        <div className="banner warning-banner">
                            <p>
                                {settings.ess === 'tesla' ? (
                                    <span><strong>Warning:</strong> RateRudder can read your Tesla battery status, but failed to update battery modes {settings.essAuthStatus.consecutiveSetFailures} time(s) due to missing command permissions.{' '}
                                    <Link href="/settings?ess=1">Re-link your Tesla account in Settings</Link> and make sure all permissions (including Energy Commands) are checked.</span>
                                ) : (
                                    <span><strong>Warning:</strong> Energy Storage System rejected battery mode updates {settings.essAuthStatus.consecutiveSetFailures} time(s).{' '}
                                    Please check your battery manufacturer app to make sure your account can change battery settings, or <Link href="/settings?ess=1">re-authenticate in Settings</Link>.</span>
                                )}
                            </p>
                        </div>
                    )}
                    {showGridWarning && (
                        <div className="banner warning-banner" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }} data-testid="grid-restrictions-warning-banner">
                            <p>
                                <span><strong>Warning:</strong> All grid features are disabled. The battery can only charge from solar. <Link href="/settings">Change this in Settings</Link></span>
                            </p>
                            <button onClick={dismissGridWarning} className="banner-dismiss-btn" aria-label="Dismiss grid restrictions warning">
                                <span aria-hidden="true">&times;</span>
                            </button>
                        </div>
                    )}
                    {showLocationWarning && (
                        <div className="banner warning-banner" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }} data-testid="location-warning-banner">
                            <p>
                                <span><strong>Warning:</strong> Location is not specified. <Link href="/settings">Configure it in Settings</Link> to improve solar forecasting accuracy.</span>
                            </p>
                            <button onClick={dismissLocationWarning} className="banner-dismiss-btn" aria-label="Dismiss location warning">
                                <span aria-hidden="true">&times;</span>
                            </button>
                        </div>
                    )}
                    {settings && settings.ess && settings.hasCredentials?.[settings.ess] && (settings.pause || settings.dryRun) && (
                        <div className="banner warning-banner" data-testid="automation-paused-dryrun-warning-banner">
                            <p>
                                <span><strong>Warning:</strong> {
                                    settings.pause && settings.dryRun
                                        ? "Automation is paused and Dry Run is enabled. The system will not write states to your hardware."
                                        : settings.pause
                                            ? "Automation is paused. The system will not perform any automated actions."
                                            : "Dry Run is enabled. The system will simulate actions but will not write them to your hardware."
                                } <Link href="/settings">Change this in Settings</Link></span>
                            </p>
                        </div>
                    )}
                    {siteID !== 'ALL' && isToday && latestAction && (
                        <CurrentStatus action={latestAction} currencySymbol={currencySymbol} planMode={onlyPlanMode} timeZone={siteTimeZone} />
                    )}

                    <SavingsHero
                        savings={savings}
                        savingsPeriods={savingsPeriods}
                        planMode={onlyPlanMode}
                        currentDate={currentDate}
                        isToday={isToday}
                        currencySymbol={currencySymbol}
                    />

                    {siteID !== 'ALL' && (
                        <>
                            {visibleActions.length === 0 && <p className="no-actions">No actions recorded for this day.</p>}
                            <ActionTimeline groupedActions={groupedActions} currencySymbol={currencySymbol} collapsible timeZone={siteTimeZone} />
                        </>
                    )}
                </>
            )}
        </div>
    );
};

export default Dashboard;
