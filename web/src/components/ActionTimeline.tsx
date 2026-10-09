import React, { useState, useMemo } from 'react';
import { Collapsible } from '@base-ui/react/collapsible';
import { type Action, BatteryMode, SolarMode, ActionReason } from '../api';
import {
    getBatteryPillLabel,
    getBatteryModeClass,
    getSolarModeLabel,
    getSolarModeClass,
    formatPrice,
    formatTime,
    formatSOCRange,
    getActionTimestamp,
    getActionTitle,
    hasSolarActivity,
    buildDaySummary,
    isZeroTime,
    getReasonText,
    gridChargeCost,
    type ActionSummary
} from '../utils/dashboardUtils';
import './ActionTimeline.css';

interface ActionTimelineProps {
    groupedActions: (Action | ActionSummary)[];
    currencySymbol?: string;
    collapsible?: boolean;
    defaultOpen?: boolean;
}

const ActionTimeline: React.FC<ActionTimelineProps> = ({
    groupedActions,
    currencySymbol = '$',
    collapsible = false,
    defaultOpen = false
}) => {
    const [open, setOpen] = useState(defaultOpen);

    const daySummary = useMemo(
        () => buildDaySummary(groupedActions, currencySymbol),
        [groupedActions, currencySymbol]
    );

    if (groupedActions.length === 0) {
        return null;
    }

    const timelineList = (
        <ul className="timeline">
            {groupedActions.map((item, index) => {
                const isSummary = 'isSummary' in item;
                const action = isSummary ? (item as ActionSummary).latestAction : (item as Action);

                const summary = isSummary ? (item as ActionSummary) : null;
                const isFault = !!action.fault || (summary?.type === 'fault');
                const hasStorms = action.systemStatus?.storms && action.systemStatus.storms.length > 0;
                const isEmergency = hasStorms || action.reason === ActionReason.EmergencyMode;
                const isVPP = !!action.systemStatus?.vppActive || action.reason === ActionReason.VPPActive;

                const reasonText = getReasonText(action, currencySymbol);
                const effectiveBatteryMode =
                    action.targetBatteryMode !== undefined && action.targetBatteryMode !== BatteryMode.NoChange
                        ? action.targetBatteryMode
                        : action.batteryMode;
                const effectiveSolarMode =
                    action.targetSolarMode !== undefined && action.targetSolarMode !== SolarMode.NoChange
                        ? action.targetSolarMode
                        : action.solarMode;

                let batteryModeClass = getBatteryModeClass(effectiveBatteryMode);
                if (isVPP) {
                    batteryModeClass = 'vpp';
                }
                const isNegPrice =
                    action.currentPrice &&
                    (action.currentPrice.dollarsPerKWH + (action.currentPrice.gridUseDollarsPerKWH || 0)) < 0;

                const title = getActionTitle(action, isFault);
                const showDeficitTag = !isZeroTime(action.deficitAt);
                const showCapacityTag = !isZeroTime(action.capacityAt) && action.reason !== ActionReason.DirectExport;
                const showSolarTag = hasSolarActivity(item) && effectiveSolarMode !== SolarMode.NoChange;
                const refTs =
                    action.systemTimestamp && !isZeroTime(action.systemTimestamp)
                        ? action.systemTimestamp
                        : action.systemStatus?.timestamp;

                return (
                    <li
                        key={index}
                        className={`timeline-item mode-${isFault && !isVPP ? 'fault' : batteryModeClass} ${
                            summary ? 'is-grouped' : ''
                        }`}
                    >
                        <div className="timeline-content">
                            <div className="timeline-card-header">
                                <div className="timeline-header-left">
                                    <span className="timeline-status-dot" aria-hidden="true" />
                                    <div className="timeline-time">
                                        {isSummary &&
                                        summary!.endTime &&
                                        formatTime(summary!.endTime, refTs) !== formatTime(summary!.startTime, refTs) ? (
                                            <div className="time-range">
                                                <span className="time-start">{formatTime(summary!.startTime, refTs)}</span>
                                                <span className="time-range-sep" aria-hidden="true">–</span>
                                                <span className="time-end">{formatTime(summary!.endTime, refTs)}</span>
                                            </div>
                                        ) : (
                                            formatTime(isSummary ? summary!.startTime : getActionTimestamp(action), refTs)
                                        )}
                                    </div>
                                </div>
                            </div>

                            <h3>{title}</h3>

                            <div className="tags">
                                {effectiveBatteryMode !== BatteryMode.NoChange && (
                                    <span className={`tag mode-${getBatteryModeClass(effectiveBatteryMode)}`}>
                                        <span className="tag-icon" aria-hidden="true">🔋</span>
                                        {getBatteryPillLabel(effectiveBatteryMode)}
                                    </span>
                                )}
                                {showSolarTag && (
                                    <span className={`tag solar-${getSolarModeClass(effectiveSolarMode)}`}>
                                        <span className="tag-icon" aria-hidden="true">☀️</span>
                                        {getSolarModeLabel(effectiveSolarMode)}
                                    </span>
                                )}
                                {showDeficitTag && (
                                    <span className="tag tag-info">Empty: {formatTime(action.deficitAt!, refTs)}</span>
                                )}
                                {showCapacityTag && (
                                    <span className="tag tag-info">Full: {formatTime(action.capacityAt!, refTs)}</span>
                                )}
                                {isNegPrice && <span className="tag tag-warning">Negative Price</span>}
                                {action.dryRun && <span className="tag dry-run">Dry Run</span>}
                            </div>

                            <div className="reason">
                                {isEmergency ? (
                                    <>
                                        {action.reason === ActionReason.EmergencyMode && !hasStorms && (
                                            <p>System manually put into emergency mode. Skipping automation.</p>
                                        )}
                                        {hasStorms && <p>Charging the battery to prepare for the storm.</p>}
                                        {hasStorms && summary && Array.from(summary.storms).length > 0 && (
                                            <p className="storm-details">
                                                Storms: {Array.from(summary.storms).join(', ')}
                                            </p>
                                        )}
                                        {hasStorms && (
                                            <p className="storm-time">
                                                Storm Duration:{' '}
                                                {formatTime(
                                                    isSummary && summary
                                                        ? summary.stormStart?.toISOString() || ''
                                                        : action.systemStatus?.storms?.[0]?.tsStart || '',
                                                    refTs
                                                )}{' '}
                                                -{' '}
                                                {formatTime(
                                                    isSummary && summary
                                                        ? summary.stormEnd?.toISOString() || ''
                                                        : action.systemStatus?.storms?.[0]?.tsEnd || '',
                                                    refTs
                                                )}
                                            </p>
                                        )}
                                    </>
                                ) : isFault ? (
                                    <div className="fault-details">
                                        {action.reason === ActionReason.GridUnavailable ||
                                        action.systemStatus?.gridUnavailable ||
                                        isVPP ? (
                                            <p>{reasonText}</p>
                                        ) : (
                                            <p className="fault-alarms">
                                                Alarms:{' '}
                                                {summary
                                                    ? Array.from(summary.alarms).join(', ')
                                                    : action.systemStatus?.alarms?.map(a => a.name).join(', ')}
                                            </p>
                                        )}
                                    </div>
                                ) : (
                                    <p>{reasonText}</p>
                                )}
                            </div>

                            <div className="timeline-footer">
                                <div className="timeline-metrics">
                                    {isSummary ? (
                                        <>
                                            {summary!.hasPrice && (
                                                <div className="timeline-metric">
                                                    <span className="label">Avg Price:</span>
                                                    <span className="value">
                                                        {formatPrice(summary!.avgPrice, currencySymbol)}
                                                        {summary!.min !== summary!.max && (
                                                            <small className="range">
                                                                {' '}
                                                                (Range: {currencySymbol} {summary!.min.toFixed(3)} -{' '}
                                                                {currencySymbol} {summary!.max.toFixed(3)})
                                                            </small>
                                                        )}
                                                    </span>
                                                </div>
                                            )}
                                            {summary!.hasSOC && (
                                                <div className="timeline-metric">
                                                    <span className="label">Battery:</span>
                                                    <span className="value">
                                                        {formatSOCRange(
                                                            summary!.startSOC ?? summary!.minSOC ?? summary!.avgSOC,
                                                            summary!.endSOC ?? summary!.maxSOC ?? summary!.avgSOC
                                                        )}
                                                    </span>
                                                </div>
                                            )}
                                        </>
                                    ) : (
                                        <>
                                            {action.currentPrice && (
                                                <div className="timeline-metric">
                                                    <span className="label">Price:</span>
                                                    <span className="value">
                                                        {formatPrice(gridChargeCost(action.currentPrice), currencySymbol)}
                                                    </span>
                                                </div>
                                            )}
                                            {action.systemStatus?.batterySOC !== undefined && (
                                                <div className="timeline-metric">
                                                    <span className="label">Battery SOC:</span>
                                                    <span className="value">
                                                        {action.systemStatus.batterySOC.toFixed(1)}%
                                                    </span>
                                                </div>
                                            )}
                                        </>
                                    )}
                                </div>
                            </div>
                        </div>
                    </li>
                );
            })}
        </ul>
    );

    if (!collapsible) {
        return timelineList;
    }

    return (
        <div className="action-timeline-section">
            <div className="day-summary-card" data-testid="day-summary-card">
                <div className="day-summary-header">
                    <span className="day-summary-label">Summary</span>
                    {daySummary.legend.length > 0 && (
                        <div className="day-summary-legend" aria-label="Activity Legend">
                            {daySummary.legend.map(item => (
                                <span key={item.category} className={`legend-item leg-${item.category}`}>
                                    <span className="legend-dot" aria-hidden="true" />
                                    {item.label}
                                </span>
                            ))}
                        </div>
                    )}
                </div>

                {daySummary.segments.length > 0 && (
                    <div className="day-summary-strip-container">
                        <div className="day-summary-strip" role="img" aria-label="24-hour battery activity bar">
                            {daySummary.segments.map((seg, idx) => (
                                <div
                                    key={idx}
                                    className={`day-summary-segment seg-${seg.category}`}
                                    style={{ left: `${seg.leftPercent}%`, width: `${seg.widthPercent}%` }}
                                />
                            ))}
                        </div>
                        <div className="day-summary-axis" aria-hidden="true">
                            <span>12a</span>
                            <span>6a</span>
                            <span>12p</span>
                            <span>6p</span>
                            <span>12a</span>
                        </div>
                    </div>
                )}

                {daySummary.beats.length > 0 && (
                    <div className="day-summary-beats">
                        {daySummary.beats.map((beat, idx) => (
                            <div key={idx} className={`day-summary-beat beat-${beat.category}`}>
                                <span className="beat-dot" aria-hidden="true" />
                                <div className="beat-body">
                                    <div className="beat-top">
                                        <span className="beat-headline">{beat.headline}</span>
                                        {beat.timeLabel && <span className="beat-time">{beat.timeLabel}</span>}
                                    </div>
                                    {(beat.socText || beat.priceText) && (
                                        <div className="beat-meta">
                                            {beat.socText && <span className="beat-soc">{beat.socText}</span>}
                                            {beat.socText && beat.priceText && (
                                                <span className="beat-sep" aria-hidden="true">
                                                    ·
                                                </span>
                                            )}
                                            {beat.priceText && <span className="beat-price">{beat.priceText}</span>}
                                        </div>
                                    )}
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            <Collapsible.Root open={open} onOpenChange={setOpen}>
                <Collapsible.Trigger className="timeline-toggle-btn">
                    <span>
                        {open ? 'Hide Detailed Action Log' : `Show Detailed Action Log (${groupedActions.length})`}
                    </span>
                    <span className={`arrow ${open ? 'up' : 'down'}`} aria-hidden="true" />
                </Collapsible.Trigger>
                <Collapsible.Panel className="timeline-collapsible-panel">
                    {timelineList}
                </Collapsible.Panel>
            </Collapsible.Root>
        </div>
    );
};

export default ActionTimeline;
