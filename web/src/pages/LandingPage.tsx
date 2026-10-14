import React, { useState, useEffect, useRef } from 'react';
import { Link } from 'wouter';
import { Accordion } from '@base-ui/react/accordion';
import {
    ResponsiveContainer,
    AreaChart,
    XAxis,
    YAxis,
    CartesianGrid,
    ReferenceLine,
    ReferenceArea,
    Line
} from 'recharts';
import { ALL_ZONES } from '../utils/forecastUtils';
import { usePageSEO, PUBLIC_PAGES_SEO } from '../utils/seo';
import './LandingPage.css';

// Simulation of a 24-hour cycle (Hourly intervals)
const chartData = [
    { time: '12 AM', hour: 0, soc: 35, manualSoc: 35, price: 0.04, zone: 'standby' },
    { time: '1 AM', hour: 1, soc: 35, manualSoc: 30, price: 0.03, zone: 'standby' },
    { time: '2 AM', hour: 2, soc: 35, manualSoc: 25, price: 0.01, zone: 'gridCharge' },
    { time: '3 AM', hour: 3, soc: 45, manualSoc: 20, price: 0.01, zone: 'gridCharge' },
    { time: '4 AM', hour: 4, soc: 55, manualSoc: 20, price: 0.02, zone: 'gridCharge' },
    { time: '5 AM', hour: 5, soc: 65, manualSoc: 20, price: 0.05, zone: 'poweringHome' },
    { time: '6 AM', hour: 6, soc: 55, manualSoc: 20, price: 0.12, zone: 'poweringHome' },
    { time: '7 AM', hour: 7, soc: 45, manualSoc: 20, price: 0.15, zone: 'poweringHome' },
    { time: '8 AM', hour: 8, soc: 30, manualSoc: 25, price: 0.14, zone: 'solarCharge' },
    { time: '9 AM', hour: 9, soc: 45, manualSoc: 40, price: 0.09, zone: 'solarCharge' },
    { time: '10 AM', hour: 10, soc: 65, manualSoc: 60, price: 0.07, zone: 'solarCharge' },
    { time: '11 AM', hour: 11, soc: 85, manualSoc: 80, price: 0.08, zone: 'solarCharge' },
    { time: '12 PM', hour: 12, soc: 95, manualSoc: 95, price: 0.10, zone: 'gridExport' },
    { time: '1 PM', hour: 13, soc: 86, manualSoc: 100, price: 0.11, zone: 'gridExport' },
    { time: '2 PM', hour: 14, soc: 77, manualSoc: 100, price: 0.15, zone: 'gridExport' },
    { time: '3 PM', hour: 15, soc: 68, manualSoc: 100, price: 0.12, zone: 'gridExport' },
    { time: '4 PM', hour: 16, soc: 60, manualSoc: 100, price: 0.18, zone: 'poweringHome' },
    { time: '5 PM', hour: 17, soc: 50, manualSoc: 90, price: 0.38, zone: 'poweringHome' },
    { time: '6 PM', hour: 18, soc: 38, manualSoc: 70, price: 0.45, zone: 'poweringHome' },
    { time: '7 PM', hour: 19, soc: 28, manualSoc: 50, price: 0.42, zone: 'poweringHome' },
    { time: '8 PM', hour: 20, soc: 22, manualSoc: 30, price: 0.35, zone: 'poweringHome' },
    { time: '9 PM', hour: 21, soc: 20, manualSoc: 20, price: 0.18, zone: 'standby' },
    { time: '10 PM', hour: 22, soc: 20, manualSoc: 20, price: 0.09, zone: 'standby' },
    { time: '11 PM', hour: 23, soc: 20, manualSoc: 20, price: 0.06, zone: 'standby' },
];

const segmentedChartData = chartData.map((d, idx) => {
    const prevZone = idx > 0 ? chartData[idx - 1].zone : null;
    const row: Record<string, string | number | null> = { ...d };
    for (const z of ALL_ZONES) {
        row[`soc_${z.key}`] = (d.zone === z.key || prevZone === z.key) ? d.soc : null;
    }
    return row;
});

const chartZoneColor = (key: string, fallback: string) =>
    ALL_ZONES.find((z) => z.key === key)?.color || fallback;

const phases = [
    {
        id: 0,
        title: "Overnight Grid Charge",
        time: "2:00 AM - 5:00 AM",
        shortTime: "2 AM - 5 AM",
        rateRudderWay: "Detects your EV charging at 2 AM so it won't drain your home battery. It then pre-charges from the $0.01/kWh grid only to 65% to cover the 6-8 AM morning peak while leaving room for free solar.",
        manualWay: "Self-Consumption dumps your battery into your EV at 2 AM, leaving it at 20% for the morning peak. Static timers do the opposite, blindly charging to 100% and wasting free morning solar.",
        price: "$0.02/kWh (Super Off-Peak)",
        priceColor: "#059669",
        socChange: "35% → 65% (Morning Need)",
        manualSocChange: "Drained to 20% by EV",
        color: chartZoneColor('gridCharge', '#8b5cf6'),
        x1: "2 AM",
        x2: "5 AM",
        icon: "🔌"
    },
    {
        id: 1,
        title: "Morning Solar Catch-Up",
        time: "8:00 AM - 12:00 PM",
        shortTime: "8 AM - 12 PM",
        rateRudderWay: "Covers the 6-8 AM breakfast peak from the battery, then captures 100% of morning rooftop solar to reach 95% before noon without clipping or paying peak morning rates.",
        manualWay: "Because Self-Consumption drained the battery overnight, you're forced to buy expensive $0.15/kWh grid power from 6-8 AM while waiting for the sun to come up.",
        price: "$0.14/kWh (Morning Peak)",
        priceColor: "#d97706",
        socChange: "30% → 95% (Free Solar)",
        manualSocChange: "Forced $0.15/kWh Grid Buy",
        color: chartZoneColor('solarCharge', '#10b981'),
        x1: "8 AM",
        x2: "12 PM",
        icon: "☀️"
    },
    {
        id: 2,
        title: "Afternoon Peak Export",
        time: "12:00 PM - 4:00 PM",
        shortTime: "12 PM - 4 PM",
        rateRudderWay: "Exports stored battery energy and live solar to the grid while midday buyback credits surge, stepping down gradually to 60% to capture peak export credits while saving enough charge for the evening.",
        manualWay: "Sits idle at 100% all afternoon, missing lucrative midday export spikes and letting excess solar go unmonetized.",
        price: "$0.15/kWh (Export Credit Spike)",
        priceColor: "#d97706",
        socChange: "95% → 60% (Peak Export)",
        manualSocChange: "100% (Missed Export Credit)",
        color: chartZoneColor('gridExport', '#f59e0b'),
        x1: "12 PM",
        x2: "4 PM",
        icon: "💰"
    },
    {
        id: 3,
        title: "Evening Grid Offset",
        time: "4:00 PM - 9:00 PM",
        shortTime: "4 PM - 9 PM",
        rateRudderWay: "Enters the evening peak at 60% and powers your home through the $0.45/kWh surge. Configure a lower 5% reserve strictly for the 5-9 PM peak to unlock extra capacity, then automatically restore a 20% storm floor overnight.",
        manualWay: "Static 20% backup reserve locks away 15% of your usable battery capacity during the most expensive hours of the day, which forces you onto $0.45/kWh grid power early.",
        price: "$0.45/kWh (Peak Surge)",
        priceColor: "#dc2626",
        socChange: "60% → 20% (Zero Grid Buy)",
        manualSocChange: "Caps Out Early at 20% Floor",
        color: chartZoneColor('poweringHome', '#38bdf8'),
        x1: "4 PM",
        x2: "9 PM",
        icon: "🌙"
    }
];

const SOLAR_FORECAST_ACCURACY = "95%";
const LOAD_FORECAST_ACCURACY = "90%";

interface SpotlightSlide {
    id: string;
    badge: string;
    tabValue: string;
    tabLabel: string;
    title: string;
    description: string;
    metrics: { label: string; value: string; highlight?: boolean }[];
    rateRudderOnly?: boolean;
    nonRateRudderOnly?: boolean;
}

const spotlightSlides: SpotlightSlide[] = [
    {
        id: "savings-boost",
        badge: "Real-Time Price Spike Result",
        tabValue: "Over 60%",
        tabLabel: "Extra Savings vs. Self-Consumption",
        title: "Squeeze 60%+ More Out of the Battery You Already Own",
        description: "Last month, a Powerwall home on hourly pricing would have saved $90 in default Self-Consumption mode. RateRudder added an additional $57 in savings, including $13.67 during a single afternoon price spike.",
        metrics: [
            { label: "Self-Consumption Alone", value: "$90 / mo" },
            { label: "With RateRudder", value: "$148 / mo (+$57)", highlight: true },
            { label: "Best Single Spike Day", value: "+$13.67 extra" }
        ]
    },
    {
        id: "negative-bill",
        badge: "Time-of-Use Optimization Result",
        tabValue: "-$50 Credit",
        tabLabel: "144% of Monthly Bill Eliminated",
        title: "Turning a $116 Electric Bill Into a $50 Utility Credit",
        description: "A FranklinWH home boosted its savings by a whopping 60%. RateRudder generated an extra $61, flipping an $11 electricity bill into a $50 net credit without the homeowner having to configure, monitor, or adjust anything.",
        metrics: [
            { label: "Grid-Only Bill", value: "$116 / mo" },
            { label: "Default Self-Consumption", value: "$11 bill left" },
            { label: "Net Bill with RateRudder", value: "-$50 credit", highlight: true }
        ]
    },
    {
        id: "forecast-accuracy",
        badge: "Zero-Config Solar & Load Modeling",
        tabValue: `${SOLAR_FORECAST_ACCURACY} / ${LOAD_FORECAST_ACCURACY}`,
        tabLabel: "Solar & Load Forecast Accuracy",
        title: "Auto-Calibrated to Your Roof, Shading & Household Routine",
        description: `Without asking for panel tilt, azimuth, or tree shading, RateRudder automatically learns your roof's profile from your system's history to hit ${SOLAR_FORECAST_ACCURACY} solar forecast accuracy while predicting home load with ${LOAD_FORECAST_ACCURACY} accuracy across weekend shifts, vacations, and EV charging.`,
        metrics: [
            { label: "Solar Forecast Accuracy", value: `${SOLAR_FORECAST_ACCURACY} (Auto-Shading)`, highlight: true },
            { label: "Home Load Accuracy", value: `${LOAD_FORECAST_ACCURACY} (Adaptive)`, highlight: true },
            { label: "EV & Vacation Detection", value: "100% Automatic" }
        ]
    },
    {
        id: "fleet-scale",
        badge: "Live Community Scale",
        tabValue: "1.25+ MWh",
        tabLabel: "Home Batteries Managed",
        title: "Megawatt-Scale Optimization Running 24/7",
        description: "Last month alone, RateRudder managed over 90 MWh of household consumption, optimized 65+ MWh of residential solar generation, and dispatched 20+ MWh of stored battery energy across Tesla Powerwall, FranklinWH, and Enphase homes.",
        metrics: [
            { label: "Batteries Managed", value: "Over 1.25 MWh", highlight: true },
            { label: "Solar Optimized (Last Mo)", value: "65+ MWh" },
            { label: "Home Energy Managed (Last Mo)", value: "90+ MWh" }
        ],
        rateRudderOnly: true
    },
    {
        id: "free-open-source",
        badge: "100% Free & Open Source",
        tabValue: "100% Free",
        tabLabel: "Open Source • Keep All Your Savings",
        title: "Keep Every Dollar Your Battery Saves You",
        description: "Instead of paying a monthly subscription fee that eats into your battery arbitrage returns, RateRudder is free during public beta and 100% open source on GitHub.",
        metrics: [
            { label: "Monthly Subscription", value: "$0 / mo", highlight: true },
            { label: "Extra Boxes Needed", value: "None (Cloud API)" },
            { label: "Source Code", value: "100% Open Source" }
        ],
        nonRateRudderOnly: true
    }
];

const faqData: { question: string; answer: React.ReactNode }[] = [
    {
        question: "How does RateRudder save me money?",
        answer: (
            <>
                RateRudder combines live utility rates with accurate solar forecasting and home load predictions to charge your battery only when electricity is cheapest and only to the exact level needed before free solar arrives, while discharging and exporting during peak hours.{' '}
                <Link to="/how-it-works" className="inline-feature-link">
                    Learn how SmartPlan works →
                </Link>
            </>
        )
    },
    {
        question: "Why is RateRudder better than my battery's built-in Self-Consumption or Time-of-Use mode?",
        answer: "Default Self-Consumption is blind to utility rates and often drains your battery on cheap morning load or into a charging EV, leaving it empty when afternoon prices spike. Static manual schedules do the opposite, blindly charging to 100% overnight even when free solar is hours away. RateRudder recalculates your optimal plan every hour with zero manual tweaking, delivering over 60% more savings."
    },
    {
        question: "Does using RateRudder void my battery warranty?",
        answer: "No. RateRudder requires no physical modifications or electrical work. We communicate exclusively through official manufacturer cloud APIs to manage modes and schedules, just like their mobile apps do."
    },
    {
        question: "What happens if there's a storm or blackout?",
        answer: "RateRudder respects your configured emergency backup reserve at all times. You can even configure time-windowed reserves, lowering your floor only during brief peak windows and automatically restoring a higher reserve overnight."
    },
    {
        question: "What equipment do I need?",
        answer: (
            <>
                You need an existing solar and home battery system. RateRudder currently supports Tesla Powerwall, FranklinWH aPower, and Enphase IQ Battery systems via their cloud APIs, with no extra hubs or wiring to install.{' '}
                <Link to="/getting-started" className="inline-feature-link">
                    See the 2-minute setup guide →
                </Link>
            </>
        )
    },
    {
        question: "Which utilities are supported?",
        answer: (
            <>
                We support 50+ utility companies and 120+ rate plans across the US, UK, and Australia, including ComEd, Ameren, Tesla Electric (ERCOT), PG&amp;E, Southern California Edison, SDG&amp;E, Eversource, Duke Energy, and many more.{' '}
                <Link to="/utilities" className="inline-feature-link">
                    Browse all supported utilities →
                </Link>
            </>
        )
    },
    {
        question: "How much does it cost?",
        answer: "Nothing! RateRudder is free during public beta and 100% open source. Rather than throwing away your hard-earned battery savings each month paying for another subscription app, you keep 100% of your savings for yourself."
    },
    {
        question: "Is RateRudder open source and safe for my electrical system?",
        answer: (
            <>
                Yes! Our entire optimization engine is{' '}
                <a href="https://github.com/raterudder/raterudder" target="_blank" rel="noopener noreferrer" className="inline-feature-link">
                    open source on GitHub
                </a>
                , so anyone can review the code, verify security, or contribute new integrations.
            </>
        )
    },
    {
        question: "Who made RateRudder?",
        answer: (
            <>
                I&apos;m{' '}
                <a href="https://jameshartig.dev/" target="_blank" rel="noopener noreferrer" className="inline-feature-link">
                    James Hartig
                </a>
                ! I started building RateRudder after getting my own home battery on ComEd hourly pricing and realizing built-in battery modes weren&apos;t designed for dynamic rates, and I opened it up to help other homeowners get the most out of their batteries too.
            </>
        )
    }
];

const LandingPage: React.FC = () => {
    usePageSEO(PUBLIC_PAGES_SEO.index);
    const [activePhase, setActivePhase] = useState(0);
    const [isAutoplay, setIsAutoplay] = useState(true);
    const [isMobile, setIsMobile] = useState(() => typeof window !== 'undefined' && window.innerWidth < 768);
    const autoplayRef = useRef<number | null>(null);

    useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    const isRateRudder = typeof window !== 'undefined' && (
        window.location.hostname === 'raterudder.com' ||
        window.location.hostname.endsWith('.raterudder.com')
    );

    const visibleSlides = React.useMemo(
        () => spotlightSlides.filter((s) => (!s.rateRudderOnly || isRateRudder) && (!s.nonRateRudderOnly || !isRateRudder)),
        [isRateRudder]
    );

    const [activeSlideIdx, setActiveSlideIdx] = useState(0);
    const [isCarouselAutoplay, setIsCarouselAutoplay] = useState(true);
    const [isCarouselHovered, setIsCarouselHovered] = useState(false);
    const [isPhaseHovered, setIsPhaseHovered] = useState(false);
    const touchStartXRef = useRef<number | null>(null);

    // Auto-cycle through the 4 daily optimization phases every 4.5 seconds
    useEffect(() => {
        if (isAutoplay && !isPhaseHovered) {
            autoplayRef.current = window.setInterval(() => {
                setActivePhase((prev) => (prev + 1) % 4);
            }, 4500);
        }
        return () => {
            if (autoplayRef.current) {
                clearInterval(autoplayRef.current);
            }
        };
    }, [isAutoplay, isPhaseHovered]);

    // Auto-cycle through spotlight carousel slides every 6 seconds
    useEffect(() => {
        if (!isCarouselAutoplay || isCarouselHovered || visibleSlides.length <= 1) {
            return;
        }
        const timer = window.setInterval(() => {
            setActiveSlideIdx((prev) => (prev + 1) % visibleSlides.length);
        }, 6000);
        return () => clearInterval(timer);
    }, [isCarouselAutoplay, isCarouselHovered, visibleSlides.length]);

    const handlePhaseSelect = (phaseId: number) => {
        setIsAutoplay(false);
        setActivePhase(phaseId);
    };

    const handleSlideSelect = (idx: number) => {
        setIsCarouselAutoplay(false);
        setActiveSlideIdx(idx);
    };

    const handlePrevSlide = () => {
        setIsCarouselAutoplay(false);
        setActiveSlideIdx((prev) => (prev - 1 + visibleSlides.length) % visibleSlides.length);
    };

    const handleNextSlide = () => {
        setIsCarouselAutoplay(false);
        setActiveSlideIdx((prev) => (prev + 1) % visibleSlides.length);
    };

    const handleTouchStart = (e: React.TouchEvent) => {
        if (e.touches.length > 0) {
            touchStartXRef.current = e.touches[0].clientX;
        }
    };

    const handleTouchEnd = (e: React.TouchEvent) => {
        if (touchStartXRef.current === null || e.changedTouches.length === 0) {
            return;
        }
        const deltaX = e.changedTouches[0].clientX - touchStartXRef.current;
        touchStartXRef.current = null;
        if (Math.abs(deltaX) > 40) {
            if (deltaX < 0) {
                handleNextSlide();
            } else {
                handlePrevSlide();
            }
        }
    };

    const currentPhase = phases[activePhase];
    const currentSlide = visibleSlides[activeSlideIdx % visibleSlides.length];

    return (
        <div className="landing-page">
            {/* 1. Sky-to-Cobalt Hero Section with Mobile Dashboard Preview */}
            <section className="hero-section">
                <div className="content-container hero-layout">
                    <div className="hero-content">
                        {isRateRudder && (
                            <div className="badge">Limited Beta Now Open</div>
                        )}
                        <h1>Maximize Battery Savings, <span className="highlight">Zero Effort.</span></h1>
                        <p>
                            RateRudder automatically charges your battery when power is cheap (or negative!),
                            holds energy through expensive peak hours, and exports solar when buyback credits surge, delivering{' '}
                            <strong>over 60% more savings</strong> than default self-consumption with zero daily tweaking.
                        </p>
                        <div className="cta-wrapper">
                            <a href="/login" className="cta-button">
                                Get Started for Free
                            </a>
                            <div className="compat-badges" aria-label="Supported Systems">
                                <span className="compat-badge">Powerwall</span>
                                <span className="compat-bullet" aria-hidden="true">•</span>
                                <span className="compat-badge">FranklinWH</span>
                                <span className="compat-bullet" aria-hidden="true">•</span>
                                <span className="compat-badge">Enphase</span>
                                <span className="compat-bullet" aria-hidden="true">•</span>
                                <Link to="/utilities" className="compat-badge compat-badge-link">
                                    50+ Utilities →
                                </Link>
                            </div>
                        </div>
                    </div>

                    <div className="hero-visual-wrapper" aria-hidden="true">
                        <div className="dashboard-phone-mockup">
                            {/* Realistic Hardware Side Buttons */}
                            <span className="phone-hw-btn phone-vol-up" />
                            <span className="phone-hw-btn phone-vol-down" />
                            <span className="phone-hw-btn phone-power" />

                            <div className="phone-notch-bar">
                                <span className="phone-speaker" />
                            </div>

                            {/* Authentic Mobile App Header with Forecast Link & Hamburger Menu */}
                            <div className="phone-app-header">
                                <div className="phone-header-left">
                                    <div className="phone-brand">
                                        <img src="/logo.svg" alt="" className="phone-logo-icon" />
                                        <span>RateRudder</span>
                                    </div>
                                    <span className="phone-nav-pill">Forecast</span>
                                </div>
                                <div className="phone-hamburger">
                                    <span />
                                    <span />
                                    <span />
                                </div>
                            </div>

                            {/* Mocked CurrentStatus Card (No Left Colored Border) */}
                            <div className="mock-status-card">
                                <div className="mock-status-top">
                                    <div className="mock-status-icon">☀️</div>
                                    <div className="mock-status-text">
                                        <span className="mock-status-eyebrow">Direct Solar Export</span>
                                        <span className="mock-status-main">Exporting Solar • Home on Battery</span>
                                        <span className="mock-status-sub">Next: Self-Powered at 4:00 PM ($0.18/kWh)</span>
                                    </div>
                                </div>
                                <div className="mock-status-metrics">
                                    <div className="mock-status-metric">
                                        <span className="mock-metric-lbl">Battery</span>
                                        <span className="mock-metric-val">88.0%</span>
                                        <div className="mock-battery-track">
                                            <div className="mock-battery-fill" style={{ width: '88%' }} />
                                        </div>
                                    </div>
                                    <div className="mock-status-metric">
                                        <span className="mock-metric-lbl">Price</span>
                                        <span className="mock-metric-val">$0.142<small>/kWh</small></span>
                                    </div>
                                </div>
                            </div>

                            {/* Mocked SavingsHero Card (Today, Last 7 Days, Last 30 Days) */}
                            <div className="mock-savings-card">
                                <span className="mock-savings-eyebrow">Today Savings</span>
                                <div className="mock-savings-hero-row">
                                    <span className="mock-savings-big">$6.42</span>
                                    <span className="mock-savings-without">($3.85)</span>
                                </div>
                                <span className="mock-savings-subcaption">Estimated. (Without RateRudder).</span>

                                <div className="mock-periods-list">
                                    <div className="mock-period-row">
                                        <span className="mock-dot credit" />
                                        <span className="mock-period-name">Last 7 Days</span>
                                        <span className="mock-period-vals">
                                            <strong className="pos">+$38.50</strong>
                                            <span className="sec">($23.10)</span>
                                            <span className="chev">›</span>
                                        </span>
                                    </div>
                                    <div className="mock-period-row">
                                        <span className="mock-dot period-30d" />
                                        <span className="mock-period-name">Last 30 Days</span>
                                        <span className="mock-period-vals">
                                            <strong className="pos">+$148.00</strong>
                                            <span className="sec">($90.80)</span>
                                            <span className="chev">›</span>
                                        </span>
                                    </div>
                                </div>

                                <div className="mock-usage-mini-grid">
                                    <div className="mock-usage-cell">
                                        <span className="lbl">Home Usage</span>
                                        <span className="val">28.4 <small>kWh</small></span>
                                    </div>
                                    <div className="mock-usage-cell">
                                        <span className="lbl">Solar Gen</span>
                                        <span className="val">41.2 <small>kWh</small></span>
                                    </div>
                                    <div className="mock-usage-cell">
                                        <span className="lbl">Battery Use</span>
                                        <span className="val">19.5 <small>kWh</small></span>
                                    </div>
                                </div>
                            </div>

                            <div className="phone-home-indicator-row">
                                <span className="phone-home-indicator" />
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* 2. Real-World Proof & Case Studies */}
            <section className="results-carousel-section" aria-label="Real-World Savings Results">
                <div className="content-container">
                    <div className="section-eyebrow-header">
                        <span className="section-eyebrow">PROVEN TELEMETRY &amp; REAL-WORLD IMPACT</span>
                    </div>

                    <div
                        className="unified-proof-frame"
                        onMouseEnter={() => setIsCarouselHovered(true)}
                        onMouseLeave={() => setIsCarouselHovered(false)}
                    >
                        <div className="impact-stats-ribbon" role="tablist" aria-label="Performance Highlights">
                            {visibleSlides.map((slide, idx) => {
                                const isActive = idx === activeSlideIdx % visibleSlides.length;
                                return (
                                    <button
                                        key={slide.id}
                                        type="button"
                                        role="tab"
                                        aria-selected={isActive}
                                        className={`impact-stat-pill ${isActive ? 'active' : ''}`}
                                        onClick={() => handleSlideSelect(idx)}
                                    >
                                        <span className="impact-stat-val">
                                            {slide.tabValue}
                                        </span>
                                        <span className="impact-stat-lbl">{slide.tabLabel}</span>
                                    </button>
                                );
                            })}
                        </div>

                        <div
                            className="spotlight-carousel-card"
                            onTouchStart={handleTouchStart}
                            onTouchEnd={handleTouchEnd}
                        >
                            <div className="spotlight-card-body">
                                <div className="spotlight-details-column">
                                    <div className="spotlight-header-row">
                                        <span className="spotlight-badge">{currentSlide.badge}</span>
                                    </div>
                                    <h2>{currentSlide.title}</h2>
                                    <p>{currentSlide.description}</p>
                                    <div className="spotlight-metrics-row">
                                        {currentSlide.metrics.map((m, idx) => (
                                            <div key={idx} className={`spotlight-metric-box ${m.highlight ? 'highlighted' : ''}`}>
                                                <span className="spotlight-metric-label">{m.label}</span>
                                                <span className="spotlight-metric-value">{m.value}</span>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            </div>

                            <div className="spotlight-carousel-controls">
                                <button
                                    type="button"
                                    className="carousel-nav-btn"
                                    onClick={handlePrevSlide}
                                    aria-label="Previous highlight"
                                >
                                    ‹
                                </button>
                                <div className="carousel-dots" role="tablist" aria-label="Select highlight slide">
                                    {visibleSlides.map((slide, idx) => (
                                        <button
                                            key={slide.id}
                                            type="button"
                                            role="tab"
                                            aria-selected={idx === activeSlideIdx % visibleSlides.length}
                                            aria-label={`Highlight ${idx + 1}: ${slide.badge}`}
                                            className={`carousel-dot ${idx === activeSlideIdx % visibleSlides.length ? 'active' : ''}`}
                                            onClick={() => handleSlideSelect(idx)}
                                        />
                                    ))}
                                </div>
                                <button
                                    type="button"
                                    className="carousel-nav-btn"
                                    onClick={handleNextSlide}
                                    aria-label="Next highlight"
                                >
                                    ›
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* 3. Interactive Daily Optimization Section with Cropped Forecast Phone Frame */}
            <section className="chart-section">
                <div className="content-container">
                    <div className="section-header">
                        <span className="section-eyebrow">24-HOUR DYNAMIC SCHEDULE</span>
                        <h2>Daily Optimization</h2>
                        <p>
                            See how RateRudder&apos;s <Link to="/how-it-works" className="inline-feature-link">SmartPlan engine</Link> outperforms default Self-Consumption across a real 24-hour cycle. Tap any phase to compare:
                        </p>
                    </div>

                    <div
                        className="interactive-chart-container"
                        onMouseEnter={() => setIsPhaseHovered(true)}
                        onMouseLeave={() => setIsPhaseHovered(false)}
                    >
                        {/* Cropped Mobile Forecast Phone (No Blue Mat) */}
                        <div className="forecast-phone-frame">
                            <span className="phone-hw-btn phone-vol-up" aria-hidden="true" />
                            <span className="phone-hw-btn phone-vol-down" aria-hidden="true" />
                            <span className="phone-hw-btn phone-power" aria-hidden="true" />

                            <div className="forecast-phone-notch" aria-hidden="true">
                                <span className="phone-speaker" />
                            </div>

                            <div className="phone-app-header forecast-app-header" aria-hidden="true">
                                <div className="phone-header-left">
                                    <div className="phone-brand">
                                        <img src="/logo.svg" alt="" className="phone-logo-icon" />
                                        <span>RateRudder</span>
                                    </div>
                                    <span className="phone-nav-pill">Dashboard</span>
                                </div>
                                <div className="phone-hamburger">
                                    <span />
                                    <span />
                                    <span />
                                </div>
                            </div>

                            <div className="forecast-phone-topbar">
                                <div className="forecast-topbar-left">
                                    <span className="forecast-screen-title">Planned Battery SOC (%)</span>
                                    <span className="forecast-plan-badge">SmartPlan</span>
                                </div>
                                <div className="chart-comparison-legend" aria-label="Chart Legend">
                                    <span className="legend-item">
                                        <span className="legend-line legend-line-primary" />
                                        RateRudder
                                    </span>
                                    <span className="legend-item">
                                        <span className="legend-line legend-line-dashed" />
                                        Self-Consumption
                                    </span>
                                </div>
                            </div>

                            <div className="chart-visual-container" aria-hidden="true">
                                <ResponsiveContainer width="100%" height={isMobile ? 190 : 235}>
                                    <AreaChart data={segmentedChartData} margin={{ top: 10, right: isMobile ? 8 : 20, left: isMobile ? -25 : -10, bottom: 0 }}>
                                        <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.06)" vertical={false} />
                                        <XAxis
                                            dataKey="time"
                                            stroke="rgba(255,255,255,0.35)"
                                            fontSize={isMobile ? 10 : 12}
                                            tickLine={false}
                                            ticks={isMobile
                                                ? ['12 AM', '4 AM', '8 AM', '12 PM', '4 PM', '8 PM']
                                                : ['12 AM', '3 AM', '6 AM', '9 AM', '12 PM', '3 PM', '6 PM', '9 PM', '11 PM']}
                                            tickMargin={6}
                                        />
                                        <YAxis
                                            stroke="rgba(255,255,255,0.35)"
                                            fontSize={isMobile ? 10 : 12}
                                            tickLine={false}
                                            axisLine={false}
                                            domain={[0, 100]}
                                            ticks={isMobile ? [0, 50, 100] : [0, 20, 40, 60, 80, 100]}
                                            unit="%"
                                        />

                                        {/* Minimum Reserve Line */}
                                        <ReferenceLine y={20} stroke="rgba(255, 255, 255, 0.2)" strokeDasharray="3 3" />

                                        {/* Highlighted Phase Area */}
                                        <ReferenceArea
                                            x1={phases[activePhase].x1}
                                            x2={phases[activePhase].x2}
                                            fill={phases[activePhase].color}
                                            fillOpacity={0.14}
                                            stroke={phases[activePhase].color}
                                            strokeOpacity={0.35}
                                        />
                                        <ReferenceLine x={phases[activePhase].x1} stroke={phases[activePhase].color} strokeOpacity={0.7} strokeDasharray="2 2" />
                                        <ReferenceLine x={phases[activePhase].x2} stroke={phases[activePhase].color} strokeOpacity={0.7} strokeDasharray="2 2" />

                                        {/* Baseline Self-Consumption comparison curve */}
                                        <Line
                                            type="monotone"
                                            dataKey="manualSoc"
                                            stroke="rgba(193, 198, 215, 0.55)"
                                            strokeWidth={2}
                                            strokeDasharray="5 5"
                                            dot={false}
                                            isAnimationActive={false}
                                        />

                                        {/* Multi-color RateRudder mode segments */}
                                        {ALL_ZONES.map((z) => (
                                            <Line
                                                key={z.key}
                                                type="monotone"
                                                dataKey={`soc_${z.key}`}
                                                stroke={z.color}
                                                strokeWidth={3}
                                                dot={false}
                                                connectNulls={false}
                                                isAnimationActive={false}
                                            />
                                        ))}
                                    </AreaChart>
                                </ResponsiveContainer>
                            </div>
                        </div>

                        {/* Phase Selector Tabs directly between Phone Screen and Info Panel */}
                        <div className="phase-selectors">
                            {phases.map((phase) => (
                                <button
                                    key={phase.id}
                                    type="button"
                                    className={`phase-btn ${activePhase === phase.id ? 'active' : ''}`}
                                    onClick={() => handlePhaseSelect(phase.id)}
                                    style={{
                                        '--phase-color': phase.color,
                                        borderColor: activePhase === phase.id ? phase.color : undefined,
                                        background: activePhase === phase.id ? `${phase.color}14` : undefined
                                    } as React.CSSProperties}
                                >
                                    <span className="btn-icon">{phase.icon}</span>
                                    <div className="btn-text">
                                        <span className="btn-title">{phase.title}</span>
                                        <span className="btn-time">{isMobile ? phase.shortTime : phase.time}</span>
                                    </div>
                                </button>
                            ))}
                        </div>

                        {/* Interactive Phase Description Panel */}
                        <div className="phase-info-panel" style={{ borderTopColor: currentPhase.color }}>
                            <div className="panel-header">
                                <h3>{currentPhase.title} <span className="time-sub">({currentPhase.time})</span></h3>
                            </div>
                            <div className="phase-comparison-grid">
                                <div className="phase-comparison-card raterudder-way">
                                    <span className="comparison-tag tag-positive">✓ With RateRudder</span>
                                    <p className="panel-desc">{currentPhase.rateRudderWay}</p>
                                </div>
                                <div className="phase-comparison-card manual-way">
                                    <span className="comparison-tag tag-negative">✕ Self-Consumption &amp; Manual Rules</span>
                                    <p className="panel-desc">{currentPhase.manualWay}</p>
                                </div>
                            </div>
                            <div className="panel-metrics">
                                <div className="metric-pill">
                                    <span className="pill-label">Grid Rate:</span>
                                    <span className="pill-val" style={{ color: currentPhase.priceColor }}>{currentPhase.price}</span>
                                </div>
                                <div className="metric-pill">
                                    <span className="pill-label">RateRudder SOC:</span>
                                    <span className="pill-val" style={{ color: currentPhase.color }}>{currentPhase.socChange}</span>
                                </div>
                                <div className="metric-pill">
                                    <span className="pill-label">Self-Consumption SOC:</span>
                                    <span className="pill-val text-muted-val">{currentPhase.manualSocChange}</span>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* 4. Simple 2-Minute Setup — Cobalt Feature Block + Settings UI Preview */}
            <section className="setup-section">
                <div className="content-container">
                    <div className="setup-cobalt-stage">
                        <div className="section-header setup-header">
                            <span className="section-eyebrow light-eyebrow">2-MINUTE CLOUD SETUP</span>
                            <h2>Simple Setup</h2>
                            <p>Automate your battery in under two minutes using your existing manufacturer cloud account.</p>
                        </div>

                        <div className="setup-split-layout">
                            <div className="setup-steps-wrapper">
                                <div className="setup-step">
                                    <div className="step-badge">1</div>
                                    <div className="step-icon-container">
                                        <svg className="step-icon" viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                            <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                                            <circle cx="12" cy="7" r="4" />
                                        </svg>
                                    </div>
                                    <div className="setup-step-copy">
                                        <h3>Sign In</h3>
                                        <p>Log in with your existing Google or Apple account. No new passwords to remember.</p>
                                    </div>
                                </div>

                                <div className="setup-step">
                                    <div className="step-badge">2</div>
                                    <div className="step-icon-container">
                                        <svg className="step-icon" viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                            <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
                                        </svg>
                                    </div>
                                    <div className="setup-step-copy">
                                        <h3>Pick Your Utility</h3>
                                        <p>Select your electric utility and rate plan from over 50 supported providers.</p>
                                    </div>
                                </div>

                                <div className="setup-step">
                                    <div className="step-badge">3</div>
                                    <div className="step-icon-container">
                                        <svg className="step-icon" viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                            <rect x="2" y="7" width="16" height="10" rx="2" ry="2" />
                                            <path d="M22 11v2" />
                                            <path d="M9 10l-2 3h4l-2 3" />
                                        </svg>
                                    </div>
                                    <div className="setup-step-copy">
                                        <h3>Link Your Battery</h3>
                                        <p>Connect to your Tesla Powerwall, FranklinWH, or Enphase system.</p>
                                    </div>
                                </div>

                                <div className="setup-guide-cta-row">
                                    <Link to="/getting-started" className="setup-deepdive-link">
                                        2-Minute Setup Guide →
                                    </Link>
                                    <Link to="/utilities" className="setup-deepdive-link">
                                        Browse All Supported Utilities →
                                    </Link>
                                </div>
                            </div>

                            {/* Non-Interactive Settings UI Preview Card */}
                            <div className="mock-settings-card" aria-hidden="true">
                                <div className="mock-settings-header">
                                    <span className="mock-settings-title">Site Settings</span>
                                </div>

                                <div className="mock-settings-field">
                                    <span className="mock-field-label">Utility Provider &amp; Rate Plan</span>
                                    <div className="mock-select-box">
                                        <span>ComEd • Hourly Pricing (BESH)</span>
                                        <span className="mock-select-chevron">▾</span>
                                    </div>
                                </div>

                                <div className="mock-settings-field">
                                    <span className="mock-field-label">Energy Storage System</span>
                                    <div className="mock-connected-row">
                                        <span className="mock-connected-dot" />
                                        <span className="mock-connected-name">FranklinWH aPower</span>
                                        <span className="mock-connected-tag">Connected</span>
                                    </div>
                                </div>

                                <div className="mock-settings-field">
                                    <span className="mock-field-label">Optimization Profile</span>
                                    <div className="mock-select-box">
                                        <span>Balanced (Default)</span>
                                        <span className="mock-select-chevron">▾</span>
                                    </div>
                                </div>

                                <div className="mock-settings-field">
                                    <span className="mock-field-label">Backup Reserve</span>
                                    <div className="mock-connected-row">
                                        <span className="mock-connected-name">20% Overnight • 5% Peak</span>
                                        <span className="mock-reserve-pill">Time-based</span>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* 5. Benefits, Features & Decision Factors Grid */}
            <section className="features-strip">
                <div className="content-container">
                    <div className="section-header">
                        <span className="section-eyebrow">INTELLIGENT AUTOMATION</span>
                        <h2>Built for Effortless Ownership</h2>
                        <p>Set it up once in two minutes and let RateRudder handle the rest safely in the cloud.</p>
                    </div>
                    <div className="features-grid features-grid-four">
                        <div className="feature-item arbitrage">
                            <div className="icon" aria-hidden="true">⚡</div>
                            <h3>100% Hands-Free Automation</h3>
                            <p>RateRudder recalculates your optimal charging and export schedule every hour as live utility rates and cloud cover shift, with zero manual rules to maintain.</p>
                        </div>
                        <div className="feature-item intelligence">
                            <div className="icon" aria-hidden="true">🧠</div>
                            <h3>Smart Charging &amp; EV Detection</h3>
                            <p>RateRudder learns your home&apos;s unique energy footprint, roof shading, and EV charging spikes and works seamlessly through official manufacturer cloud APIs.</p>
                        </div>
                        <div className="feature-item notifications">
                            <div className="icon" aria-hidden="true">🔔</div>
                            <h3>Proactive Smart Notifications</h3>
                            <p>Stay informed without opening an app. Get optional alerts for real-time price spikes, grid outages, and abnormal load, plus daily morning plans and evening savings summaries.</p>
                            <div className="mock-notif-popdown" aria-hidden="true">
                                <div className="mock-notif-icon-wrap">
                                    <img src="/logo.svg" alt="" className="mock-notif-logo" />
                                </div>
                                <div className="mock-notif-content">
                                    <div className="mock-notif-toprow">
                                        <span className="mock-notif-app">RateRudder</span>
                                        <span className="mock-notif-time">8:00 AM</span>
                                    </div>
                                    <span className="mock-notif-title">☀️ Great Solar Day Ahead</span>
                                    <span className="mock-notif-body">Battery at 38%. Full battery expected by 1:45 PM.</span>
                                </div>
                            </div>
                        </div>
                        <div className="feature-item grid">
                            <div className="icon" aria-hidden="true">🔓</div>
                            <h3>Free &amp; Open Source</h3>
                            <p>Why throw away your savings each month paying for another battery app? RateRudder is free during public beta and <a href="https://github.com/raterudder/raterudder" target="_blank" rel="noopener noreferrer" className="inline-feature-link">open source on GitHub</a> so you keep 100% of what you save. <Link to="/changelog" className="inline-feature-link">See recent updates →</Link></p>
                        </div>
                    </div>

                    {/* Decision Factors Strip */}
                    <div className="decision-factors-card">
                        <div className="card-header">
                            <span className="section-eyebrow">UNDER THE HOOD</span>
                            <h3>Decision Factors</h3>
                            <p className="card-sub">Every hour, RateRudder combines five live signals to compute your optimal battery schedule:</p>
                        </div>
                        <ul className="factors-list">
                            <li className="factor-item-check">
                                <span className="check-icon">✓</span>
                                <div className="factor-details">
                                    <strong>Zero-Config Solar Forecasting</strong>
                                    <span>Auto-detects roof orientation and tree shading from historical production and weather forecasts.</span>
                                </div>
                            </li>
                            <li className="factor-item-check">
                                <span className="check-icon">✓</span>
                                <div className="factor-details">
                                    <strong>Adaptive Home Load Prediction</strong>
                                    <span>Learns weekday/weekend shifts, vacation days, and HVAC temperature sensitivity.</span>
                                </div>
                            </li>
                            <li className="factor-item-check">
                                <span className="check-icon">✓</span>
                                <div className="factor-details">
                                    <strong>Automated EV Charging Detection</strong>
                                    <span>Spots high-draw EV charging spikes so your home battery isn&apos;t accidentally drained into your car.</span>
                                </div>
                            </li>
                            <li className="factor-item-check">
                                <span className="check-icon">✓</span>
                                <div className="factor-details">
                                    <strong>Direct Solar &amp; Battery Peak Export</strong>
                                    <span>Routes solar or stored battery energy directly to the grid when export credits surge.</span>
                                </div>
                            </li>
                            <li className="factor-item-check">
                                <span className="check-icon">✓</span>
                                <div className="factor-details">
                                    <strong>Time-Windowed Backup Reserves</strong>
                                    <span>Lower your reserve floor during peak hours for extra savings, then restore storm protection overnight.</span>
                                </div>
                            </li>
                        </ul>
                        <div className="factors-deepdive-footer">
                            <span className="factors-deepdive-text">
                                Curious why pure price optimization fails without reserve shields, hysteresis, and cycle wear guardrails?
                            </span>
                            <Link to="/how-it-works" className="factors-deepdive-link">
                                Explore How SmartPlan Works →
                            </Link>
                        </div>
                    </div>
                </div>
            </section>

            {/* 6. FAQ & Closing CTA */}
            <section className="faq-section">
                <div className="content-container">
                    <div className="section-header">
                        <span className="section-eyebrow">FREQUENTLY ASKED QUESTIONS</span>
                        <h2>Common Questions</h2>
                    </div>
                    <Accordion.Root className="faq-container">
                        {faqData.map((item, index) => (
                            <Accordion.Item key={index} className="faq-item" value={index}>
                                <Accordion.Header>
                                    <Accordion.Trigger className="faq-question">
                                        <span>{item.question}</span>
                                        <span className="toggle-icon" aria-hidden="true" />
                                    </Accordion.Trigger>
                                </Accordion.Header>
                                <Accordion.Panel className="faq-answer">
                                    <p>{item.answer}</p>
                                </Accordion.Panel>
                            </Accordion.Item>
                        ))}
                    </Accordion.Root>

                    <div className="closing-cta-card">
                        <h2>Ready to Maximize Your Battery Savings?</h2>
                        <p>Connect your Powerwall, FranklinWH, or Enphase battery in minutes and keep 100% of what you save.</p>
                        <a href="/login" className="cta-button">
                            Get Started for Free
                        </a>
                    </div>

                    <p className="marketing-disclaimer">
                        *Actual savings vary by utility plan, battery capacity, solar generation, and household usage.
                    </p>
                </div>
            </section>
        </div>
    );
};

export default LandingPage;
