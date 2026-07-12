import React from 'react';
import { Meter } from '@base-ui/react/meter';
import { type Action, BatteryMode, ActionReason } from '../api';
import { getBatteryModeLabel } from '../utils/dashboardUtils';

interface CurrentStatusProps {
    action: Action;
}

const CurrentStatus: React.FC<CurrentStatusProps> = ({ action }) => {
    const soc = action.systemStatus?.batterySOC ?? 0;
    const hasPrice = action.currentPrice !== undefined;
    const price = hasPrice ? (action.currentPrice!.dollarsPerKWH + (action.currentPrice!.gridUseDollarsPerKWH || 0)) : 0;

    if (action.paused) {
        return (
            <div className="current-status-card paused">
                <div className="status-main">
                    <div className="status-icon">
                        <span className="icon" aria-hidden="true">⏸️</span>
                    </div>
                    <div className="status-info">
                        <span className="status-label">System Paused</span>
                        <span className="status-value">Automation is currently paused</span>
                    </div>
                </div>
                <div className="status-metrics">
                    <div className="metric">
                        <span className="metric-label">Battery</span>
                        <span className="metric-value">{soc.toFixed(1)}%</span>
                        <Meter.Root className="battery-bar" value={soc} min={0} max={100} aria-label="Battery Percentage">
                            <Meter.Track className="battery-track">
                                <Meter.Indicator className="battery-fill" />
                            </Meter.Track>
                        </Meter.Root>
                    </div>
                    {hasPrice && (
                        <div className="metric">
                            <span className="metric-label">Price</span>
                            <span className="metric-value">$ {price.toFixed(3)}<small>/kWh</small></span>
                        </div>
                    )}
                </div>
            </div>
        );
    }

    if (action.systemStatus?.gridUnavailable) {
        return (
            <div className="current-status-card grid-unavailable">
                <div className="status-main">
                    <div className="status-icon">
                        <span className="icon" aria-hidden="true">⚠️</span>
                    </div>
                    <div className="status-info">
                        <span className="status-label">Grid Unavailable</span>
                        <span className="status-value">Grid is currently down</span>
                    </div>
                </div>
                <div className="status-metrics">
                    <div className="metric">
                        <span className="metric-label">Battery</span>
                        <span className="metric-value">{soc.toFixed(1)}%</span>
                        <Meter.Root className="battery-bar" value={soc} min={0} max={100} aria-label="Battery Percentage">
                            <Meter.Track className="battery-track">
                                <Meter.Indicator className="battery-fill" />
                            </Meter.Track>
                        </Meter.Root>
                    </div>
                </div>
            </div>
        );
    }

    if (action.systemStatus?.vppActive) {
        const vppKW = action.systemStatus?.vppKW || 0;
        return (
            <div className="current-status-card vpp">
                <div className="status-main">
                    <div className="status-icon">
                        <span className="icon" aria-hidden="true">🔌</span>
                    </div>
                    <div className="status-info">
                        <span className="status-label">VPP Event Active</span>
                        <span className="status-value">Grid Services active ({vppKW.toFixed(1)} kW)</span>
                    </div>
                </div>
                <div className="status-metrics">
                    <div className="metric">
                        <span className="metric-label">Battery</span>
                        <span className="metric-value">{soc.toFixed(1)}%</span>
                        <Meter.Root className="battery-bar" value={soc} min={0} max={100} aria-label="Battery Percentage">
                            <Meter.Track className="battery-track">
                                <Meter.Indicator className="battery-fill" />
                            </Meter.Track>
                        </Meter.Root>
                    </div>
                    {hasPrice && (
                        <div className="metric">
                            <span className="metric-label">Price</span>
                            <span className="metric-value">$ {price.toFixed(3)}<small>/kWh</small></span>
                        </div>
                    )}
                </div>
            </div>
        );
    }

    const isBatteryAtReserve = action.reason === ActionReason.BatteryAtReserve;

    const effectiveBatteryMode = action.targetBatteryMode
        ? action.targetBatteryMode
        : action.batteryMode;
    const mode = effectiveBatteryMode;

    let state: 'charging' | 'discharging' | 'standby' = 'standby';
    if (isBatteryAtReserve) {
        state = 'standby';
    } else if (mode === BatteryMode.Load) {
        state = 'discharging';
    } else if (mode === BatteryMode.ChargeAny || mode === BatteryMode.ChargeSolar) {
        state = 'charging';
    }

    const capacityAt = action.capacityAt ? new Date(action.capacityAt) : null;
    const deficitAt = action.deficitAt ? new Date(action.deficitAt) : null;

    const isValidDate = (d: Date | null) => {
        return d && !isNaN(d.getTime()) && d.getFullYear() > 1970;
    };

    const formatDuration = (ms: number): string => {
        const minutes = Math.round(ms / 60000);
        if (minutes < 1) {
            return 'less than a minute';
        }
        if (minutes < 60) {
            return `${minutes} minute${minutes === 1 ? '' : 's'}`;
        }
        const hours = Math.round(minutes / 60);
        return `${hours} hour${hours === 1 ? '' : 's'}`;
    };

    const now = new Date();
    const capacityMs = (capacityAt && isValidDate(capacityAt)) ? capacityAt.getTime() - now.getTime() : null;
    const deficitMs = (deficitAt && isValidDate(deficitAt)) ? deficitAt.getTime() - now.getTime() : null;

    let timeRemainingText = '';
    const capValid = capacityMs !== null && capacityMs > 0;
    const defValid = deficitMs !== null && deficitMs > 0;

    if (defValid) {
        timeRemainingText = `Deficit in ${formatDuration(deficitMs)}`;
    } else if (capValid) {
        timeRemainingText = `Capacity in ${formatDuration(capacityMs)}`;
    }

    const statusLabel = isBatteryAtReserve
        ? 'Battery At Reserve'
        : state === 'discharging'
        ? 'Self-Powered'
        : `System ${state.charAt(0).toUpperCase() + state.slice(1)}`;

    const statusValue = isBatteryAtReserve
        ? 'Holding Reserve'
        : state === 'discharging'
        ? 'Rely on Solar & Battery'
        : getBatteryModeLabel(mode);

    return (
        <div className={`current-status-card ${state}`}>
            <div className="status-main">
                <div className="status-icon">
                    {isBatteryAtReserve ? (
                        <span className="icon" aria-hidden="true">🔋</span>
                    ) : (
                        <>
                            {state === 'charging' && <span className="icon" aria-hidden="true">⚡</span>}
                            {state === 'discharging' && <span className="icon" aria-hidden="true">🏠</span>}
                            {state === 'standby' && <span className="icon" aria-hidden="true">⏲️</span>}
                        </>
                    )}
                </div>
                <div className="status-info">
                    <span className="status-label">{statusLabel}</span>
                    <span className="status-value">{statusValue}</span>
                    {timeRemainingText && (
                        <span className="status-subvalue">{timeRemainingText}</span>
                    )}
                </div>
            </div>
            <div className="status-metrics">
                <div className="metric">
                    <span className="metric-label">Battery</span>
                    <span className="metric-value">{soc.toFixed(1)}%</span>
                    <Meter.Root className="battery-bar" value={soc} min={0} max={100} aria-label="Battery Percentage">
                        <Meter.Track className="battery-track">
                            <Meter.Indicator className="battery-fill" />
                        </Meter.Track>
                    </Meter.Root>
                </div>
                {hasPrice && (
                    <div className="metric">
                        <span className="metric-label">Price</span>
                        <span className="metric-value">$ {price.toFixed(3)}<small>/kWh</small></span>
                    </div>
                )}
            </div>
        </div>
    );
};

export default CurrentStatus;
