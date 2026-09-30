import { type Action, BatteryMode, SolarMode, ActionReason } from '../api';

export const getBatteryModeLabel = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'Hold Battery';
        case BatteryMode.ChargeAny: return 'Charge From Solar+Grid';
        case BatteryMode.Load: return 'Solar first, then battery';
        case BatteryMode.Export: return 'Export to Grid';
        case BatteryMode.NoChange: return 'No Change';
        default: return 'Unknown';
    }
};

export const getBatteryModeClass = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'standby';
        case BatteryMode.ChargeAny: return 'charge_any';
        case BatteryMode.Load: return 'load';
        case BatteryMode.Export: return 'export';
        case BatteryMode.NoChange: return 'no_change';
        default: return 'unknown';
    }
};

export const getSolarModeLabel = (mode: number) => {
    switch (mode) {
        case SolarMode.NoExport: return 'Use & No Export';
        case SolarMode.Any: return 'Use & Export';
        case SolarMode.Export: return 'Direct Export';
        case SolarMode.NoChange: return 'No Change';
        default: return 'Unknown';
    }
};

export const getSolarModeClass = (mode: number) => {
    switch (mode) {
        case SolarMode.NoExport: return 'no_export';
        case SolarMode.Any: return 'export';
        case SolarMode.Export: return 'direct_export';
        case SolarMode.NoChange: return 'no_change';
        default: return 'unknown';
    }
};

export const formatPrice = (dollars: number) => `$ ${dollars.toFixed(3)}/kWh`;

export const formatCurrency = (amount: number, forceSign: boolean = false) => {
    const sign = amount >= 0 ? (forceSign ? '+ ' : '') : '- ';
    return `${sign}$ ${Math.abs(amount).toFixed(2)}`;
};

export const isZeroTime = (ts?: string): boolean => {
    return !ts || ts.startsWith('0001-01-01');
};

export const getActionTimestamp = (action: Action): string => {
    if (action.systemTimestamp && !isZeroTime(action.systemTimestamp)) {
        return action.systemTimestamp;
    }
    return action.timestamp;
};

export const extractOffsetMinutes = (isoStr?: string): number | null => {
    if (!isoStr || isZeroTime(isoStr) || isoStr.endsWith('Z')) return null;
    const match = isoStr.match(/([+-])(\d{2}):?(\d{2})$/);
    if (!match) return null;
    const sign = match[1] === '+' ? 1 : -1;
    const hours = parseInt(match[2], 10);
    const minutes = parseInt(match[3] || '0', 10);
    return sign * (hours * 60 + minutes);
};

export const formatTimeInOffset = (isoStr?: string, offsetMinutes?: number | null): string => {
    if (!isoStr || isZeroTime(isoStr)) return '';
    try {
        if (offsetMinutes !== null && offsetMinutes !== undefined) {
            const d = new Date(isoStr);
            if (isNaN(d.getTime())) return isoStr;
            const targetMs = d.getTime() + (offsetMinutes * 60 * 1000);
            const targetDate = new Date(targetMs);
            let hour = targetDate.getUTCHours();
            const min = String(targetDate.getUTCMinutes()).padStart(2, '0');
            const ampm = hour >= 12 ? 'PM' : 'AM';
            hour = hour % 12;
            if (hour === 0) hour = 12;
            return `${hour}:${min} ${ampm}`;
        }
        const directOffset = extractOffsetMinutes(isoStr);
        if (directOffset !== null) {
            return formatTimeInOffset(isoStr, directOffset);
        }
        return new Date(isoStr).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
    } catch {
        return isoStr;
    }
};

export const formatTime = (ts?: string, referenceTs?: string): string => {
    if (!ts || isZeroTime(ts)) return '';
    const offset = extractOffsetMinutes(ts) ?? extractOffsetMinutes(referenceTs);
    return formatTimeInOffset(ts, offset);
};

export const formatHour12 = (hour: number): string => {
    const h = ((hour % 24) + 24) % 24;
    const displayHour = h === 0 ? 12 : h > 12 ? h - 12 : h;
    const ampm = h >= 12 ? 'PM' : 'AM';
    return `${displayHour} ${ampm}`;
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
    const refTs = (!action.systemTimestamp || isZeroTime(action.systemTimestamp)) ? action.systemStatus?.timestamp : action.systemTimestamp;
    const getDeficitTimeStr = (act: Action) => {
        if (!isZeroTime(act.deficitAt)) return formatTime(act.deficitAt!, refTs);
        if (!isZeroTime(act.hitBufferedDeficitAt)) return formatTime(act.hitBufferedDeficitAt!, refTs);
        if (!isZeroTime(act.hitThresholdDeficitAt)) return formatTime(act.hitThresholdDeficitAt!, refTs);
        return '';
    };
    const deficitTimeStr = getDeficitTimeStr(action);
    const capacityTimeStr = !isZeroTime(action.capacityAt) ? formatTime(action.capacityAt, refTs) : '';
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
                `Relying on solar and battery now to power the home, since the battery will refill anyway.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DeficitSave: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts: string[] = [];
            if (deficitTimeStr) {
                parts.push(`If we rely on the battery, it would deplete around ${deficitTimeStr}.`);
            }
            parts.push(`Since electricity prices now (${nowCostStr}) are cheap and are expected to remain cheap before the deficit, we can delay charging for now. We are keeping the battery in standby to preserve its remaining energy for the peak period${futureCostStr ? ` (${futureCostStr})` : ''}.`);
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DeficitSaveForPeak: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts: string[] = [];
            if (deficitTimeStr) {
                parts.push(`If we rely on the battery, it would deplete around ${deficitTimeStr}.`);
            }
            parts.push(`Since electricity prices now (${nowCostStr}) are cheap, we are keeping the battery in standby to preserve its remaining energy for the peak period${futureCostStr ? ` (${futureCostStr})` : ''}.`);
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
                `Relying on solar and battery now to power the home to create headroom, ensuring we can capture all solar production later without curtailment.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.HoldSimilarPrice: {
            const parts = [
                `Current electricity price (${nowCostStr}) is comparable to the expected export credit.`,
                `Keeping the battery in standby to save battery wear and using grid power so the battery can refill to 100% and export surplus solar to the grid.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageSave: {
            const parts = [
                `Electricity prices are currently at their peak${nowCostStr ? ` (${nowCostStr})` : ''}. Relying on solar and battery to power the home, avoiding expensive grid imports.`
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.EVChargingStandby: {
            const parts = [
                `EV charging detected during your configured charging period.`,
                `Keeping the home battery in standby to force EV charging from the grid, preserving battery reserves for morning and peak hours.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.SufficientBattery: {
            const parts = [
                'The battery has enough stored energy to meet predicted demand. Relying on solar and battery to cover home load and minimize grid imports.'
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
                parts.push(`If we rely on the battery, it would deplete around ${deficitTimeStr}${comparisonStr}.`);
            }
            parts.push(`Relying on solar and battery now to power the home, and waiting to refill it during ${refillWindowStr}.`);

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
        case ActionReason.DirectExport: {
            if (action.batteryMode === BatteryMode.Export) {
                if (action.solarMode === SolarMode.Export) {
                    const parts = [
                        `High export credit window active${nowCostStr ? ` (${nowCostStr})` : ''}.`,
                        `Discharging battery and exporting solar to the grid at peak rates.`,
                    ];
                    return parts.concat(suffixParts).join(' ');
                }
                const parts = [
                    `High export credit window active${nowCostStr ? ` (${nowCostStr})` : ''}.`,
                    `Discharging battery directly to the grid at peak rates.`,
                ];
                return parts.concat(suffixParts).join(' ');
            }
            if (action.batteryMode === BatteryMode.Standby) {
                const parts = [
                    `High electricity price window active${nowCostStr ? ` (${nowCostStr})` : ''} and battery reserve is limited.`,
                    `Holding remaining battery energy in standby to defend against peak grid imports, while solar generation powers the home and exports surplus to the grid.`,
                ];
                return parts.concat(suffixParts).join(' ');
            }
            const parts = [
                `High solar export credit window active${nowCostStr ? ` (${nowCostStr})` : ''}.`,
                `Powering the home from the battery so 100% of solar generation can export directly to the grid at peak rates.`,
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

export function getPlanStatusSubvalue(action: Action, refTs?: string): string | null {
    if (!action.plan || !action.plan.periods || action.plan.periods.length === 0) {
        return null;
    }

    const periods = action.plan.periods;
    const currentPeriod = periods[0];
    const effectiveMode = action.targetBatteryMode !== undefined && action.targetBatteryMode !== BatteryMode.NoChange
        ? action.targetBatteryMode
        : action.batteryMode;

    const isBatteryAtReserve = action.reason === ActionReason.BatteryAtReserve;
    const isDirectExportReason = action.reason === ActionReason.DirectExport;
    const isDirectSolarExportStandby = isDirectExportReason && action.batteryMode === BatteryMode.Standby;

    let state: 'charging' | 'discharging' | 'standby' = 'standby';
    if (isBatteryAtReserve || isDirectSolarExportStandby) {
        state = 'standby';
    } else if (effectiveMode === BatteryMode.Load || effectiveMode === BatteryMode.Export) {
        state = 'discharging';
    } else if (effectiveMode === BatteryMode.ChargeAny) {
        state = 'charging';
    }

    // Standby: find what we are waiting for or holding for
    if (state === 'standby') {
        if (action.reason === ActionReason.PreventSolarCurtailment) {
            return 'Holding reserve for daytime solar recharge';
        }
        if (action.reason === ActionReason.ArbitrageHoldExport) {
            const nextExport = periods.find(p => p.batteryMode === BatteryMode.Export);
            if (nextExport) {
                return `Holding reserve for ${formatTime(nextExport.startTime, refTs)} export`;
            }
            return 'Holding reserve for upcoming export window';
        }

        // Look for the next mode change in the plan
        for (let i = 1; i < periods.length; i++) {
            const p = periods[i];
            if (p.batteryMode === BatteryMode.ChargeAny) {
                const price = p.price ? (p.price.dollarsPerKWH + (p.price.gridUseDollarsPerKWH || 0)) : null;
                const priceStr = price !== null ? ` ($${price.toFixed(3)}/kWh)` : '';
                return `Waiting to charge at ${formatTime(p.startTime, refTs)}${priceStr}`;
            }
            if (p.batteryMode === BatteryMode.Load && (p.reason === ActionReason.DischargeAtPeak || (p.description && p.description.toLowerCase().includes('peak')))) {
                return `Saving reserve for ${formatTime(p.startTime, refTs)} peak rate`;
            }
            if (p.batteryMode === BatteryMode.Export) {
                return `Holding reserve for ${formatTime(p.startTime, refTs)} export`;
            }
        }

        if (action.reason === ActionReason.DeficitSaveForPeak || action.reason === ActionReason.ArbitrageHoldSave) {
            return 'Saving reserve for upcoming peak rate';
        }
        return 'Holding reserve in standby';
    }

    // Charging: find end of contiguous charge block
    if (state === 'charging') {
        let endChargeTime = currentPeriod.endTime;
        for (let i = 1; i < periods.length; i++) {
            if (periods[i].batteryMode === BatteryMode.ChargeAny) {
                endChargeTime = periods[i].endTime;
            } else {
                break;
            }
        }
        const targetSoc = action.chargeToSoc || (currentPeriod.endSoc ? Math.round(currentPeriod.endSoc) : 100);
        const price = action.currentPrice ? (action.currentPrice.dollarsPerKWH + (action.currentPrice.gridUseDollarsPerKWH || 0)) : null;
        if (price !== null && price < 0.06) {
            return `Charging to ${targetSoc}% until ${formatTime(endChargeTime, refTs)} • Low rate ($${price.toFixed(3)}/kWh)`;
        }
        if (targetSoc > 0 && targetSoc < 100) {
            return `Charging to ${targetSoc}% until ${formatTime(endChargeTime, refTs)}`;
        }
        return `Charging until ${formatTime(endChargeTime, refTs)}`;
    }

    // Discharging:
    if (state === 'discharging') {
        // Direct export
        if (effectiveMode === BatteryMode.Export || action.reason === ActionReason.DirectExport) {
            let endExportTime = currentPeriod.endTime;
            for (let i = 1; i < periods.length; i++) {
                if (periods[i].batteryMode === BatteryMode.Export) {
                    endExportTime = periods[i].endTime;
                } else {
                    break;
                }
            }
            return `Exporting to grid until ${formatTime(endExportTime, refTs)}`;
        }

        // Peak discharge
        let endDischargeTime = currentPeriod.endTime;
        for (let i = 1; i < periods.length; i++) {
            if (periods[i].batteryMode === BatteryMode.Load) {
                endDischargeTime = periods[i].endTime;
            } else {
                break;
            }
        }
        const isPeak = action.reason === ActionReason.DischargeAtPeak || (action.description && action.description.toLowerCase().includes('peak'));
        if (isPeak) {
            const price = action.currentPrice ? (action.currentPrice.dollarsPerKWH + (action.currentPrice.gridUseDollarsPerKWH || 0)) : null;
            const priceStr = price !== null ? ` ($${price.toFixed(3)}/kWh)` : '';
            return `Peak rate defense${priceStr} until ${formatTime(endDischargeTime, refTs)}`;
        }
        return `Powering home on solar & battery`;
    }

    return null;
}
