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
                `The battery will deplete${deficitTimeStr ? ` around ${deficitTimeStr}` : ''}.`,
                `Charging now at ${nowCostStr} is cheaper than the best future window${futureCostStr ? ` (${futureCostStr})` : ''}.`,
            ];
            if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageCharge: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Forecast shows higher prices later${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Charging the battery cheaply now so we can use the battery later during the expensive window.`,
            ];
            if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DischargeBeforeCapacity: {
            const parts = [
                `Solar generation will fill the battery${capacityTimeStr ? ` by ${capacityTimeStr}` : ''} before battery depletion${deficitTimeStr ? ` (expected ${deficitTimeStr})` : ''}.`,
                `Using the battery now rather than letting it go to waste.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DeficitSave:
        case ActionReason.DeficitSaveForPeak: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `The battery will deplete${deficitTimeStr ? ` around ${deficitTimeStr}` : ''}, but forecasted electricity prices are higher${futureCostStr ? ` (${futureCostStr})` : ''} than now (${nowCostStr}).`,
                `Holding the battery in reserve so it can offset those higher costs.`,
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
                    `Holding off charging the batteries until then.`,
                ];
            } else {
                parts = [
                    `A cheaper charging window is coming up${futureCostStr ? ` at ${futureCostStr}` : ''} which is cheaper than now (${nowCostStr}).`,
                    `Holding off charging the batteries until then.`,
                ];
                if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
            }
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ChargeSurvivePeak: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Battery will deplete before forecasted high-price period${futureCostStr ? ` (${futureCostStr})` : ''}.`,
                `Charging now at the current rate (${nowCostStr}) so we can use the battery later during the expensive window.`,
            ];
            if (delta !== null) parts.push(`Estimated savings: ${formatPrice(delta)}/kWh.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.PreventSolarCurtailment: {
            const parts = [
                `Solar generation will exceed battery capacity${capacityTimeStr ? ` by ${capacityTimeStr}` : ''}.`,
                `Using the battery now to create headroom so we don't have to curtail solar production later.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageSave: {
            const parts = [
                `Electricity prices are at their peak${nowCostStr ? ` (${nowCostStr})` : ''}. Using the battery to avoid paying the highest rates of the day.`
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.SufficientBattery: {
            const parts = [
                'The battery has enough stored energy to meet predicted demand. Using the battery normally to reduce grid usage.'
            ];
            return parts.concat(suffixParts).join(' ');
        }
        default:
            return action.description || `Unknown reason: ${reason}`;
    }
};
export type SummaryType = 'no_change' | 'fault';

export interface ActionSummary {
    isSummary: true;
    type: SummaryType;
    reason?: ActionReason;
    latestAction: Action;
    startTime: string;
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
