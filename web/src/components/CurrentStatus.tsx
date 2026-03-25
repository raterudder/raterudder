import React from 'react';
import { Meter } from '@base-ui/react/meter';
import { type Action, BatteryMode } from '../api';
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
                        <span className="icon">⏸️</span>
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
                        <span className="icon">⚠️</span>
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

    const effectiveBatteryMode = action.targetBatteryMode
        ? action.targetBatteryMode
        : action.batteryMode;
    const mode = effectiveBatteryMode;
    const kw = action.systemStatus?.batteryKW || 0;

    let state: 'charging' | 'discharging' | 'standby' = 'standby';
    if (mode === BatteryMode.Load || kw > 0.1) state = 'discharging';
    else if (mode === BatteryMode.ChargeAny || mode === BatteryMode.ChargeSolar || kw < -0.1) state = 'charging';

    return (
        <div className={`current-status-card ${state}`}>
            <div className="status-main">
                <div className="status-icon">
                    {state === 'charging' && <span className="icon">⚡</span>}
                    {state === 'discharging' && <span className="icon">🏠</span>}
                    {state === 'standby' && <span className="icon">⏲️</span>}
                </div>
                <div className="status-info">
                    <span className="status-label">System {state.charAt(0).toUpperCase() + state.slice(1)}</span>
                    <span className="status-value">{getBatteryModeLabel(mode)}</span>
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
