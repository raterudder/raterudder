import { BatteryMode, ActionReason, SolarMode } from '../api';
import type { PlanPeriod, PriceHistoryRes, EnergyHistoryRes } from '../api';
import {
    formatTime,
    formatSOCRange,
    formatPrice,
    getSummaryBeatHeadline,
    type SummaryPhaseCategory,
    type DaySummaryBeat,
} from './dashboardUtils';

export interface ZoneConfig {
    key: string;
    label: string;
    color: string;
}

export const ALL_ZONES: ZoneConfig[] = [
    { key: 'solarCharge', label: 'Solar Charge', color: '#10b981' },
    { key: 'poweringHome', label: 'Powering Home', color: '#38bdf8' },
    { key: 'solarExport', label: 'Solar Export', color: '#f97316' },
    { key: 'standby', label: 'Standby', color: '#64748b' },
    { key: 'gridCharge', label: 'Grid Charge', color: '#8b5cf6' },
    { key: 'gridExport', label: 'Grid Export', color: '#f59e0b' },
];

export function classifyPlanPeriod(p: PlanPeriod): ZoneConfig {
    // 1. Forced grid charge
    if (p.batteryMode === BatteryMode.ChargeAny) {
        return ALL_ZONES.find((z) => z.key === 'gridCharge')!;
    }
    // 2. Forced grid export
    if (p.batteryMode === BatteryMode.Export) {
        return ALL_ZONES.find((z) => z.key === 'gridExport')!;
    }
    // 3. Solar charging: SOC is rising
    if (p.endSoc > p.startSoc + 0.1) {
        return ALL_ZONES.find((z) => z.key === 'solarCharge')!;
    }
    // 4. Solar export: solar exporting while battery covers load (direct export)
    const isSolarExport =
        (p.solarMode === SolarMode.Export || p.reason === ActionReason.DirectExport) &&
        (p.batteryMode === BatteryMode.Load || p.endSoc < p.startSoc - 0.1);
    if (isSolarExport) {
        return ALL_ZONES.find((z) => z.key === 'solarExport')!;
    }
    // 5. Discharging to power home: SOC is falling
    if (p.endSoc < p.startSoc - 0.1) {
        return ALL_ZONES.find((z) => z.key === 'poweringHome')!;
    }
    // 6. Standby (includes holding at reserve or flat SOC)
    return ALL_ZONES.find((z) => z.key === 'standby')!;
}

export function truncateIsoHour(isoStr: string): string {
    if (!isoStr) return isoStr;
    const hourPattern = /:\d{2}:\d{2}(\.\d+)?([+-]\d{2}:?\d{2}|Z)$/;
    if (hourPattern.test(isoStr)) {
        return isoStr.replace(hourPattern, ':00:00$2');
    }
    const d = new Date(isoStr);
    if (isNaN(d.getTime())) return isoStr;
    const hourMs = Math.floor(d.getTime() / 3600000) * 3600000;
    return new Date(hourMs).toISOString();
}

export function reconstructFuturePricesFromPlan(periods: PlanPeriod[]): PriceHistoryRes[] {
    if (!periods || periods.length === 0) return [];
    const priceEpsilon = 1e-3;
    const res: PriceHistoryRes[] = [];

    const appendEntry = (tsIso: string, importDollars: number, exportDollars: number) => {
        if (res.length > 0) {
            const last = res[res.length - 1];
            const lastHourMs = Math.floor(new Date(last.tsHourStart).getTime() / 3600000) * 3600000;
            const curHourMs = Math.floor(new Date(tsIso).getTime() / 3600000) * 3600000;
            if (
                lastHourMs === curHourMs &&
                Math.abs(last.dollarsPerKWH - importDollars) < priceEpsilon &&
                Math.abs((last.exportDollarsPerKWH ?? 0) - exportDollars) < priceEpsilon
            ) {
                return;
            }
        }
        res.push({
            tsHourStart: tsIso,
            dollarsPerKWH: importDollars,
            gridUseDollarsPerKWH: 0,
            exportDollarsPerKWH: exportDollars,
        });
    };

    for (let i = 0; i < periods.length; i++) {
        const p = periods[i];
        let startIso = p.tsStart;
        const startDate = new Date(p.tsStart);
        if (i === 0 && !isNaN(startDate.getTime())) {
            const min = startDate.getUTCMinutes();
            if (min % 15 !== 0 || min % 20 === 0) {
                startIso = truncateIsoHour(p.tsStart);
            }
        }
        const importDollars = p.importDollars ?? 0;
        const exportDollars = p.exportDollars ?? 0;
        appendEntry(startIso, importDollars, exportDollars);

        const hourMs = Math.floor(startDate.getTime() / 3600000) * 3600000;
        const nextHourMs = hourMs + 3600000;
        const endMs = new Date(p.tsEnd).getTime();
        if (!isNaN(endMs) && endMs > nextHourMs) {
            appendEntry(new Date(nextHourMs).toISOString(), importDollars, exportDollars);
        }
    }
    return res;
}

export function buildPlanSolarLoadSeries(
    periods: PlanPeriod[],
    energyHistory: EnergyHistoryRes[] = [],
    includeHistory: boolean = false
): any[] {
    if (!periods || periods.length === 0) return [];

    interface HourBucket {
        hourMs: number;
        ts: string;
        durationHours: number;
        solarKWH: number;
        loadKWH: number;
    }

    const buckets: HourBucket[] = [];
    for (const p of periods) {
        const startMs = new Date(p.tsStart).getTime();
        const hourMs = Math.floor(startMs / 3600000) * 3600000;
        let bucket = buckets[buckets.length - 1];
        if (!bucket || bucket.hourMs !== hourMs) {
            bucket = {
                hourMs,
                ts: truncateIsoHour(p.tsStart),
                durationHours: 0,
                solarKWH: 0,
                loadKWH: 0,
            };
            buckets.push(bucket);
        }
        const dur = p.durationHours > 0 ? p.durationHours : (new Date(p.tsEnd).getTime() - startMs) / 3600000;
        bucket.durationHours += dur;
        bucket.solarKWH += p.solarKWH || 0;
        bucket.loadKWH += p.loadKWH || 0;
    }

    const firstHourMs = buckets[0]?.hourMs ?? Infinity;
    const series: any[] = [];

    if (includeHistory && energyHistory && energyHistory.length > 0) {
        for (const h of energyHistory) {
            const hMs = new Date(h.tsHourStart).getTime();
            if (hMs < firstHourMs) {
                series.push({
                    ts: h.tsHourStart,
                    hour: new Date(h.tsHourStart).getHours(),
                    predictedSolarKWH: h.solarKWH || 0,
                    avgHomeLoadKWH: Math.floor((h.homeLoadKWH || 0) * 10) / 10,
                    isHistory: true,
                });
            }
        }
    }

    for (const b of buckets) {
        const solarRate = b.durationHours > 0 ? b.solarKWH / b.durationHours : b.solarKWH;
        const loadRate = b.durationHours > 0 ? b.loadKWH / b.durationHours : b.loadKWH;
        series.push({
            ts: b.ts,
            hour: new Date(b.ts).getHours(),
            predictedSolarKWH: solarRate,
            avgHomeLoadKWH: Math.floor(loadRate * 10) / 10,
            isHistory: false,
        });
    }

    return series;
}

export function buildPlanPriceSeries(
    periods: PlanPeriod[],
    futurePrices?: PriceHistoryRes[],
    priceHistory: PriceHistoryRes[] = [],
    includeHistory: boolean = false
): any[] {
    if (!periods || periods.length === 0) return [];

    const resolvedFuture =
        futurePrices && futurePrices.length > 0
            ? futurePrices
            : reconstructFuturePricesFromPlan(periods);

    const firstFutureMs =
        resolvedFuture.length > 0
            ? new Date(resolvedFuture[0].tsHourStart).getTime()
            : new Date(periods[0].tsStart).getTime();

    const series: any[] = [];

    if (includeHistory && priceHistory && priceHistory.length > 0) {
        for (const p of priceHistory) {
            const pMs = new Date(p.tsHourStart).getTime();
            if (pMs < firstFutureMs) {
                series.push({
                    ts: p.tsHourStart,
                    hour: new Date(p.tsHourStart).getHours(),
                    gridChargeDollarsPerKWH: p.dollarsPerKWH + (p.gridUseDollarsPerKWH || 0),
                    gridExportDollarsPerKWH: p.exportDollarsPerKWH ?? p.dollarsPerKWH ?? 0,
                    isHistory: true,
                });
            }
        }
    }

    for (const p of resolvedFuture) {
        series.push({
            ts: p.tsHourStart,
            hour: new Date(p.tsHourStart).getHours(),
            gridChargeDollarsPerKWH: p.dollarsPerKWH + (p.gridUseDollarsPerKWH || 0),
            gridExportDollarsPerKWH: p.exportDollarsPerKWH ?? 0,
            isHistory: false,
        });
    }

    const lastPeriod = periods[periods.length - 1];
    if (lastPeriod && series.length > 0) {
        const lastPt = series[series.length - 1];
        if (new Date(lastPeriod.tsEnd).getTime() > new Date(lastPt.ts).getTime()) {
            series.push({
                ts: lastPeriod.tsEnd,
                hour: new Date(lastPeriod.tsEnd).getHours(),
                gridChargeDollarsPerKWH: lastPt.gridChargeDollarsPerKWH,
                gridExportDollarsPerKWH: lastPt.gridExportDollarsPerKWH,
                isHistory: false,
            });
        }
    }

    return series;
}

export interface PlanSummaryStats {
    totalSolarKWH: number;
    totalLoadKWH: number;
    minImportDollars: number;
    minImportTs: string;
    maxImportDollars: number;
    maxImportTs: string;
    maxExportDollars: number;
    maxExportTs: string;
}

export function computePlanSummaryStats(
    periods: PlanPeriod[],
    futurePrices?: PriceHistoryRes[]
): PlanSummaryStats {
    let totalSolarKWH = 0;
    let totalLoadKWH = 0;

    for (const p of periods || []) {
        totalSolarKWH += p.solarKWH || 0;
        totalLoadKWH += p.loadKWH || 0;
    }

    const resolvedPrices =
        futurePrices && futurePrices.length > 0
            ? futurePrices
            : reconstructFuturePricesFromPlan(periods || []);

    let minImportDollars = Infinity;
    let minImportTs = '';
    let maxImportDollars = -Infinity;
    let maxImportTs = '';
    let maxExportDollars = -Infinity;
    let maxExportTs = '';

    for (const p of resolvedPrices) {
        const imp = p.dollarsPerKWH + (p.gridUseDollarsPerKWH || 0);
        const exp = p.exportDollarsPerKWH ?? 0;
        if (imp < minImportDollars) {
            minImportDollars = imp;
            minImportTs = p.tsHourStart;
        }
        if (imp > maxImportDollars) {
            maxImportDollars = imp;
            maxImportTs = p.tsHourStart;
        }
        if (exp > maxExportDollars) {
            maxExportDollars = exp;
            maxExportTs = p.tsHourStart;
        }
    }

    if (minImportDollars === Infinity) minImportDollars = 0;
    if (maxImportDollars === -Infinity) maxImportDollars = 0;
    if (maxExportDollars === -Infinity) maxExportDollars = 0;

    return {
        totalSolarKWH,
        totalLoadKWH,
        minImportDollars,
        minImportTs,
        maxImportDollars,
        maxImportTs,
        maxExportDollars,
        maxExportTs,
    };
}

function zoneKeyToSummaryCategory(zoneKey: string): SummaryPhaseCategory {
    switch (zoneKey) {
        case 'gridCharge':
            return 'charge';
        case 'gridExport':
            return 'export';
        case 'solarCharge':
            return 'solarCharge';
        case 'solarExport':
            return 'solarExport';
        case 'poweringHome':
            return 'load';
        case 'standby':
        default:
            return 'standby';
    }
}

export function buildPlanSummaryBeats(
    periods: PlanPeriod[],
    currencySymbol: string = '$'
): DaySummaryBeat[] {
    if (!periods || periods.length === 0) {
        return [];
    }

    const refTs = periods[0].tsStart;

    interface RawPlanPhase {
        category: SummaryPhaseCategory;
        tsStart: string;
        tsEnd: string;
        durationHours: number;
        durationMinutes: number;
        count: number;
        startSoc: number;
        endSoc: number;
        importCostWeighted: number;
        exportCreditWeighted: number;
        representativeReason: ActionReason;
        batteryMode: BatteryMode;
        solarMode: SolarMode;
    }

    const mergeInto = (target: RawPlanPhase, next: RawPlanPhase) => {
        target.tsEnd = next.tsEnd;
        target.durationHours += next.durationHours;
        target.durationMinutes += next.durationMinutes;
        target.count += next.count;
        target.endSoc = next.endSoc;
        target.importCostWeighted += next.importCostWeighted;
        target.exportCreditWeighted += next.exportCreditWeighted;
        if (next.representativeReason === ActionReason.DischargeAtPeak) {
            target.representativeReason = ActionReason.DischargeAtPeak;
        }
    };

    const mergeAdjacentSameCategory = (phases: RawPlanPhase[]): RawPlanPhase[] => {
        const result: RawPlanPhase[] = [];
        for (const p of phases) {
            const prev = result[result.length - 1];
            if (prev && prev.category === p.category) {
                mergeInto(prev, p);
            } else {
                result.push({ ...p });
            }
        }
        return result;
    };

    const rawPhases: RawPlanPhase[] = periods.map((p) => {
        const z = classifyPlanPeriod(p);
        const category = zoneKeyToSummaryCategory(z.key);
        const durHours =
            p.durationHours > 0
                ? p.durationHours
                : Math.max(0, (new Date(p.tsEnd).getTime() - new Date(p.tsStart).getTime()) / 3600000);
        return {
            category,
            tsStart: p.tsStart,
            tsEnd: p.tsEnd,
            durationHours: durHours,
            durationMinutes: durHours * 60,
            count: 1,
            startSoc: p.startSoc,
            endSoc: p.endSoc,
            importCostWeighted: (p.importDollars ?? 0) * durHours,
            exportCreditWeighted: (p.exportDollars ?? 0) * durHours,
            representativeReason: p.reason,
            batteryMode: p.batteryMode,
            solarMode: p.solarMode,
        };
    });

    // 1. Coalesce adjacent same-category phases
    const coalescedPhases = mergeAdjacentSameCategory(rawPhases);

    // 2. Combine similar non-standby phases separated by short standby (<= 60 min)
    const afterStandbyBridge: RawPlanPhase[] = [];
    for (const p of coalescedPhases) {
        const prev = afterStandbyBridge[afterStandbyBridge.length - 1];
        const prevPrev = afterStandbyBridge[afterStandbyBridge.length - 2];
        if (prev && prev.category === p.category) {
            mergeInto(prev, p);
        } else if (
            p.category !== 'standby' &&
            prev &&
            prev.category === 'standby' &&
            prev.durationMinutes <= 60 &&
            prevPrev &&
            prevPrev.category === p.category
        ) {
            afterStandbyBridge.pop();
            mergeInto(prevPrev, p);
        } else {
            afterStandbyBridge.push({ ...p });
        }
    }

    // 3. Combine similar non-standby phases separated by a single brief (< 30 min) period
    const afterSinglePeriodBridge: RawPlanPhase[] = [];
    for (const p of afterStandbyBridge) {
        const prev = afterSinglePeriodBridge[afterSinglePeriodBridge.length - 1];
        const prevPrev = afterSinglePeriodBridge[afterSinglePeriodBridge.length - 2];
        const isSinglePeriodSeparator = prev && (prev.count === 1 || prev.durationMinutes < 30);
        if (prev && prev.category === p.category) {
            mergeInto(prev, p);
        } else if (
            p.category !== 'standby' &&
            isSinglePeriodSeparator &&
            prevPrev &&
            prevPrev.category === p.category
        ) {
            afterSinglePeriodBridge.pop();
            mergeInto(prevPrev, p);
        } else {
            afterSinglePeriodBridge.push({ ...p });
        }
    }

    // 4. Hide standby unless the entire horizon is standby
    const hasNonStandby = afterSinglePeriodBridge.some((p) => p.category !== 'standby');
    const nonStandbyFiltered = hasNonStandby
        ? afterSinglePeriodBridge.filter((p) => p.category !== 'standby')
        : afterSinglePeriodBridge;

    // 5. Filter out < 30 minute minor blips unless none remain
    let significantPhases = nonStandbyFiltered.filter((p) => p.durationMinutes >= 29.5);
    if (significantPhases.length === 0) {
        significantPhases = nonStandbyFiltered.length > 0 ? [nonStandbyFiltered[0]] : [rawPhases[0]];
    }

    return significantPhases.map((p) => {
        const startFormatted = formatTime(p.tsStart, refTs);
        const endFormatted = formatTime(p.tsEnd, refTs);
        const timeLabel =
            startFormatted && endFormatted && startFormatted !== endFormatted
                ? `${startFormatted} – ${endFormatted}`
                : startFormatted;

        const isExport = p.category === 'export' || p.category === 'solarExport';
        const avgPrice =
            p.durationHours > 0
                ? (isExport ? p.exportCreditWeighted : p.importCostWeighted) / p.durationHours
                : 0;

        return {
            category: p.category,
            headline: getSummaryBeatHeadline(
                p.category,
                {
                    reason: p.representativeReason,
                    batteryMode: p.batteryMode,
                    solarMode: p.solarMode,
                },
                true
            ),
            timeLabel,
            socText: formatSOCRange(p.startSoc, p.endSoc),
            priceText: formatPrice(avgPrice, currencySymbol),
        };
    });
}

