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

export const getBatteryPillLabel = (mode: number) => {
    switch (mode) {
        case BatteryMode.Standby: return 'Hold Battery';
        case BatteryMode.ChargeAny: return 'Charge From Solar+Grid';
        case BatteryMode.Load: return 'Power Home';
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

export const getCurrencySymbol = (utilityProvider?: string | null): string => {
    switch (utilityProvider) {
        case 'octopus':
            return '£';
        default:
            return '$';
    }
};

export const formatPrice = (dollars: number, symbol: string = '$') => `${symbol} ${dollars.toFixed(3)}/kWh`;

export const formatCurrency = (amount: number, forceSign: boolean = false, symbol: string = '$') => {
    const sign = amount >= 0 ? (forceSign ? '+ ' : '') : '- ';
    return `${sign}${symbol} ${Math.abs(amount).toFixed(2)}`;
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

export const getReasonText = (action: Action, symbol: string = '$'): string => {
    const reason = action.reason;
    if (!reason) {
        return action.description;
    }

    const currentPrice = action.currentPrice;
    const futurePrice = action.futurePrice;
    const nowCost = currentPrice ? gridChargeCost(currentPrice) : null;
    const futureCost = futurePrice ? gridChargeCost(futurePrice) : null;
    const nowCostStr = nowCost !== null ? formatPrice(nowCost, symbol) : '';
    const futureCostStr = futureCost !== null ? formatPrice(futureCost, symbol) : '';
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
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta, symbol)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageChargeExport: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Forecast shows higher prices later${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Charging the battery cheaply now to cover home load during the peak, allowing us to export maximum solar to the grid at higher rates.`,
            ];
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta, symbol)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageCharge:
        case ActionReason.ArbitrageChargeSave: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts = [
                `Forecast shows higher electricity prices later${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Charging the battery cheaply now so we can use stored energy later and avoid buying from the grid during the expensive window.`,
            ];
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta, symbol)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DischargeBeforeCapacity: {
            const parts = [
                `Solar generation is forecast to fully charge the battery${capacityTimeStr ? ` by ${capacityTimeStr}` : ''} before it would run low${deficitTimeStr ? ` at ${deficitTimeStr}` : ''}.`,
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
            parts.push(`Since electricity prices now (${nowCostStr}) are cheap and are expected to remain cheap before the battery runs low, we can delay charging for now. We are keeping the battery in standby to preserve its remaining energy for the peak period${futureCostStr ? ` (${futureCostStr})` : ''}.`);
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta, symbol)}.`);
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.DeficitSaveForPeak: {
            const delta = nowCost !== null && futureCost !== null ? futureCost - nowCost : null;
            const parts: string[] = [];
            if (deficitTimeStr) {
                parts.push(`If we rely on the battery, it would deplete around ${deficitTimeStr}.`);
            }
            parts.push(`Since electricity prices now (${nowCostStr}) are cheap, we are keeping the battery in standby to preserve its remaining energy for the peak period${futureCostStr ? ` (${futureCostStr})` : ''}.`);
            if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta, symbol)}.`);
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
                if (delta !== null && delta >= 0.01) parts.push(`Estimated savings: ${formatPrice(delta, symbol)}.`);
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
                parts.push(`Estimated savings: ${formatPrice(delta, symbol)}.`);
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
            if (
                action.recentHomeUsageAbnormal &&
                action.recentHomeUsageKWH != null &&
                action.q3HomeUsageKWH != null
            ) {
                const parts = [
                    `Battery is at reserve. Recent home usage (${action.recentHomeUsageKWH.toFixed(1)} kWh) was well above your normal usage (${action.q3HomeUsageKWH.toFixed(1)} kWh), depleting the reserve buffer.`,
                ];
                return parts.concat(suffixParts).join(' ');
            }
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
                `Higher export credit rates are coming up${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
                `Keeping the battery in standby to preserve stored energy, allowing maximum solar export to the grid during the peak period.`,
            ];
            return parts.concat(suffixParts).join(' ');
        }
        case ActionReason.ArbitrageHold:
        case ActionReason.ArbitrageHoldSave: {
            const parts = [
                `Higher electricity prices are coming up${futureCostStr ? ` (${futureCostStr})` : ''} compared to right now (${nowCostStr}).`,
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
    startSOC?: number;
    endSOC?: number;
    count: number;
    alarms: Set<string>;
    storms: Set<string>;
    stormStart?: Date;
    stormEnd?: Date;
    hasPrice: boolean;
    hasSOC: boolean;
    hasSolar?: boolean;
}

export interface ActionSummaryAccumulator extends Omit<ActionSummary, 'avgPrice' | 'avgSOC'> {
    priceTotal: number;
    priceCount: number;
    socTotal: number;
    socCount: number;
}

export const formatSOCRange = (startSOC: number, endSOC: number): string => {
    const startRound = Math.round(startSOC);
    const endRound = Math.round(endSOC);
    if (Math.abs(endRound - startRound) >= 1) {
        return `${startRound}% → ${endRound}%`;
    }
    return `${endRound}%`;
};

export const hasSolarActivity = (item: Action | ActionSummary): boolean => {
    if ('isSummary' in item) {
        if (item.hasSolar !== undefined) {
            return item.hasSolar;
        }
        return (item.latestAction.systemStatus?.solarKW ?? 0) > 0;
    }
    return (item.systemStatus?.solarKW ?? 0) > 0;
};

export const getActionTitle = (action: Action, isSummaryFault = false): string => {
    const isFault = !!action.fault || isSummaryFault;
    const hasStorms = Boolean(action.systemStatus?.storms && action.systemStatus.storms.length > 0);
    const isEmergency = hasStorms || action.reason === ActionReason.EmergencyMode;
    const isVPP = !!action.systemStatus?.vppActive || action.reason === ActionReason.VPPActive;

    if (isVPP) {
        return 'VPP Event Active';
    }
    if (isEmergency) {
        return hasStorms ? 'Storm Hedge Mode' : 'Emergency Mode';
    }
    if (action.reason === ActionReason.GridUnavailable || action.systemStatus?.gridUnavailable) {
        return 'Grid Unavailable';
    }
    if (isFault) {
        return 'System Fault';
    }
    if (action.batteryMode === BatteryMode.NoChange) {
        return 'No Change';
    }

    switch (action.reason) {
        case ActionReason.BatteryAtReserve:
            return 'Battery At Reserve';
        case ActionReason.DirectExport:
            if (action.batteryMode === BatteryMode.Export) {
                return action.solarMode === SolarMode.Export ? 'Battery & Solar Grid Export' : 'Direct Battery Export';
            }
            if (action.batteryMode === BatteryMode.Standby) {
                return 'Peak Defense Standby';
            }
            return 'Direct Solar Export';
        case ActionReason.ArbitrageHoldExport:
        case ActionReason.ArbitrageHoldSave:
        case ActionReason.ArbitrageHold:
            return 'Saving Battery for Peak';
        case ActionReason.HoldSimilarPrice:
            return 'Standby for Similar Price';
        case ActionReason.VPPPrep:
            return 'VPP Pre-Charging';
        case ActionReason.AlwaysChargeBelowThreshold:
            return 'Low-Rate Grid Charge';
        case ActionReason.DeficitCharge:
            return 'Topping Up Battery';
        case ActionReason.ArbitrageChargeExport:
        case ActionReason.ArbitrageChargeSave:
        case ActionReason.ArbitrageCharge:
            return 'Charging Before Peak';
        case ActionReason.SufficientBattery:
            return 'Self-Powered';
        case ActionReason.SufficientBatteryTillCharge:
            return 'Using Battery Until Cheap Window';
        case ActionReason.DischargeBeforeCapacity:
            return 'Using Battery Before Solar Refill';
        case ActionReason.PreventSolarCurtailment:
            return 'Making Room for Solar';
        case ActionReason.DischargeAtPeak:
            return 'Peak Rate Defense';
        case ActionReason.DeficitSave:
        case ActionReason.DeficitSaveForPeak:
            return 'Saving Battery for Peak';
        case ActionReason.WaitingToCharge:
            return 'Waiting for Cheaper Rate';
        case ActionReason.EVChargingStandby:
            return 'EV Charging Standby';
        case ActionReason.MissingBattery:
            return 'Missing Battery Info';
        default:
            return getBatteryModeLabel(action.batteryMode);
    }
};

export type SummaryPhaseCategory =
    | 'charge'
    | 'solarCharge'
    | 'load'
    | 'solarExport'
    | 'export'
    | 'standby'
    | 'vpp'
    | 'fault';

export interface DaySummarySegment {
    category: SummaryPhaseCategory;
    leftPercent: number;
    widthPercent: number;
}

export interface DaySummaryBeat {
    category: SummaryPhaseCategory;
    headline: string;
    timeLabel: string;
    socText?: string;
    priceText?: string;
}

export interface DaySummaryLegendItem {
    category: SummaryPhaseCategory;
    label: string;
}

export interface DaySummaryData {
    segments: DaySummarySegment[];
    beats: DaySummaryBeat[];
    legend: DaySummaryLegendItem[];
}

const SUMMARY_LEGEND_ORDER: DaySummaryLegendItem[] = [
    { category: 'solarCharge', label: 'Solar Charge' },
    { category: 'load', label: 'Powering Home' },
    { category: 'solarExport', label: 'Solar Export' },
    { category: 'standby', label: 'Standby' },
    { category: 'charge', label: 'Grid Charge' },
    { category: 'export', label: 'Grid Export' },
    { category: 'vpp', label: 'VPP Event' },
    { category: 'fault', label: 'Alert' },
];

const getMinutesFromMidnight = (isoStr?: string, referenceTs?: string): number => {
    if (!isoStr || isZeroTime(isoStr)) return 0;
    try {
        const d = new Date(isoStr);
        if (isNaN(d.getTime())) return 0;
        const offsetMinutes = extractOffsetMinutes(isoStr) ?? extractOffsetMinutes(referenceTs);
        if (offsetMinutes !== null && offsetMinutes !== undefined) {
            const targetDate = new Date(d.getTime() + offsetMinutes * 60 * 1000);
            return targetDate.getUTCHours() * 60 + targetDate.getUTCMinutes();
        }
        return d.getHours() * 60 + d.getMinutes();
    } catch {
        return 0;
    }
};

const classifySummaryItem = (
    item: Action | ActionSummary,
    startSOC: number,
    endSOC: number,
    hasSOC: boolean
): SummaryPhaseCategory => {
    const action = 'isSummary' in item ? item.latestAction : item;
    const isSummaryFault = 'isSummary' in item && item.type === 'fault';
    const isVPP = !!action.systemStatus?.vppActive || action.reason === ActionReason.VPPActive;
    if (isVPP) return 'vpp';

    const hasStorms = Boolean(action.systemStatus?.storms && action.systemStatus.storms.length > 0);
    const isFaultOrEmergency =
        !!action.fault ||
        isSummaryFault ||
        hasStorms ||
        action.reason === ActionReason.EmergencyMode ||
        action.reason === ActionReason.GridUnavailable ||
        !!action.systemStatus?.gridUnavailable;
    if (isFaultOrEmergency) return 'fault';

    const effectiveMode =
        action.targetBatteryMode !== undefined && action.targetBatteryMode !== BatteryMode.NoChange
            ? action.targetBatteryMode
            : action.batteryMode;

    if (effectiveMode === BatteryMode.ChargeAny) {
        return 'charge';
    }
    if (effectiveMode === BatteryMode.Export) {
        return 'export';
    }
    if (action.reason === ActionReason.DirectExport && effectiveMode !== BatteryMode.Standby) {
        return 'solarExport';
    }
    if (
        (hasSOC && Math.round(endSOC) - Math.round(startSOC) >= 1) ||
        (!('isSummary' in item) &&
            (action.systemStatus?.batteryKW ?? 0) < -0.1 &&
            (action.systemStatus?.solarKW ?? 0) > 0)
    ) {
        return 'solarCharge';
    }
    if (effectiveMode === BatteryMode.Load && action.reason !== ActionReason.BatteryAtReserve) {
        return 'load';
    }
    return 'standby';
};

const getSummaryBeatHeadline = (
    category: SummaryPhaseCategory,
    action: Action,
    isCurrent: boolean
): string => {
    const hasStorms = Boolean(action.systemStatus?.storms && action.systemStatus.storms.length > 0);
    switch (category) {
        case 'charge':
            if (action.reason === ActionReason.VPPPrep) {
                return isCurrent ? 'Pre-charging for VPP' : 'Pre-charged for VPP';
            }
            if (action.reason === ActionReason.AlwaysChargeBelowThreshold) {
                return isCurrent ? 'Charging at low price' : 'Charged at low price';
            }
            if (
                action.reason === ActionReason.ArbitrageChargeExport ||
                action.reason === ActionReason.ArbitrageChargeSave ||
                action.reason === ActionReason.ArbitrageCharge
            ) {
                return isCurrent ? 'Charging before peak' : 'Charged before peak';
            }
            if (action.reason === ActionReason.DeficitCharge) {
                return isCurrent ? 'Topping up battery' : 'Topped up battery';
            }
            return isCurrent ? 'Charging battery' : 'Charged battery';
        case 'solarCharge':
            return isCurrent ? 'Charging from solar' : 'Charged from solar';
        case 'solarExport':
            return isCurrent ? 'Exporting solar' : 'Exported solar';
        case 'export':
            if (action.batteryMode === BatteryMode.Load && action.solarMode === SolarMode.Export) {
                return isCurrent ? 'Exporting solar' : 'Exported solar';
            }
            return isCurrent ? 'Exporting to grid' : 'Exported to grid';
        case 'load':
            if (action.reason === ActionReason.DischargeAtPeak) {
                return isCurrent ? 'Powering home at peak' : 'Powered home at peak';
            }
            if (
                action.reason === ActionReason.PreventSolarCurtailment ||
                action.reason === ActionReason.DischargeBeforeCapacity
            ) {
                return isCurrent ? 'Making room for solar' : 'Made room for solar';
            }
            return isCurrent ? 'Running on solar & battery' : 'Ran on solar & battery';
        case 'vpp':
            return isCurrent ? 'VPP event active' : 'VPP event';
        case 'fault':
            if (hasStorms) {
                return isCurrent ? 'Preparing for storm' : 'Prepared for storm';
            }
            if (action.reason === ActionReason.GridUnavailable || action.systemStatus?.gridUnavailable) {
                return 'Grid outage backup';
            }
            if (action.reason === ActionReason.EmergencyMode) {
                return 'Emergency mode active';
            }
            return 'System fault detected';
        case 'standby':
        default:
            return isCurrent ? 'Holding battery in standby' : 'Held battery in standby';
    }
};

export const buildDaySummary = (
    groupedActions: (Action | ActionSummary)[],
    currencySymbol: string = '$'
): DaySummaryData => {
    if (!groupedActions || groupedActions.length === 0) {
        return { segments: [], beats: [], legend: [] };
    }

    // groupedActions is newest-to-oldest; reverse to chronological (oldest-to-newest)
    const chronological = [...groupedActions].reverse();

    interface RawPhase {
        category: SummaryPhaseCategory;
        startTime: string;
        endTime: string;
        effectiveEndTime: string;
        startMinutes: number;
        endMinutes: number;
        durationMinutes: number;
        hasSOC: boolean;
        startSOC: number;
        endSOC: number;
        hasPrice: boolean;
        avgPrice: number;
        priceWeight: number;
        latestAction: Action;
        refTs?: string;
        isLatest: boolean;
    }

    const rawPhases: RawPhase[] = chronological.map((item, idx) => {
        const isSummary = 'isSummary' in item;
        const action = isSummary ? item.latestAction : item;
        const refTs =
            action.systemTimestamp && !isZeroTime(action.systemTimestamp)
                ? action.systemTimestamp
                : action.systemStatus?.timestamp;
        const startTime = isSummary ? item.startTime : getActionTimestamp(action);
        const endTime = isSummary && item.endTime ? item.endTime : startTime;

        let effectiveEndTime = endTime;
        if (idx < chronological.length - 1) {
            const nextItem = chronological[idx + 1];
            const nextStart = 'isSummary' in nextItem ? nextItem.startTime : getActionTimestamp(nextItem);
            const endMs = new Date(endTime).getTime();
            const nextStartMs = new Date(nextStart).getTime();
            if (!isNaN(endMs) && !isNaN(nextStartMs)) {
                const gapMinutes = (nextStartMs - endMs) / 60000;
                if (gapMinutes > 0 && gapMinutes <= 45) {
                    effectiveEndTime = nextStart;
                }
            }
        }

        const startMs = new Date(startTime).getTime();
        const effEndMs = new Date(effectiveEndTime).getTime();
        const durationMinutes =
            !isNaN(startMs) && !isNaN(effEndMs) && effEndMs >= startMs
                ? (effEndMs - startMs) / 60000
                : 0;

        const startMinutes = getMinutesFromMidnight(startTime, refTs);
        const rawEndMinutes = getMinutesFromMidnight(effectiveEndTime, refTs);
        const endMinutes = Math.min(1440, Math.max(rawEndMinutes, startMinutes + 20));

        const hasSOC = isSummary
            ? item.hasSOC
            : action.systemStatus?.batterySOC !== undefined && action.systemStatus.batterySOC !== 0;
        const singleSOC = action.systemStatus?.batterySOC ?? 0;
        const startSOC = isSummary ? (item.startSOC ?? item.minSOC ?? item.avgSOC) : singleSOC;
        const endSOC = isSummary ? (item.endSOC ?? item.maxSOC ?? item.avgSOC) : singleSOC;

        const hasPrice = isSummary
            ? item.hasPrice
            : Boolean(action.currentPrice && !isZeroTime(action.currentPrice.tsStart));
        const avgPrice = isSummary
            ? item.avgPrice
            : action.currentPrice
            ? gridChargeCost(action.currentPrice)
            : 0;
        const priceWeight = isSummary ? item.count : 1;

        return {
            category: classifySummaryItem(item, startSOC, endSOC, hasSOC),
            startTime,
            endTime,
            effectiveEndTime,
            startMinutes,
            endMinutes,
            durationMinutes,
            hasSOC,
            startSOC,
            endSOC,
            hasPrice,
            avgPrice,
            priceWeight,
            latestAction: action,
            refTs,
            isLatest: idx === chronological.length - 1,
        };
    });

    // 1. Build 24h bar segments (merge adjacent same-category segments and snap touching boundaries)
    const mergedBarPhases: { category: SummaryPhaseCategory; startMinutes: number; endMinutes: number }[] = [];
    for (const p of rawPhases) {
        const prev = mergedBarPhases[mergedBarPhases.length - 1];
        if (prev && prev.category === p.category && p.startMinutes <= prev.endMinutes + 45) {
            prev.endMinutes = Math.max(prev.endMinutes, p.endMinutes);
        } else {
            if (prev && p.startMinutes > prev.endMinutes && p.startMinutes - prev.endMinutes <= 45) {
                prev.endMinutes = p.startMinutes;
            }
            const clampedStart = prev ? Math.max(p.startMinutes, prev.endMinutes) : p.startMinutes;
            const clampedEnd = Math.min(1440, Math.max(p.endMinutes, clampedStart + 15));
            mergedBarPhases.push({
                category: p.category,
                startMinutes: clampedStart,
                endMinutes: clampedEnd,
            });
        }
    }

    const segments: DaySummarySegment[] = mergedBarPhases.map(seg => {
        const leftPercent = Math.max(0, Math.min(100, (seg.startMinutes / 1440) * 100));
        const rightPercent = Math.max(leftPercent, Math.min(100, (seg.endMinutes / 1440) * 100));
        const widthPercent = Math.max(1, rightPercent - leftPercent);
        return {
            category: seg.category,
            leftPercent,
            widthPercent,
        };
    });

    const usedCategories = new Set(segments.map(s => s.category));
    const legend = SUMMARY_LEGEND_ORDER.filter(item => usedCategories.has(item.category));

    // 2. Build story beats:
    // Filter out < 30 minute blips (except the newest/current action, faults, or VPP)
    let significantPhases = rawPhases.filter(
        p => p.isLatest || p.category === 'fault' || p.category === 'vpp' || p.durationMinutes >= 30
    );
    if (significantPhases.length === 0) {
        significantPhases = [rawPhases[rawPhases.length - 1]];
    }

    // Merge consecutive phases of the same category
    const mergedPhases: RawPhase[] = [];
    for (const p of significantPhases) {
        const prev = mergedPhases[mergedPhases.length - 1];
        if (prev && prev.category === p.category) {
            prev.endTime = p.endTime;
            prev.effectiveEndTime = p.effectiveEndTime;
            prev.durationMinutes += p.durationMinutes;
            if (p.hasSOC) {
                if (!prev.hasSOC) {
                    prev.startSOC = p.startSOC;
                }
                prev.endSOC = p.endSOC;
                prev.hasSOC = true;
            }
            if (p.hasPrice) {
                const totalWeight = (prev.hasPrice ? prev.priceWeight : 0) + p.priceWeight;
                prev.avgPrice =
                    ((prev.hasPrice ? prev.avgPrice * prev.priceWeight : 0) + p.avgPrice * p.priceWeight) /
                    totalWeight;
                prev.priceWeight = totalWeight;
                prev.hasPrice = true;
            }
            prev.latestAction = p.latestAction;
            prev.refTs = p.refTs;
            prev.isLatest = p.isLatest;
        } else {
            mergedPhases.push({ ...p });
        }
    }

    // Hide standby from bullet points unless the entire day is standby
    const hasNonStandby = mergedPhases.some(p => p.category !== 'standby');
    let beatPhases = hasNonStandby
        ? mergedPhases.filter(p => p.category !== 'standby')
        : mergedPhases;

    if (beatPhases.length === 0) {
        beatPhases = [mergedPhases[mergedPhases.length - 1]];
    }

    // Order newest to oldest
    const newestFirst = [...beatPhases].reverse();

    const beats: DaySummaryBeat[] = newestFirst.map(p => {
        const startFormatted = formatTime(p.startTime, p.refTs);
        const endFormatted = formatTime(p.endTime, p.refTs);
        const timeLabel =
            startFormatted && endFormatted && startFormatted !== endFormatted
                ? `${startFormatted} – ${endFormatted}`
                : startFormatted;

        return {
            category: p.category,
            headline: getSummaryBeatHeadline(p.category, p.latestAction, p.isLatest),
            timeLabel,
            socText: p.hasSOC ? formatSOCRange(p.startSOC, p.endSOC) : undefined,
            priceText: p.hasPrice ? formatPrice(p.avgPrice, currencySymbol) : undefined,
        };
    });

    return { segments, beats, legend };
};

export function getPlanStatusSubvalue(action: Action, refTs?: string, symbol: string = '$'): string | null {
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
                return `Holding reserve for ${formatTime(nextExport.tsStart, refTs)} export`;
            }
            return 'Holding reserve for upcoming export window';
        }

        // Look for the next mode change in the plan
        for (let i = 1; i < periods.length; i++) {
            const p = periods[i];
            if (p.batteryMode === BatteryMode.ChargeAny) {
                const price = p.importDollars !== undefined ? p.importDollars : null;
                const priceStr = price !== null ? ` (${symbol}${price.toFixed(3)}/kWh)` : '';
                return `Waiting to charge at ${formatTime(p.tsStart, refTs)}${priceStr}`;
            }
            if (p.batteryMode === BatteryMode.Load && p.reason === ActionReason.DischargeAtPeak) {
                return `Saving reserve for ${formatTime(p.tsStart, refTs)} peak rate`;
            }
            if (p.batteryMode === BatteryMode.Export) {
                return `Holding reserve for ${formatTime(p.tsStart, refTs)} export`;
            }
        }

        if (action.reason === ActionReason.DeficitSaveForPeak || action.reason === ActionReason.ArbitrageHoldSave) {
            return 'Saving reserve for upcoming peak rate';
        }
        return 'Holding reserve in standby';
    }

    // Charging: find end of contiguous charge block
    if (state === 'charging') {
        let endChargeTime = currentPeriod.tsEnd;
        for (let i = 1; i < periods.length; i++) {
            if (periods[i].batteryMode === BatteryMode.ChargeAny) {
                endChargeTime = periods[i].tsEnd;
            } else {
                break;
            }
        }
        const targetSoc = action.chargeToSoc || (currentPeriod.endSoc ? Math.round(currentPeriod.endSoc) : 100);
        const price = action.currentPrice ? (action.currentPrice.dollarsPerKWH + (action.currentPrice.gridUseDollarsPerKWH || 0)) : null;
        if (price !== null && price < 0.06) {
            return `Charging to ${targetSoc}% until ${formatTime(endChargeTime, refTs)} • Low rate (${symbol}${price.toFixed(3)}/kWh)`;
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
            let endExportTime = currentPeriod.tsEnd;
            for (let i = 1; i < periods.length; i++) {
                if (periods[i].batteryMode === BatteryMode.Export) {
                    endExportTime = periods[i].tsEnd;
                } else {
                    break;
                }
            }
            return `Exporting to grid until ${formatTime(endExportTime, refTs)}`;
        }

        // Peak discharge
        let endDischargeTime = currentPeriod.tsEnd;
        for (let i = 1; i < periods.length; i++) {
            if (periods[i].batteryMode === BatteryMode.Load) {
                endDischargeTime = periods[i].tsEnd;
            } else {
                break;
            }
        }
        const isPeak = action.reason === ActionReason.DischargeAtPeak || (action.description && action.description.toLowerCase().includes('peak'));
        if (isPeak) {
            const price = action.currentPrice ? (action.currentPrice.dollarsPerKWH + (action.currentPrice.gridUseDollarsPerKWH || 0)) : null;
            const priceStr = price !== null ? ` (${symbol}${price.toFixed(3)}/kWh)` : '';
            return `Peak rate defense${priceStr} until ${formatTime(endDischargeTime, refTs)}`;
        }
        return `Powering home on solar & battery`;
    }

    return null;
}
