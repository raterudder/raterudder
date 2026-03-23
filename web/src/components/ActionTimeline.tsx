import { type Action, BatteryMode, SolarMode, ActionReason } from '../api';
import {
    getBatteryModeLabel,
    getBatteryModeClass,
    getSolarModeLabel,
    getSolarModeClass,
    formatPrice,
    formatTime,
    getReasonText,
    gridChargeCost,
    type ActionSummary
} from '../utils/dashboardUtils';
import './ActionTimeline.css';

interface ActionTimelineProps {
    groupedActions: (Action | ActionSummary)[];
}

const ActionTimeline: React.FC<ActionTimelineProps> = ({ groupedActions }) => {
    return (
        <ul className="timeline">
            {groupedActions.map((item, index) => {
                const isSummary = 'isSummary' in item;
                const action = isSummary ? (item as ActionSummary).latestAction : (item as Action);

                // For summaries, we might have multiple actions in one card
                const summary = isSummary ? (item as ActionSummary) : null;
                const isFault = !!action.fault || (summary?.type === 'fault');
                const hasStorms = action.systemStatus?.storms && action.systemStatus.storms.length > 0;
                const isEmergency = hasStorms || action.reason === ActionReason.EmergencyMode;

                const reasonText = getReasonText(action);
                const batteryModeClass = getBatteryModeClass(action.batteryMode);
                const isNegPrice = action.currentPrice && (action.currentPrice.dollarsPerKWH + (action.currentPrice.gridUseDollarsPerKWH || 0)) < 0;

                // Determine Title
                let title = getBatteryModeLabel(action.batteryMode);
                if (isFault) title = 'System Fault';
                if (isEmergency) {
                    title = hasStorms ? 'Storm Hedge Mode' : 'Emergency Mode';
                }
                if (action.reason === ActionReason.BatteryAtReserve) {
                    title = 'Battery At Reserve';
                }

                const showDeficitTag = action.deficitAt && action.deficitAt !== '0001-01-01T00:00:00Z';
                const showCapacityTag = action.capacityAt && action.capacityAt !== '0001-01-01T00:00:00Z';

                return (
                    <li key={index} className={`timeline-item mode-${isFault ? 'fault' : batteryModeClass} ${summary ? 'is-grouped' : ''}`}>
                        <div className="timeline-marker"></div>

                        <div className="timeline-time">
                            {formatTime(isSummary ? summary!.startTime : action.timestamp)}
                        </div>

                        <div className="timeline-content">
                            <h3>
                                {title}
                                {summary && summary.count > 1 && (
                                    <span className="count">({summary.count}x)</span>
                                )}
                            </h3>

                            <div className="reason">
                                {isEmergency ? (
                                    <>
                                        {action.reason === ActionReason.EmergencyMode && !hasStorms && <p>System manually put into emergency mode. Skipping automation.</p>}
                                        {hasStorms && <p>Charging the battery to prepare for the storm.</p>}
                                        {hasStorms && summary && Array.from(summary.storms).length > 0 && (
                                            <p className="storm-details">Storms: {Array.from(summary.storms).join(', ')}</p>
                                        )}
                                        {hasStorms && (
                                            <p className="storm-time">
                                                Storm Duration: {formatTime(isSummary && summary ? summary.stormStart?.toISOString() || '' : action.systemStatus?.storms?.[0]?.tsStart || '')} - {formatTime(isSummary && summary ? summary.stormEnd?.toISOString() || '' : action.systemStatus?.storms?.[0]?.tsEnd || '')}
                                            </p>
                                        )}
                                    </>
                                ) : isFault ? (
                                    <div className="fault-details">
                                        <p className="fault-alarms">
                                            Alarms: {summary ? Array.from(summary.alarms).join(', ') : action.systemStatus?.alarms?.map(a => a.name).join(', ')}
                                        </p>
                                    </div>
                                ) : (
                                    <p>{reasonText}</p>
                                )}
                            </div>

                            <div className="tags">
                                {(action.batteryMode !== BatteryMode.NoChange || (action.targetBatteryMode !== undefined && action.targetBatteryMode !== BatteryMode.NoChange)) && (
                                    <span className={`tag mode-${getBatteryModeClass(action.targetBatteryMode || action.batteryMode)}`}>
                                        {getBatteryModeLabel(action.targetBatteryMode || action.batteryMode)}
                                    </span>
                                )}
                                {(action.solarMode !== SolarMode.NoChange || (action.targetSolarMode !== undefined && action.targetSolarMode !== SolarMode.NoChange)) && (
                                    <span className={`tag solar-${getSolarModeClass(action.targetSolarMode || action.solarMode)}`}>
                                        {getSolarModeLabel(action.targetSolarMode || action.solarMode)}
                                    </span>
                                )}
                                {showDeficitTag && (
                                    <span className="tag tag-info">Deficit: {formatTime(action.deficitAt!)}</span>
                                )}
                                {showCapacityTag && (
                                    <span className="tag tag-info">Capacity: {formatTime(action.capacityAt!)}</span>
                                )}
                                {isNegPrice && (
                                    <span className="tag tag-warning">Negative Price</span>
                                )}
                                {action.dryRun && (
                                    <span className="tag dry-run">Dry Run</span>
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
                                                        {formatPrice(summary!.avgPrice)}
                                                        {summary!.min !== summary!.max && (
                                                            <small className="range"> (Range: $ {summary!.min.toFixed(3)} - $ {summary!.max.toFixed(3)})</small>
                                                        )}
                                                    </span>
                                                </div>
                                            )}
                                            {summary!.hasSOC && (
                                                <div className="timeline-metric">
                                                    <span className="label">Battery:</span>
                                                    <span className="value">
                                                        {summary!.avgSOC.toFixed(1)}%
                                                        {summary!.minSOC !== summary!.maxSOC && (
                                                            <small className="range"> (Range: {summary!.minSOC.toFixed(0)}% - {summary!.maxSOC.toFixed(0)}%)</small>
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
                                                    <span className="value">{formatPrice(gridChargeCost(action.currentPrice))}</span>
                                                </div>
                                            )}
                                            {action.systemStatus?.batterySOC !== undefined && (
                                                <div className="timeline-metric">
                                                    <span className="label">Battery SOC:</span>
                                                    <span className="value">{action.systemStatus.batterySOC.toFixed(1)}%</span>
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
};

export default ActionTimeline;
