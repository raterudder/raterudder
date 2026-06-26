import { type Action, BatteryMode, SolarMode, ActionReason } from '../api';

export const getBatteryModeLabel = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'Hold Battery';
        case BatteryMode.ChargeAny: return 'Charge From Solar+Grid';
        case BatteryMode.ChargeSolar: return 'Charge From Solar';
        case BatteryMode.Load: return 'Use Battery';
        case BatteryMode.NoChange: return 'No Change';
        default: return 'Unknown';
    }
};

export const getBatteryModeClass = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'standby';
        case BatteryMode.ChargeAny: return 'charge_any';
        case BatteryMode.ChargeSolar: return 'charge_solar';
        case BatteryMode.Load: return 'load';
        case BatteryMode.NoChange: return 'no_change';
        default: return 'unknown';
    }
};

export const getSolarModeLabel = (mode: number) => {
    switch (mode) {
        case SolarMode.NoExport: return 'Use & No Export';
        case SolarMode.Any: return 'Use & Export';
        case SolarMode.NoChange: return 'No Change';
        default: return 'Unknown';
    }
};

export const getSolarModeClass = (mode: number) => {
    switch (mode) {
        case SolarMode.NoExport: return 'no_export';
        case SolarMode.Any: return 'export';
        case SolarMode.NoChange: return 'no_change';
        default: return 'unknown';
    }
};

export const formatPrice = (dollars: number) => `$ ${dollars.toFixed(3)}/kWh`;

export const formatCurrency = (amount: number, forceSign: boolean = false) => {
    const sign = amount >= 0 ? (forceSign ? '+ ' : '') : '- ';
    return `${sign}$ ${Math.abs(amount).toFixed(2)}`;
};

export const formatTime = (ts: string) => {
    try {
        return new Date(ts).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
    } catch {
        return ts;
    }
};

// gridChargeCost returns the effective grid charging cost (base price + delivery adder).
export const gridChargeCost = (price: { dollarsPerKWH: number; gridUseDollarsPerKWH?: number }): number =>
    price.dollarsPerKWH + (price.gridUseDollarsPerKWH ?? 0);

export const getReasonText = (action: Action): string => {
    const reason = action.reason;
    if (!reason) {
        return action.description;
    }

    const currentPrice = action.currentPrice;
    const futurePrice = action.futurePrice;
    const nowCost = currentPrice ? gridChargeCost(currentPrice) : null;
    const futureCost = futurePrice ? gridChargeCost(futurePrice) : null;
    const nowCostStr = nowCost !== null ? formatPrice(nowCost) : '';
    const futureCostStr = futureCost !== null ? formatPrice(futureCost) : '';
    const getDeficitTimeStr = (act: Action) => {
        const isZero = (ts?: string) => !ts || ts === '0001-01-01T00:00:00Z';
        if (!isZero(act.deficitAt)) return formatTime(act.deficitAt!);
        if (!isZero(act.hitBelowDeficitAt)) return formatTime(act.hitBelowDeficitAt!);
        if (!isZero(act.hitAboveDeficitAt)) return formatTime(act.hitAboveDeficitAt!);
        return '';
    };
    const deficitTimeStr = getDeficitTimeStr(action);
    const capacityTimeStr = action.capacityAt ? formatTime(action.capacityAt) : '';
    const isNegativePrice = currentPrice && currentPrice.dollarsPerKWH < 0;
    const solarMode = action.targetSolarMode || action.solarMode ;

    const suffixParts = [];

    if (isNegativePrice && solarMode === SolarMode.NoExport) {
        suffixParts.push('Disabled solar export because the price is negative.');
    }

    switch (reason) {
        case ActionReason.AlwaysChargeBelowThreshold: {
            const parts = [
                `Current price (${nowCostStr}) is below your always-charge threshold.`,
                `Charging the battery now to lock in this low rate.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.MissingBattery:
            return 'No battery capacity was detected. The system is standing by until battery information is available.';
        case ActionReason.DeficitCharge: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            let costComparison = '';
            if (nowCost !== null && futureCost !== null) {
                const diff = futureCost - nowCost;
                if (diff >= 0.01) {
                    costComparison = `Charging now at ${nowCostStr} is cheaper than the cheapest future charging window${futureCostStr ? ` (${futureCostStr})` : ''}.`;
                } else if (diff <= -0.01) {
                    costComparison = `Charging now at ${nowCostStr} is more expensive than the cheapest future charging window${futureCostStr ? ` (${futureCostStr})` : ''}.`;
                } else {
                    costComparison = `Charging now at ${nowCostStr} is the same price as the cheapest future charging window${futureCostStr ? ` (${futureCostStr})` : ''}.`;
                }
            } else {
                costComparison = `Charging now${nowCostStr ? ` at ${nowCostStr}` : ''} is cheaper than the cheapest future charging window${futureCostStr ? ` (${futureCostStr})` : ''}.`;
            }
            const parts: string[] = [];
            if (deficitTimeStr) {
                parts.push(`If we do not charge, the battery would deplete around ${deficitTimeStr}.`);
            }
            parts.push(costComparison);
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageChargeExport: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Forecast shows higher prices later${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Charging the battery cheaply now to cover home load during the peak, allowing us to export maximum solar to the grid at higher rates.`,
            ];
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageCharge:
        case ActionReason.ArbitrageChargeSave: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Forecast shows higher electricity prices later${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Charging the battery cheaply now so we can use stored energy later and avoid buying from the grid during the expensive window.`,
            ];
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DischargeBeforeCapacity: {
            const parts = [
                `Solar generation is forecast to fully charge the battery${capacityTimeStr ? ` by ${capacityTimeStr}` : ''} before the next predicted deficit${deficitTimeStr ? ` at ${deficitTimeStr}` : ''}.`,
                `Using the battery now to power the home, since it will refill anyway.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DeficitSave:
        case ActionReason.DeficitSaveForPeak: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts: string[] = [];
            if (deficitTimeStr) {
                parts.push(`If discharged, the battery would deplete around ${deficitTimeStr}.`);
            }
            parts.push(`Since electricity prices now (${nowCostStr}) are cheap and are expected to remain cheap before the deficit, we can delay charging for now. We are keeping the battery in standby to preserve its remaining energy for the peak period${futureCostStr ? ` (${futureCostStr})` : ''}.`);
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.WaitingToCharge: {
            const delta = nowCost !== null && futureCost !== null ? nowCost - futureCost : null;

            let parts: string[] = [];
            if (delta !== null && delta < 0.01) {
                parts = [
                    `A charging window is coming up which is similar in price or cheaper than now.`,
                    `Holding off grid-charging the batteries and keeping them in standby until then.`,
                ];
            } else {
                parts = [
                    `A cheaper charging window is coming up${futureCostStr ? ` at ${futureCostStr}` : ''} compared to now (${nowCostStr}).`,
                    `Holding off grid-charging the batteries and keeping them in standby until then.`,
                ];
                if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta)}.`);
            }
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.PreventSolarCurtailment: {
            const parts = [
                `Solar generation is forecast to exceed battery capacity${capacityTimeStr ? ` by ${capacityTimeStr}` : ''}.`,
                `Discharging the battery now to create headroom, ensuring we can capture all solar production later without curtailment.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageSave: {
            const parts = [
                `Electricity prices are currently at their peak${nowCostStr ? ` (${nowCostStr})` : ''}. Discharging the battery to power the home, avoiding expensive grid imports.`
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.SufficientBattery: {
            const parts = [
                'The battery has enough stored energy to meet predicted demand. Discharging the battery to cover home load and minimize grid imports.'
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.SufficientBatteryTillCharge: {
            const delta = nowCost !== null && futureCost !== null ? nowCost - futureCost : null;
            let comparisonStr = '';
            if (nowCost !== null && futureCost !== null) {
                const diff = nowCost - futureCost;
                if (diff >= 0.01) {
                    comparisonStr = `, but a cheaper charging window is coming up${futureCostStr ? ` (${futureCostStr})` : ''} compared to now (${nowCostStr})`;
                } else if (diff <= -0.01) {
                    comparisonStr = `, but a more expensive charging window is coming up${futureCostStr ? ` (${futureCostStr})` : ''} compared to now (${nowCostStr})`;
                } else {
                    comparisonStr = `, but a charging window with the same price is coming up${futureCostStr ? ` (${futureCostStr})` : ''} compared to now (${nowCostStr})`;
                }
            } else {
                comparisonStr = `, but a cheaper charging window is coming up${futureCostStr ? ` (${futureCostStr})` : ''}`;
            }

            const refillWindowStr = nowCost !== null && futureCost !== null && Math.abs(nowCost - futureCost) < 0.01 ? 'that window' : 'the cheaper window';
            const parts: string[] = [];
            if (deficitTimeStr) {
                parts.push(`If discharged, the battery would deplete around ${deficitTimeStr}${comparisonStr}.`);
            }
            parts.push(`Discharging the battery now to power the home, and waiting to refill it during ${refillWindowStr}.`);

            if (delta !== null && delta >= 0.01) {
                parts.push(`Estimated savings: ${formatPrice(delta)}.`);
            }
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.GridUnavailable:
            return 'Grid is currently unavailable. The system is standing by to protect the battery and ensure power is available for the home.';
        case ActionReason.VPPActive:
            return 'Virtual Power Plant (VPP) event is currently active. Automation is temporarily disabled to allow grid services to run.';
        case ActionReason.VPPPrep: {
            const parts = [
                'Preparing for upcoming Virtual Power Plant (VPP) event.'
            ];
            if (nowCostStr && futureCostStr) {
                parts.push(`Charging the battery now at ${nowCostStr} is cheaper than charging later before the event at ${futureCostStr} to ensure we enter the event with maximum capacity.`);
            } else {
                parts.push('Pre-charging the battery from the grid now to ensure we enter the VPP event with maximum capacity.');
            }
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.BatteryAtReserve: {
            const parts = [
                'Battery is at reserve. Using remaining energy because standby is not meaningful (battery is already held at reserve).',
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageHoldExport: {
            const parts = [
                `An export arbitrage window is coming up${futureCostStr ? ` (${futureCostStr})` : ''} with higher rates than now (${nowCostStr}).`,
                `Keeping the battery in standby to preserve stored energy, allowing maximum solar export to the grid during the peak period.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageHold:
        case ActionReason.ArbitrageHoldSave: {
            const parts = [
                `An arbitrage window is coming up${futureCostStr ? ` (${futureCostStr})` : ''} with higher rates than now (${nowCostStr}).`,
                `Keeping the battery in standby to preserve stored energy so we can avoid importing from the grid during the peak period.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        default:
            return action.description || `Unknown reason: ${reason}`;
    }
};
export type SummaryType = 'no_change' | 'fault' | 'grouped';

export interface ActionSummary {
    isSummary: true;
    type: SummaryType;
    reason?: ActionReason;
    latestAction: Action;
    startTime: string;
    endTime?: string;
    avgPrice: number;
    min: number;
    max: number;
    avgSOC: number;
    minSOC: number;
    maxSOC: number;
    count: number;
    alarms: Set<string>;
    storms: Set<string>;
    stormStart?: Date;
    stormEnd?: Date;
    hasPrice: boolean;
    hasSOC: boolean;
}

export interface ActionSummaryAccumulator extends Omit<ActionSummary, 'avgPrice' | 'avgSOC'> {
    priceTotal: number;
    priceCount: number;
    socTotal: number;
    socCount: number;
}
