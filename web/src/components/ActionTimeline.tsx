import React from 'react';
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

interface ActionTimelineProps {
    groupedActions: (Action | ActionSummary)[];
}

const ActionTimeline: React.FC<ActionTimelineProps> = ({ groupedActions }) => {
    return (
        <ul className="action-list">
            {groupedActions.map((item, index) => {
                if ('isSummary' in item) {
                    const summary = item as ActionSummary;
                    const isFault = summary.type === 'fault';
                    const isEmergency = isFault && summary.reason === ActionReason.EmergencyMode;
                    const hasStorms = isEmergency && summary.storms && summary.storms.size > 0;

                    let title = isFault ? 'System Fault' : getBatteryModeLabel(summary.latestAction.batteryMode);
                    let description = '';

                    if (isEmergency) {
                        if (hasStorms) {
                            title = 'Storm Hedge Mode';
                            description = 'Franklin is charging the battery to prepare for the storm.';
                        } else {
                            title = 'Emergency Mode';
                            description = 'System manually put into emergency mode. Skipping automation.';
                        }
                    } else if (!isFault && summary.latestAction) {
                        description = getReasonText(summary.latestAction);
                    }

                    const alarms = isFault && !isEmergency ? Array.from(summary.alarms).join(', ') : '';

                    return (
                        <li key={index} className={`action-item summary-item ${isFault ? 'fault-item' : ''} ${isEmergency ? 'emergency-item' : ''}`}>
                            <div className="action-time">
                                {formatTime(summary.startTime)}
                                {summary.count > 1 && summary.endTime && (
                                    <> - {formatTime(summary.endTime)}</>
                                )}
                            </div>
                            <div className="action-details">
                                <h3>{title} {summary.count > 1 && <span>({summary.count}x)</span>}</h3>
                                {isEmergency ? (
                                    <div className="emergency-details">
                                        <p>{description}</p>
                                        {hasStorms && summary.stormStart && summary.stormEnd && (
                                            <p className="storm-time">
                                                Storm Duration: {formatTime(summary.stormStart.toISOString())} - {formatTime(summary.stormEnd.toISOString())}
                                            </p>
                                        )}
                                    </div>
                                ) : (
                                    <>
                                        {isFault && alarms && (
                                            <p className="fault-alarms">Alarms: {alarms}</p>
                                        )}
                                        {!isFault && description && (
                                            <p>{description}</p>
                                        )}
                                        <div className="tags">
                                            {(summary.latestAction.targetBatteryMode !== undefined && summary.latestAction.targetBatteryMode !== BatteryMode.NoChange) && (
                                                <span className={`tag mode-${getBatteryModeClass(summary.latestAction.targetBatteryMode)}`}>{getBatteryModeLabel(summary.latestAction.targetBatteryMode)}</span>
                                            )}
                                            {(summary.latestAction.targetSolarMode !== undefined && summary.latestAction.targetSolarMode !== SolarMode.NoChange) && (
                                                <span className={`tag solar-${getSolarModeClass(summary.latestAction.targetSolarMode)}`}>{getSolarModeLabel(summary.latestAction.targetSolarMode)}</span>
                                            )}
                                            {summary.latestAction.deficitAt && summary.latestAction.deficitAt !== '0001-01-01T00:00:00Z' && (
                                                <span className="tag tag-info">Deficit: {formatTime(summary.latestAction.deficitAt)}</span>
                                            )}
                                            {summary.latestAction.capacityAt && summary.latestAction.capacityAt !== '0001-01-01T00:00:00Z' && (
                                                <span className="tag tag-info">Capacity: {formatTime(summary.latestAction.capacityAt)}</span>
                                            )}
                                        </div>
                                    </>
                                )}
                                {(summary.hasPrice || summary.hasSOC) && (
                                    <div className="action-footer">
                                        {summary.hasPrice && (
                                            <span>
                                                <span className="price-label">Avg Price:</span>{formatPrice(summary.avgPrice)}
                                                {summary.hasPrice && summary.min !== summary.max && <span className="price-range"> (Range: $ {summary.min.toFixed(3)} - $ {summary.max.toFixed(3)})</span>}                                                        </span>
                                        )}
                                        {summary.hasSOC && (
                                            <span>
                                                <span className="price-label">Battery:</span> {summary.avgSOC.toFixed(1)}%
                                                {summary.minSOC !== summary.maxSOC && <span className="soc-range"> (Range: {summary.minSOC.toFixed(0)}% - {summary.maxSOC.toFixed(0)}%)</span>}
                                            </span>
                                        )}
                                    </div>
                                )}
                            </div>
                        </li>
                    );
                }
                const action = item as Action;
                const reasonText = getReasonText(action);
                const isNegPrice = action.currentPrice && (action.currentPrice.dollarsPerKWH+action.currentPrice.gridUseDollarsPerKWH) < 0;
                const showDeficit = action.deficitAt && action.deficitAt !== '0001-01-01T00:00:00Z';
                const showCapacity = action.capacityAt && action.capacityAt !== '0001-01-01T00:00:00Z';

                const deficitReasons: string[] = [
                    ActionReason.DeficitCharge,
                    ActionReason.DeficitSaveForPeak,
                    ActionReason.DeficitSave,
                    ActionReason.WaitingToCharge,
                    ActionReason.DischargeBeforeCapacity,
                    ActionReason.PreventSolarCurtailment,
                    ActionReason.ChargeSurvivePeak,
                ];
                const showDeficitTag = showDeficit && action.reason && deficitReasons.includes(action.reason);
                const showCapacityTag = showCapacity && (action.reason === ActionReason.DischargeBeforeCapacity || action.reason === ActionReason.PreventSolarCurtailment);

                return (
                    <li key={index} className="action-item">
                        <div className="action-time">
                            {new Date(action.timestamp).toLocaleTimeString()}
                        </div>
                        <div className="action-details">
                            <h3>{getBatteryModeLabel(action.batteryMode)}</h3>
                            <p>{reasonText}</p>
                            <div className="tags">
                                {(action.batteryMode !== BatteryMode.NoChange || (action.targetBatteryMode !== undefined && action.targetBatteryMode !== BatteryMode.NoChange)) && (
                                    <span className={`tag mode-${getBatteryModeClass(action.targetBatteryMode || action.batteryMode)}`}>{getBatteryModeLabel(action.targetBatteryMode || action.batteryMode)}</span>
                                )}
                                {(action.solarMode !== SolarMode.NoChange || (action.targetSolarMode !== undefined && action.targetSolarMode !== SolarMode.NoChange)) && (
                                    <span className={`tag solar-${getSolarModeClass(action.targetSolarMode || action.solarMode)}`}>{getSolarModeLabel(action.targetSolarMode || action.solarMode)}</span>
                                )}
                                {isNegPrice && (
                                    <span className="tag tag-warning">Negative Price</span>
                                )}
                                {showDeficitTag && (
                                    <span className="tag tag-info">Deficit: {formatTime(action.deficitAt!)}</span>
                                )}
                                {showCapacityTag && (
                                    <span className="tag tag-info">Full by: {formatTime(action.capacityAt!)}</span>
                                )}
                                {action.dryRun && (
                                    <span className="tag dry-run">Dry Run</span>
                                )}
                            </div>
                            <div className="action-footer">
                                {action.currentPrice && (
                                    <span>
                                        <span className="price-label">Price:</span>{formatPrice(gridChargeCost(action.currentPrice))}
                                        {action.futurePrice && action.futurePrice.dollarsPerKWH > 0 && (
                                            <span className="price-future"> · Peak: {formatPrice(gridChargeCost(action.futurePrice))}</span>
                                        )}
                                    </span>
                                )}
                                {action.systemStatus && !!action.systemStatus.batterySOC && (
                                    <span className="battery-soc">
                                        <span className="price-label">Battery:</span> {action.systemStatus.batterySOC.toFixed(1)}%
                                    </span>
                                )}
                            </div>
                        </div>
                    </li>
                );
            })}
        </ul>
    );
};

export default ActionTimeline;
