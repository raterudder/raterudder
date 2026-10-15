import React, { useState, useEffect, useMemo } from 'react';
import { Link } from 'wouter';
import { fetchUtilities, type UtilityProviderInfo } from '../api';
import { usePageSEO, PUBLIC_PAGES_SEO } from '../utils/seo';
import './MarketingPages.css';

interface UtilityMetadata {
    region: string;
    category: 'realtime' | 'california' | 'texas' | 'tou' | 'international';
    blurb: string;
}

const UTILITY_METADATA: Record<string, UtilityMetadata> = {
    comed: {
        region: 'Illinois',
        category: 'realtime',
        blurb: 'Fetches live real-time prices from ComEd alongside PJM Day-Ahead hourly rates. Day-Ahead prices and 5-minute real-time prices are adjusted in upcoming hours against live trends to significantly reduce forecast error.'
    },
    ameren: {
        region: 'Illinois',
        category: 'realtime',
        blurb: 'Pulls official MISO Day-Ahead market reports directly as soon as next-day clearing prices are published and pairs them with your Ameren Illinois delivery Zone (I, II, or III) and space heating riders.'
    },
    tesla_electric_tx: {
        region: 'Texas (ERCOT)',
        category: 'texas',
        blurb: 'Connects directly to the official ERCOT Public API for Real-Time Settlement Point Prices (SPP) and Day-Ahead Market (DAM) prices, adding regulated PUCT wire fees for CenterPoint, Oncor, AEP, or TNMP.'
    },
    green_mountain_tx: {
        region: 'Texas (ERCOT)',
        category: 'texas',
        blurb: 'Models overnight $0.00/kWh Free Nights charging windows paired with regulated Texas TDU delivery rates (CenterPoint, Oncor, AEP Texas, TNMP) so your battery refills for free overnight.'
    },
    direct_energy_tx: {
        region: 'Texas (ERCOT)',
        category: 'texas',
        blurb: 'Optimizes Direct Energy Twelve Hour Power (9 PM to 9 AM free energy) and fixed plans with automatic Texas TDU delivery fee modeling.'
    },
    txu_tx: {
        region: 'Texas (ERCOT)',
        category: 'texas',
        blurb: 'Automates TXU Free Nights & Solar Days (8 PM to 6 AM free energy) and Season Pass plans across all 5 ERCOT Transmission and Distribution Utilities.'
    },
    pg_e: {
        region: 'California',
        category: 'california',
        blurb: 'Models the full California ACC Net Billing Tariff (NEM 3.0 / SBP) export table, capturing September evening export surges alongside NEM 2.0 non-bypassable charges across E-TOU-C, E-TOU-D, EV2-A, and E-ELEC.'
    },
    sce: {
        region: 'California',
        category: 'california',
        blurb: 'Optimizes Southern California Edison TOU-D-4-9PM, TOU-D-5-8PM, and TOU-D-PRIME tariffs with full support for NEM 1.0, NEM 2.0, and NEM 3.0 hourly Avoided Cost Calculator export credits.'
    },
    sdge: {
        region: 'California',
        category: 'california',
        blurb: 'Supports SDG&E TOU-DR1, TOU-DR2, TOU-ELEC, DR-SES, and EV-TOU-2/EV-TOU-5 schedules across Coastal, Inland, Mountain, and Desert climate zones with NEM 2.0 and Solar Billing Plan (NEM 3.0) export matrices.'
    },
    ladwp: {
        region: 'California',
        category: 'california',
        blurb: 'Automates Los Angeles Department of Water & Power Standard (R-1A) and 3-tier Time-of-Use (R-1B) schedules across summer and winter seasonal windows.'
    },
    bwp: {
        region: 'California',
        category: 'california',
        blurb: 'Models Burbank Water and Power Residential Flat and R-TOU-EV schedules to avoid afternoon peak imports and charge during off-peak EV windows.'
    },
    eversource: {
        region: 'CT • MA • NH',
        category: 'realtime',
        blurb: 'Fetches live Eversource CT Rate 7 Variable Peak Pricing (VPP) daily rates, models CT Renewable Energy Solutions (2022-2026) export adjustments, and automatically pre-charges to 100% before ConnectedSolutions VPP events.'
    },
    duke_carolinas_nc: {
        region: 'North Carolina',
        category: 'tou',
        blurb: 'Models Duke Energy Carolinas NC Time-of-Use, Critical Peak Pricing (RSTC/RETC), and PowerPair Avoided Cost Schedule PP export credits across summer evening and winter morning peaks.'
    },
    duke_carolinas_sc: {
        region: 'South Carolina',
        category: 'tou',
        blurb: 'Supports Duke Energy Carolinas SC Residential TOU, Solar TOU (R-STOU), and Critical Peak schedules with net metering and avoided-cost export riders.'
    },
    duke_progress_nc: {
        region: 'North Carolina',
        category: 'tou',
        blurb: 'Optimizes Duke Energy Progress NC Standard, R-TOU, and R-TOU-CPP tariffs with Schedule PP avoided-cost export modeling.'
    },
    duke_progress_sc: {
        region: 'South Carolina',
        category: 'tou',
        blurb: 'Automates Duke Energy Progress SC Residential and R-TOU-EV schedules including SC Solar Integration Services export adjustments.'
    },
    duke_indiana: {
        region: 'Indiana',
        category: 'tou',
        blurb: 'Supports Duke Energy Indiana Standard and optional Time-of-Use rates with Excess Distributed Generation (EDG) or 1:1 net metering credits.'
    },
    duke_florida: {
        region: 'Florida',
        category: 'tou',
        blurb: 'Models Duke Energy Florida RS-1 and RST-1 Time-of-Use schedules across summer afternoon and winter morning peak windows.'
    },
    srp: {
        region: 'Arizona',
        category: 'tou',
        blurb: 'Supports Salt River Project E-13, E-14 (EV Export), E-15, E-16, E-27, and E-28 plans, accounting for SRP fixed solar export credits vs. high summer peak import spreads.'
    },
    aps: {
        region: 'Arizona',
        category: 'tou',
        blurb: 'Optimizes Arizona Public Service TOU-E, R-3, and R-EV plans alongside Resource Comparison Proxy (RCP) solar export credits.'
    },
    heco: {
        region: 'Hawai‘i (O‘ahu)',
        category: 'tou',
        blurb: 'Models Hawaiian Electric Schedule ARD TOU R (Shift and Save), where daytime solar rates are roughly a third of the evening peak, paired with SRE, CGS+, CGS, and Smart Export riders.'
    },
    helco: {
        region: 'Hawai‘i Island',
        category: 'tou',
        blurb: 'Optimizes Hawai‘i Electric Light Schedule R and ARD TOU R on Hawai‘i Island against steep evening peaks and island-specific export riders.'
    },
    meco: {
        region: 'Maui • Lāna‘i • Moloka‘i',
        category: 'tou',
        blurb: 'Supports Maui Electric Schedule R and ARD TOU R with per-island rate tables and export credits for Maui, Lāna‘i, and Moloka‘i.'
    },
    xcel: {
        region: 'CO • MN • MI • WI • TX • ND • SD',
        category: 'tou',
        blurb: 'Comprehensive coverage across Xcel Energy residential, space-heating, and Time-of-Use rate schedules in Colorado, Minnesota, Michigan, Wisconsin, Texas, North Dakota, and South Dakota.'
    },
    dominion: {
        region: 'VA • NC • SC',
        category: 'tou',
        blurb: 'Supports Dominion Energy schedules across Virginia (Schedule 1 & 1G TOU), North Carolina (1, 1P, 1T), and South Carolina (Rates 5, 6, and 8).'
    },
    psegli: {
        region: 'New York (Long Island)',
        category: 'tou',
        blurb: 'Models all 6 PSEG Long Island Time-of-Day and TOU rates (Rates 190, 191, 192, 193, 194, and 195) to pre-charge during Super Off-Peak overnight windows.'
    },
    bge: {
        region: 'Maryland',
        category: 'tou',
        blurb: 'Optimizes Baltimore Gas & Electric Schedule R, Schedule RL (Optional TOU), Schedule EV, and Schedule RD across summer and winter peak windows.'
    },
    pepco_dc: {
        region: 'Washington, D.C.',
        category: 'tou',
        blurb: 'Supports Pepco DC Rate R and Rate R-PIV (Plug-In Vehicle Charging) Time-of-Use schedules.'
    },
    peco: {
        region: 'Pennsylvania',
        category: 'tou',
        blurb: 'Models PECO Energy Rate R/RH and Residential Time-of-Use schedules with seasonal afternoon peak avoidance.'
    },
    dlc: {
        region: 'Pennsylvania',
        category: 'tou',
        blurb: 'Supports Duquesne Light Co. Residential Service (RS) and Time-of-Use Supply Rate Pilot schedules.'
    },
    gp: {
        region: 'Georgia',
        category: 'tou',
        blurb: 'Automates Georgia Power TOU-OA (Overnight Advantage), TOU-RD (Residential Demand), and TOU-REO (Nights & Weekends) with RNR solar export modeling.'
    },
    sawnee: {
        region: 'Georgia',
        category: 'tou',
        blurb: 'Supports Sawnee EMC Schedule H, Schedule TU (Time-of-Use), and Schedule CPPR (Critical Peak Pricing).'
    },
    walton: {
        region: 'Georgia',
        category: 'tou',
        blurb: 'Optimizes Walton EMC Schedule A-15 and Schedule TU-5 Time-of-Use tariffs.'
    },
    fpl: {
        region: 'Florida',
        category: 'tou',
        blurb: 'Models Florida Power & Light Residential Service (RS-1) and Residential Time of Use Rider (RTR-1).'
    },
    jea: {
        region: 'Florida (Jacksonville)',
        category: 'tou',
        blurb: 'Supports JEA Rate R and Rate GST (General Service Time-of-Day) with asymmetric solar export credit modeling.'
    },
    seco: {
        region: 'Florida',
        category: 'tou',
        blurb: 'Optimizes SECO Energy Schedule RS and Schedule RS-TOU tariffs.'
    },
    rocky_mountain_power: {
        region: 'UT • ID • WY',
        category: 'tou',
        blurb: 'Supports Rocky Mountain Power residential and Time-of-Use schedules across Utah, Idaho, and Wyoming with Schedule 135/136 net billing export rates.'
    },
    idaho: {
        region: 'Idaho',
        category: 'tou',
        blurb: 'Models Idaho Power Standard Residential (Schedule 1) and On-Site Generation Time-of-Use (Schedule 6) tariffs.'
    },
    pse: {
        region: 'Washington',
        category: 'tou',
        blurb: 'Supports Puget Sound Energy Schedule 7, Schedule 307 (TOU), and Schedule 327 (TOU with Super Off-Peak) for dual morning and evening peaks.'
    },
    portland_general_electric: {
        region: 'Oregon',
        category: 'tou',
        blurb: 'Optimizes Portland General Electric (PGE) Time of Day residential rate plans.'
    },
    we: {
        region: 'Wisconsin',
        category: 'tou',
        blurb: 'Models WE Energies Schedule RG1 and Schedule RG2 Residential Time-of-Use tariffs with customizable peak window hours.'
    },
    mvea: {
        region: 'Colorado',
        category: 'tou',
        blurb: 'Optimizes Mountain View Electric Association Rate 16.01 and Time-of-Day Rate 16.05, automatically protecting against Rate 16.05 zero-export-credit rules.'
    },
    novec: {
        region: 'Virginia',
        category: 'tou',
        blurb: 'Supports Northern Virginia Electric Cooperative (NOVEC) Schedule R-1 and Schedule R-1-EV for overnight electric vehicle and battery charging.'
    },
    rutherford_electric: {
        region: 'North Carolina',
        category: 'tou',
        blurb: 'Models Rutherford Electric Time of Day Service with Super Off-Peak overnight charging and separate generation export credits.'
    },
    centerpoint_indiana: {
        region: 'Indiana',
        category: 'tou',
        blurb: 'Supports CenterPoint Energy Indiana South Rate RS with Excess Distributed Generation (EDG) or standard 1:1 net metering.'
    },
    epb: {
        region: 'Tennessee (Chattanooga)',
        category: 'tou',
        blurb: 'Automates EPB Chattanooga Base Rate, Time Shift, and Night Shift plans.'
    },
    octopus: {
        region: 'United Kingdom',
        category: 'international',
        blurb: 'Full support across all 14 UK DNO regions (A through P) for Octopus Flux (02:00-05:00 cheap import + 16:00-19:00 premium export) and Intelligent Octopus Go.'
    },
    origin: {
        region: 'Australia (NSW • VIC • QLD • SA)',
        category: 'international',
        blurb: 'Supports Origin Battery Maximiser, Battery Starter, Go Solar Variable, and Solar Boost across Australian NEM states.'
    },
    globird: {
        region: 'Australia (VIC • NSW • QLD • SA)',
        category: 'international',
        blurb: 'Optimizes GloBird FOUR4FREE and SOLARPLUS / ZEROHERO plans, charging for $0.00 during the 11 AM-2 PM solar sponge window and exporting during 6-8 PM ZeroHero peaks.'
    },
    engie: {
        region: 'Australia',
        category: 'international',
        blurb: 'Models ENGIE Australia Solar Flat Rate and Solar Time-of-Use tariffs with state-specific feed-in tariffs.'
    }
};

type CategoryFilter = 'all' | 'realtime' | 'california' | 'texas' | 'tou' | 'international';

const SupportedUtilitiesPage: React.FC = () => {
    usePageSEO(PUBLIC_PAGES_SEO.utilities);

    const [utilities, setUtilities] = useState<UtilityProviderInfo[]>([]);
    const [loading, setLoading] = useState(true);
    const [searchQuery, setSearchQuery] = useState('');
    const [activeCategory, setActiveCategory] = useState<CategoryFilter>('all');

    useEffect(() => {
        let cancelled = false;
        fetchUtilities()
            .then((liveUtilities) => {
                if (!cancelled && Array.isArray(liveUtilities)) {
                    const visible = liveUtilities.filter((u) => !u.hidden && u.id !== 'tou_example');
                    setUtilities(visible);
                }
            })
            .catch(() => {
                // Handled gracefully by empty state if backend is unreachable
            })
            .finally(() => {
                if (!cancelled) {
                    setLoading(false);
                }
            });
        return () => {
            cancelled = true;
        };
    }, []);

    const filteredUtilities = useMemo(() => {
        const q = searchQuery.trim().toLowerCase();
        return utilities.filter((u) => {
            if (u.hidden || u.id === 'tou_example') return false;
            const meta = UTILITY_METADATA[u.id];
            const cat = meta?.category || 'tou';
            if (activeCategory !== 'all' && cat !== activeCategory) {
                return false;
            }
            if (!q) return true;
            const matchName = u.name.toLowerCase().includes(q) || u.id.toLowerCase().includes(q);
            const matchMeta =
                (meta?.region.toLowerCase().includes(q) ?? false) ||
                (meta?.blurb.toLowerCase().includes(q) ?? false);
            const matchRates = (u.rates || []).some(
                (r) => r.name.toLowerCase().includes(q) || r.id.toLowerCase().includes(q)
            );
            return matchName || matchMeta || matchRates;
        });
    }, [utilities, searchQuery, activeCategory]);

    const totalRatesCount = useMemo(
        () => utilities.reduce((acc, u) => acc + (u.rates?.length || 0), 0),
        [utilities]
    );

    const utilityCountDisplay = utilities.length > 0 ? `${utilities.length}+` : '50+';
    const rateCountDisplay = totalRatesCount > 0 ? `${totalRatesCount}+` : '120+';

    return (
        <div className="marketing-subpage">
            {/* Hero Banner */}
            <section className="mp-hero">
                <div className="content-container mp-hero-inner">
                    <span className="mp-hero-eyebrow">SUPPORTED UTILITIES &amp; WHOLESALE MARKETS</span>
                    <h1>
                        Built for Real-Time Grids &amp; <span className="highlight">Complex Tariffs</span>
                    </h1>
                    <p>
                        RateRudder natively models <strong>{utilityCountDisplay} utilities</strong> and <strong>{rateCountDisplay} residential rate plans</strong>, from live 5-minute PJM, MISO, and ERCOT wholesale feeds to California NEM 3.0 Avoided Cost export tables and ConnectedSolutions VPP events.
                    </p>
                    <div className="mp-hero-pills">
                        <span className="mp-hero-pill">Official PJM, MISO &amp; ERCOT Feeds</span>
                        <span className="mp-hero-pill">Supply + Delivery Adder Math</span>
                        <span className="mp-hero-pill">NEM 2.0, NEM 3.0 &amp; VPP Ready</span>
                    </div>
                </div>
            </section>

            {/* Section 1: Deep-Dive Spotlights on Notable Market Integrations */}
            <section className="mp-section">
                <div className="content-container">
                    <div className="mp-section-header">
                        <span className="mp-eyebrow">DIRECT MARKET &amp; TARIFF INTEGRATIONS</span>
                        <h2>How RateRudder Models Notable Grids</h2>
                        <p>
                            Most battery apps only support static weekday/weekend timers. RateRudder connects directly to regional transmission organizations (RTOs) and models utility-specific delivery adders, export credits, and VPP rules:
                        </p>
                    </div>

                    <div className="guardrails-grid">
                        <div className="guardrail-card">
                            <span className="guardrail-icon" aria-hidden="true">⚡</span>
                            <h3>ComEd Hourly Pricing (Illinois)</h3>
                            <p>
                                RateRudder combines ComEd&apos;s live <strong>5-minute real-time pricing feed</strong> with official <strong>PJM Day-Ahead hourly rates</strong> so SmartPlan has an accurate 24-hour price curve. Rather than relying blindly on raw PJM forecasts, RateRudder dynamically calibrates upcoming day-ahead rates against live 5-minute price errors—suppressing false-alarm peaks and cutting PJM forecast error. We also add your exact residential delivery class (Single-Family or Multi-Family, with or without Electric Space Heat) and model <strong>1:1 supply export</strong> vs. full net metering.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <span className="guardrail-icon" aria-hidden="true">📊</span>
                            <h3>Ameren Illinois Power Smart Pricing</h3>
                            <p>
                                For Ameren Illinois PSP homeowners, RateRudder fetches official <strong>MISO Day-Ahead market reports</strong> directly as soon as next-day clearing prices are published, pairing them with your seasonal delivery Zone (<code>Zone I, II, or III</code>) and electric space heating riders.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <span className="guardrail-icon" aria-hidden="true">🤠</span>
                            <h3>Texas / ERCOT &amp; Tesla Electric</h3>
                            <p>
                                For <strong>Tesla Electric Dynamic</strong> plans, RateRudder authenticates directly with the <strong>official ERCOT Public API</strong> to pull Real-Time Settlement Point Prices (SPP) and Day-Ahead Market (DAM) prices, automatically layering on regulated PUCT wire charges for <strong>CenterPoint, Oncor, AEP Texas, or TNMP</strong>. Also supports TXU, Green Mountain, and Direct Energy <strong>Free Nights</strong> plans.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <span className="guardrail-icon" aria-hidden="true">☀️</span>
                            <h3>California NEM 2.0 &amp; NEM 3.0 (PG&amp;E, SCE, SDG&amp;E)</h3>
                            <p>
                                Under California&apos;s Net Billing Tariff (NEM 3.0 / SBP), midday solar exports earn pennies while September evening exports can pay several dollars per kWh. RateRudder models the complete monthly and weekday/weekend Avoided Cost Calculator export table plus non-bypassable charges across <code>E-TOU-C</code>, <code>EV2-A</code>, <code>E-ELEC</code>, <code>TOU-D-PRIME</code>, and <code>EV-TOU-5</code>.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <span className="guardrail-icon" aria-hidden="true">🔋</span>
                            <h3>Eversource ConnectedSolutions &amp; VPP (CT, MA, NH)</h3>
                            <p>
                                RateRudder pulls daily Variable Peak Pricing (Rate 7 VPP) rates directly from Eversource Connecticut, models CT Renewable Energy Solutions (RRES) application-year export adjustments (<code>2022-2026</code>), and automatically pre-charges your battery to 100% ahead of summer weekday <strong>ConnectedSolutions Passive Dispatch (5-8 PM)</strong> events.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <span className="guardrail-icon" aria-hidden="true">🌍</span>
                            <h3>Hawai‘i Shift &amp; Save, Duke Schedule PP, UK &amp; Australia</h3>
                            <p>
                                From <strong>Hawaiian Electric&apos;s</strong> <code>ARD TOU R</code> Shift and Save tariff across O‘ahu, Maui, and Hawai‘i Island and <strong>SRP / APS</strong> Arizona solar export plans, to <strong>Duke Energy</strong> PowerPair Schedule PP riders, <strong>Octopus Flux / Intelligent Go (UK)</strong> across all 14 DNO regions, and <strong>Origin / GloBird ZEROHERO (Australia)</strong>.
                            </p>
                        </div>
                    </div>
                </div>
            </section>

            {/* Section 2: Interactive Search & Full Directory of Supported Utilities */}
            <section className="mp-section alt-bg">
                <div className="content-container">
                    <div className="mp-section-header">
                        <span className="mp-eyebrow">COMPLETE TARIFF DIRECTORY</span>
                        <h2>All Supported Utilities &amp; Rate Plans</h2>
                        <p>
                            Every utility below is pre-configured in RateRudder. Search by utility name, state, or rate code to inspect supported plans:
                        </p>
                    </div>

                    {/* Search & Category Filter Controls */}
                    <div className="utilities-filter-bar">
                        <div className="utilities-search-input-wrap">
                            <span className="utilities-search-icon" aria-hidden="true">🔍</span>
                            <input
                                type="search"
                                className="utilities-search-input"
                                placeholder="Search utility, state, or rate plan (e.g. ComEd, Texas, E-TOU-C, EV2-A, Octopus)..."
                                value={searchQuery}
                                onChange={(e) => setSearchQuery(e.target.value)}
                                aria-label="Search supported utilities and rate plans"
                            />
                            {searchQuery && (
                                <button
                                    type="button"
                                    className="utilities-search-clear"
                                    onClick={() => setSearchQuery('')}
                                    aria-label="Clear search"
                                >
                                    ✕
                                </button>
                            )}
                        </div>

                        <div className="utilities-category-pills" role="tablist" aria-label="Filter utilities by category">
                            {([
                                { key: 'all', label: utilities.length > 0 ? `All (${utilities.length})` : 'All' },
                                { key: 'realtime', label: 'Wholesale & VPP' },
                                { key: 'texas', label: 'Texas / ERCOT' },
                                { key: 'california', label: 'California NEM 2/3' },
                                { key: 'tou', label: 'US Time-of-Use & EV' },
                                { key: 'international', label: 'UK & Australia' }
                            ] as { key: CategoryFilter; label: string }[]).map((tab) => (
                                <button
                                    key={tab.key}
                                    type="button"
                                    role="tab"
                                    aria-selected={activeCategory === tab.key}
                                    className={`utilities-cat-pill ${activeCategory === tab.key ? 'active' : ''}`}
                                    onClick={() => setActiveCategory(tab.key)}
                                >
                                    {tab.label}
                                </button>
                            ))}
                        </div>
                    </div>

                    {/* Directory Grid */}
                    {loading && utilities.length === 0 ? (
                        <div className="utilities-empty-state">
                            <p>Loading supported utilities...</p>
                        </div>
                    ) : filteredUtilities.length === 0 ? (
                        <div className="utilities-empty-state">
                            <h3>No matching utilities or rate plans found</h3>
                            <p>
                                Don&apos;t see your electric utility or Time-of-Use tariff yet? Submit a quick request and we&apos;ll email you in a few days once your utility is supported.
                            </p>
                            <div className="utilities-empty-actions">
                                <Link to="/welcome?requestUtility=1" className="utilities-request-btn">
                                    Request Your Utility →
                                </Link>
                                <button
                                    type="button"
                                    className="utilities-reset-btn"
                                    onClick={() => {
                                        setSearchQuery('');
                                        setActiveCategory('all');
                                    }}
                                >
                                    Show All Utilities
                                </button>
                            </div>
                        </div>
                    ) : (
                        <div className="utilities-directory-grid">
                            {filteredUtilities.map((u) => {
                                const meta = UTILITY_METADATA[u.id] || {
                                    region: 'United States',
                                    category: 'tou',
                                    blurb: `Pre-configured residential and Time-of-Use tariff schedules for ${u.name}.`
                                };
                                return (
                                    <article key={u.id} className="utility-directory-card">
                                        <div className="utility-card-top">
                                            <span className="utility-region-tag">{meta.region}</span>
                                        </div>
                                        <h3 className="utility-card-title">{u.name}</h3>
                                        <p className="utility-card-blurb">{meta.blurb}</p>
                                        <div className="utility-rates-block">
                                            <span className="utility-rates-label">
                                                Supported Plans ({u.rates?.length || 0})
                                            </span>
                                            <div className="utility-rate-pills">
                                                {(u.rates || []).map((r) => (
                                                    <span key={r.id} className="utility-rate-pill">
                                                        {r.name}
                                                    </span>
                                                ))}
                                            </div>
                                        </div>
                                    </article>
                                );
                            })}
                        </div>
                    )}

                    {/* Missing Utility Request Banner */}
                    <div className="utilities-missing-banner">
                        <div className="utilities-missing-copy">
                            <h3>Don&apos;t See Your Utility or Rate Plan?</h3>
                            <p>
                                We add new electric utilities and Time-of-Use schedules every week. Let us know which utility or rate plan you use and we&apos;ll shoot you an email in a few days when it&apos;s ready.
                            </p>
                        </div>
                        <Link to="/welcome?requestUtility=1" className="utilities-request-btn">
                            Request Your Utility →
                        </Link>
                    </div>

                    {/* Closing CTA */}
                    <div className="mp-bottom-cta" style={{ marginTop: '2.5rem' }}>
                        <h2>See Your Utility Plan Optimized Live</h2>
                        <p>
                            Sign in with Google or Apple, pick your utility from the dropdown, and preview your 24-hour SmartPlan in under two minutes.
                        </p>
                        <div className="mp-cta-actions">
                            <Link to="/login" className="mp-btn-primary">
                                Get Started for Free
                            </Link>
                            <Link to="/how-it-works" className="mp-btn-secondary">
                                How SmartPlan Works →
                            </Link>
                        </div>
                    </div>
                </div>
            </section>
        </div>
    );
};

export default SupportedUtilitiesPage;
