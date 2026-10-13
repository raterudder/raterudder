import React, { useState, useEffect } from 'react';
import { Collapsible } from '@base-ui/react/collapsible';
import { type SavingsStats } from '../api';
import { formatCurrency } from '../utils/dashboardUtils';

interface SavingsHeroProps {
    savings: SavingsStats | null;
    savingsPeriods?: SavingsStats[];
    planMode?: boolean;
    currentDate?: Date;
    isToday?: boolean;
    currencySymbol?: string;
}

const SavingsHero: React.FC<SavingsHeroProps> = ({
    savings,
    savingsPeriods,
    planMode = false,
    currentDate,
    isToday = true,
    currencySymbol = '$',
}) => {
    const [isMobile, setIsMobile] = useState(window.innerWidth < 768);
    const [open, setOpen] = useState(false);
    const currentDateTs = currentDate?.getTime();
    const [selection, setSelection] = useState<{ dateTs: number | undefined; periodIdx: number }>({
        dateTs: currentDateTs,
        periodIdx: 0,
    });
    const selectedPeriodIdx = selection.dateTs === currentDateTs ? selection.periodIdx : 0;
    const setSelectedPeriodIdx = (idx: number) => setSelection({ dateTs: currentDateTs, periodIdx: idx });

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    if (!savings) return null;

    const useNewHero = Boolean(planMode && savingsPeriods && savingsPeriods.length > 0);
    const activeIdx = useNewHero && savingsPeriods && selectedPeriodIdx < savingsPeriods.length ? selectedPeriodIdx : 0;
    const activeStats = (useNewHero && savingsPeriods ? savingsPeriods[activeIdx] : savings) || savings;

    const getPeriodMeta = (idx: number) => {
        if (idx === 0) {
            if (isToday) {
                return { header: 'Today Savings', row: 'Today', dotClass: 'battery' };
            }
            const dateLabel = currentDate
                ? currentDate.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
                : 'Day';
            return { header: `${dateLabel} Savings`, row: dateLabel, dotClass: 'battery' };
        }
        if (idx === 1) {
            return { header: 'Last 7 Days Savings', row: 'Last 7 Days', dotClass: 'credit' };
        }
        return { header: 'Last 30 Days Savings', row: 'Last 30 Days', dotClass: 'period-30d' };
    };

    const netSavings = savings.batterySavings + savings.solarSavings + savings.credit;
    const activeMeta = getPeriodMeta(activeIdx);
    const activeTotalSavings = activeStats.totalSavingsDollars ?? 0;
    const activeWithoutRRSavings = activeStats.solarAndBatterySavingsDollars ?? 0;
    const activeShowRR = (activeStats.rateRudderSavingsDollars ?? 0) >= 0.05;

    const subcaptionParts: string[] = [];
    if (activeStats.estimated) subcaptionParts.push('Estimated.');
    if (activeShowRR) subcaptionParts.push('(Without RateRudder).');
    const subcaption = subcaptionParts.join(' ');

    return (
        <div className="savings-hero">
            <Collapsible.Root open={isMobile ? open : true} onOpenChange={setOpen}>
                <div className="overview-hero">
                    <div className="net-savings-panel">
                        {useNewHero ? (
                            <>
                                <span className="hero-label">{activeMeta.header}</span>
                                <div className="hero-value-group hero-value-inline">
                                    <span
                                        className={`hero-value ${activeTotalSavings >= 0 ? 'positive' : 'negative'}`}
                                        data-testid="hero-primary-savings"
                                    >
                                        {formatCurrency(activeTotalSavings, false, currencySymbol)}
                                    </span>
                                    {activeShowRR && (
                                        <span className="hero-value-secondary" data-testid="hero-without-rr-savings">
                                            ({formatCurrency(activeWithoutRRSavings, false, currencySymbol)})
                                        </span>
                                    )}
                                </div>
                                {subcaption && (
                                    <span className="hero-subcaption" data-testid="hero-subcaption">
                                        {subcaption}
                                    </span>
                                )}
                                <div className="hero-breakdown">
                                    {savingsPeriods!.map((periodStats, idx) => {
                                        if (idx === activeIdx) return null;
                                        const meta = getPeriodMeta(idx);
                                        const periodTotal = periodStats.totalSavingsDollars ?? 0;
                                        const periodWithoutRR = periodStats.solarAndBatterySavingsDollars ?? 0;
                                        const periodShowRR = (periodStats.rateRudderSavingsDollars ?? 0) >= 0.05;
                                        return (
                                            <button
                                                key={idx}
                                                type="button"
                                                className="breakdown-item breakdown-period-btn"
                                                onClick={() => setSelectedPeriodIdx(idx)}
                                            >
                                                <span className={`dot ${meta.dotClass}`} aria-hidden="true"></span>
                                                <span className="label">{meta.row}</span>
                                                <span className="breakdown-values">
                                                    <span className={`value ${periodTotal >= 0 ? 'positive' : 'negative'}`}>
                                                        {formatCurrency(periodTotal, true, currencySymbol)}
                                                    </span>
                                                    {periodShowRR && (
                                                        <span className="value-secondary">
                                                            ({formatCurrency(periodWithoutRR, false, currencySymbol)})
                                                        </span>
                                                    )}
                                                    <span className="period-chevron" aria-hidden="true">›</span>
                                                </span>
                                            </button>
                                        );
                                    })}
                                </div>
                            </>
                        ) : (
                            <>
                                <span className="hero-label">Savings</span>
                                <div className="hero-value-group">
                                    <span className={`hero-value ${netSavings >= 0 ? 'positive' : 'negative'}`}>
                                        {formatCurrency(netSavings, false, currencySymbol)}
                                    </span>
                                </div>
                                <div className="hero-breakdown">
                                    <div className="breakdown-item">
                                        <span className="dot solar" aria-hidden="true"></span>
                                        <span className="label">Solar</span>
                                        <span className={`value ${savings.solarSavings >= 0 ? 'positive' : 'negative'}`}>
                                            {formatCurrency(savings.solarSavings, true, currencySymbol)}
                                        </span>
                                    </div>
                                    <div className="breakdown-item">
                                        <span className="dot battery" aria-hidden="true"></span>
                                        <span className="label">Battery</span>
                                        <span className={`value ${savings.batterySavings >= 0 ? 'positive' : 'negative'}`}>
                                            {formatCurrency(savings.batterySavings, true, currencySymbol)}
                                        </span>
                                    </div>
                                    {Math.abs(savings.credit) > 0.01 && (
                                        <div className="breakdown-item">
                                            <span className="dot credit" aria-hidden="true"></span>
                                            <span className="label">Export</span>
                                            <span className={`value ${savings.credit >= 0 ? 'positive' : 'negative'}`}>
                                                {formatCurrency(savings.credit, true, currencySymbol)}
                                            </span>
                                        </div>
                                    )}
                                </div>
                            </>
                        )}
                    </div>

                    {isMobile && (
                        <Collapsible.Trigger className="stats-toggle-btn">
                            {open ? 'Hide Details' : 'Show Details'}
                            <span className={`arrow ${open ? 'up' : 'down'}`} />
                        </Collapsible.Trigger>
                    )}

                    <Collapsible.Panel className="usage-stats-panel-wrapper">
                        <div className="usage-stats-panel">
                            <div className="stats-row">
                                <div className="stat-card">
                                    <span className="stat-label">Home Usage</span>
                                    <span className="stat-value">{activeStats.homeUsed.toFixed(1)} <small>kWh</small></span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Solar Gen</span>
                                    <span className="stat-value">{activeStats.solarGenerated.toFixed(1)} <small>kWh</small></span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Battery Use</span>
                                    <span className="stat-value">{activeStats.batteryUsed.toFixed(1)} <small>kWh</small></span>
                                </div>
                            </div>
                            <div className="stats-row grid-metrics">
                                <div className="stat-card">
                                    <span className="stat-label">Grid (In/Out)</span>
                                    <span className="stat-value traffic-value">
                                        <span className="traffic-in">{activeStats.gridImported.toFixed(1)}</span>
                                        <span className="traffic-sep">/</span>
                                        <span className="traffic-out">{activeStats.gridExported.toFixed(1)}</span>
                                        <small>kWh</small>
                                    </span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Total Credit</span>
                                    <span className={`stat-value ${activeStats.credit > 0 ? 'positive' : activeStats.credit < 0 ? 'negative' : ''}`}>
                                        {formatCurrency(activeStats.credit, false, currencySymbol)}
                                    </span>
                                </div>
                                <div className="stat-card">
                                    <span className="stat-label">Total Cost</span>
                                    <span className="stat-value">{formatCurrency(activeStats.cost, false, currencySymbol)}</span>
                                </div>
                            </div>
                        </div>
                    </Collapsible.Panel>
                </div>
            </Collapsible.Root>
        </div>
    );
};

export default SavingsHero;
