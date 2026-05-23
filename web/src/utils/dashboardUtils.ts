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
    const deficitTimeStr = action.deficitAt ? formatTime(action.deficitAt) : '';
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
            const parts = [
                `If we do not charge, the battery would deplete${deficitTimeStr ? ` around ${deficitTimeStr}` : ''}.`,
                `Charging now at ${nowCostStr} is cheaper than the cheapest future charging window${futureCostStr ? ` (${futureCostStr})` : ''}.`,
            ];
            if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageChargeExport: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Forecast shows higher prices later${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Charging the battery cheaply now to cover home load during the peak, allowing us to export maximum solar to the grid at higher rates.`,
            ];
            if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageCharge:
        case ActionReason.ArbitrageChargeSave: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Forecast shows higher electricity prices later${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Charging the battery cheaply now so we can use stored energy later and avoid buying from the grid during the expensive window.`,
            ];
            if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
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
            const parts = [
                `If discharged, the battery would deplete${deficitTimeStr ? ` around ${deficitTimeStr}` : ''}.`,
                `Since electricity prices now (${nowCostStr}) are cheap and are expected to remain cheap before the deficit, we can delay charging for now. We are keeping the battery in standby to preserve its remaining energy for the peak period${futureCostStr ? ` (${futureCostStr})` : ''}.`,
            ];
            if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
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
                if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
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
            const parts = [
                `If discharged, the battery would deplete${deficitTimeStr ? ` around ${deficitTimeStr}` : ''}, but a cheaper charging window is coming up${futureCostStr ? ` (${futureCostStr})` : ''} compared to now (${nowCostStr}).`,
                `Discharging the battery now to power the home, and waiting to refill it during the cheaper window.`,
            ];
            if (delta !== null && delta > 0) {
                parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
            }
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.GridUnavailable:
            return 'Grid is currently unavailable. The system is standing by to protect the battery and ensure power is available for the home.';
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
