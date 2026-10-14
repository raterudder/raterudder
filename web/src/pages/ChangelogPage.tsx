import React from 'react';
import { Link } from 'wouter';
import { usePageSEO, PUBLIC_PAGES_SEO } from '../utils/seo';
import './MarketingPages.css';

interface ChangelogEntry {
    category: 'engine' | 'forecasting' | 'ux' | 'integrations';
    categoryLabel: string;
    title: string;
    description: string;
}

interface ChangelogMonth {
    month: string;
    badge: string;
    entries: ChangelogEntry[];
}

const changelogMonths: ChangelogMonth[] = [
    {
        month: "October 2026",
        badge: "Latest Release • SmartPlan & Counterfactual Savings",
        entries: [
            {
                category: "ux",
                categoryLabel: "Dashboard & Savings",
                title: "Side-by-Side Counterfactual Savings ('With vs. Without RateRudder')",
                description: "Redesigned the Dashboard Savings Hero to calculate and display your counterfactual savings, showing what your bill would have been on default Self-Consumption across Today, the Last 7 Days, and the Last 30 Days, alongside collapsible 12-hour action timeline rollups."
            },
            {
                category: "forecasting",
                categoryLabel: "Load Forecasting",
                title: "Automatic EV Charging Detection & Weekend Morning Adaptation",
                description: "Removed manual EV charging schedules in favor of automatic high-draw EV load spike detection. Added outlier morning detection so weekend and holiday household routines do not skew weekday breakfast peak forecasts."
            },
            {
                category: "forecasting",
                categoryLabel: "Solar Forecasting",
                title: "Zero-Config Solar Calibration & Inverter DC-Curtailment Detection",
                description: "Improved solar forecasting with adjacent-hour clear-sky sampling, immediate-period cloud damping, and automatic detection of inverter DC clipping so midday solar potential is accurately modeled."
            },
            {
                category: "integrations",
                categoryLabel: "Utilities & Hardware",
                title: "Texas / ERCOT, Octopus Energy & FranklinWH TOU Schedule Optimizations",
                description: "Added support for Texas (ERCOT) retail and wholesale tariffs and Octopus Energy dynamic rates, sub-hour ComEd pricing history, and streamlined FranklinWH Time-of-Use schedule updates to reduce mode-switch delays."
            }
        ]
    },
    {
        month: "September 2026",
        badge: "Sub-Hour Dynamic Programming & Smart Alerts",
        entries: [
            {
                category: "engine",
                categoryLabel: "SmartPlan Engine",
                title: "Sub-Hour Horizon Dynamic Programming Planner ('Plan Mode')",
                description: "Launched the multi-step dynamic programming planning engine that subdivides 24-hour utility and weather horizons into sub-hour intervals, replacing greedy single-hour rules with full-horizon trajectory optimization."
            },
            {
                category: "ux",
                categoryLabel: "Forecast & Notifications",
                title: "Multi-Color Forecast Trajectory Graph & Proactive Web Push Notifications",
                description: "Introduced the color-coded 24-hour Forecast & Plan visualization (Grid Charge, Solar Charge, Powering Home, Solar Export, Grid Export, Standby) and added Web Push / PWA notifications for real-time price spikes, grid outages, abnormal load, and daily summaries with customizable quiet hours."
            },
            {
                category: "engine",
                categoryLabel: "Grid Events & VPP",
                title: "Automated Virtual Power Plant (VPP) Event Pre-Charging",
                description: "Added automated pre-charge preparation ahead of scheduled utility Virtual Power Plant (VPP) dispatch events so your battery reaches full capacity before high-value grid export windows begin."
            }
        ]
    },
    {
        month: "August 2026",
        badge: "Time-Based Reserves, Vacation Detection & Load Forecasting",
        entries: [
            {
                category: "engine",
                categoryLabel: "Backup & Reserves",
                title: "Time-Windowed Variable Backup Reserves & Auto-Utility Setup",
                description: "Released time-windowed variable backup reserves, allowing homeowners to lower their reserve floor during expensive evening peaks while automatically restoring a higher storm reserve overnight, alongside automatic utility detection from connected battery telemetry."
            },
            {
                category: "forecasting",
                categoryLabel: "Load Forecasting",
                title: "Automatic Vacation Detection, Same-Day Load Weighting & EV Standby Protection",
                description: "Added automatic vacation detection so extended away periods do not distort normal daily load forecasts. Improved household load predictions by weighting matching days of the week higher and detecting EV charging spikes to hold the home battery in Standby rather than draining into the vehicle."
            }
        ]
    }
];

const ChangelogPage: React.FC = () => {
    usePageSEO(PUBLIC_PAGES_SEO.changelog);
    return (
        <div className="marketing-subpage">
            {/* Hero Banner */}
            <section className="mp-hero">
                <div className="content-container mp-hero-inner">
                    <span className="mp-hero-eyebrow">PRODUCT UPDATES &amp; RELEASE NOTES</span>
                    <h1>
                        What&apos;s New in <span className="highlight">RateRudder</span>
                    </h1>
                    <p>
                        RateRudder is actively developed in the open. Here is a monthly summary of recent upgrades to our <strong>SmartPlan</strong> optimization engine, solar and load forecasting, and supported utilities.
                    </p>
                    <div className="mp-hero-pills">
                        <span className="mp-hero-pill">Open Source on GitHub</span>
                        <span className="mp-hero-pill">Continuous Cloud Updates</span>
                        <span className="mp-hero-pill">Automatic Rollouts</span>
                    </div>
                </div>
            </section>

            {/* Monthly Grouped Changelog Timeline */}
            <section className="mp-section">
                <div className="content-container">
                    <div className="changelog-timeline">
                        {changelogMonths.map((group) => (
                            <article key={group.month} className="changelog-month-group">
                                <div className="changelog-month-header">
                                    <h2 className="changelog-month-title">{group.month}</h2>
                                    <span className="changelog-month-badge">{group.badge}</span>
                                </div>

                                <div className="changelog-entries">
                                    {group.entries.map((entry, idx) => (
                                        <div key={idx} className="changelog-entry">
                                            <div className="changelog-entry-top">
                                                <span className={`changelog-tag ${entry.category}`}>
                                                    {entry.categoryLabel}
                                                </span>
                                            </div>
                                            <h3>{entry.title}</h3>
                                            <p>{entry.description}</p>
                                        </div>
                                    ))}
                                </div>
                            </article>
                        ))}
                    </div>

                    <div className="changelog-archive-note">
                        <p>
                            Looking for earlier releases? RateRudder has been in continuous development well before the recent monthly highlights shown above. You can browse the complete commit and release history anytime on{' '}
                            <a href="https://github.com/raterudder/raterudder" target="_blank" rel="noopener noreferrer">
                                GitHub →
                            </a>
                        </p>
                    </div>
                </div>
            </section>

            {/* Bottom Call to Action */}
            <section className="mp-section alt-bg">
                <div className="content-container">
                    <div className="mp-bottom-cta">
                        <h2>Experience the Latest SmartPlan</h2>
                        <p>
                            Every improvement above runs automatically in the cloud for all connected Tesla Powerwall, FranklinWH, and Enphase systems.
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

export default ChangelogPage;
