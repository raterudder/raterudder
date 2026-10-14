import React from 'react';
import { Link } from 'wouter';
import { usePageSEO, PUBLIC_PAGES_SEO } from '../utils/seo';
import './MarketingPages.css';

const GettingStartedPage: React.FC = () => {
    usePageSEO(PUBLIC_PAGES_SEO['getting-started']);
    return (
        <div className="marketing-subpage">
            {/* Hero Banner */}
            <section className="mp-hero">
                <div className="content-container mp-hero-inner">
                    <span className="mp-hero-eyebrow">2-MINUTE CLOUD SETUP</span>
                    <h1>
                        Up and Running in <span className="highlight">Under Two Minutes</span>
                    </h1>
                    <p>
                        No Raspberry Pi, no extra CT clamps in your breaker panel, and no YAML files to maintain. RateRudder connects directly to your existing solar and battery system through its official cloud API in four simple steps.
                    </p>
                    <div className="mp-hero-pills">
                        <span className="mp-hero-pill">1. Sign In (15s)</span>
                        <span className="mp-hero-pill">2. Pick Utility (30s)</span>
                        <span className="mp-hero-pill">3. Link Battery (45s)</span>
                        <span className="mp-hero-pill">4. Automate &amp; Relax (30s)</span>
                    </div>
                </div>
            </section>

            {/* Step-by-Step Visual Walkthrough */}
            <section className="mp-section">
                <div className="content-container">
                    <div className="gs-steps-list">
                        {/* Step 1 */}
                        <div className="gs-step-card">
                            <div className="gs-step-copy">
                                <div className="gs-step-meta">
                                    <span className="gs-step-number">Step 1</span>
                                    <span className="gs-step-duration">~15 Seconds</span>
                                </div>
                                <h2>Sign In with Google or Apple</h2>
                                <p>
                                    Create your RateRudder account in one click using your existing Google or Apple login. There are no new passwords to manage, no credit card required, and no third-party ad trackers.
                                </p>
                                <ul className="gs-checklist">
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Multi-Site Ready:</strong> Manage a primary home and a second property from a single account.</span>
                                    </li>
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Household Sharing:</strong> Invite family members to view savings and receive outage notifications.</span>
                                    </li>
                                </ul>
                            </div>

                            <div className="gs-ui-preview" aria-hidden="true">
                                <div className="gs-ui-topbar">
                                    <div className="gs-ui-dots">
                                        <span /><span /><span />
                                    </div>
                                    <span className="gs-ui-url">raterudder.com/login</span>
                                </div>
                                <div className="gs-mock-auth-box">
                                    <div style={{ fontWeight: 800, fontSize: '1.25rem', color: '#f8fafc' }}>
                                        RateRudder
                                    </div>
                                    <div style={{ fontSize: '0.8rem', color: '#94a3b8', marginBottom: '0.25rem' }}>
                                        To get started, sign in below.
                                    </div>
                                    <div className="gs-mock-oauth-btn google">
                                        Sign in with Google
                                    </div>
                                    <div className="gs-mock-oauth-btn apple">
                                        Sign in with Apple
                                    </div>
                                </div>
                            </div>
                        </div>

                        {/* Step 2 */}
                        <div className="gs-step-card">
                            <div className="gs-step-copy">
                                <div className="gs-step-meta">
                                    <span className="gs-step-number">Step 2</span>
                                    <span className="gs-step-duration">~30 Seconds</span>
                                </div>
                                <h2>Pick Your Utility &amp; Rate Plan</h2>
                                <p>
                                    Select your electric utility and rate schedule from our <Link to="/utilities" style={{ color: '#2563eb', fontWeight: 600 }}>supported utilities directory</Link>. RateRudder pulls live wholesale hourly prices, day-ahead market curves, and Time-of-Use schedules, and combines supply rates with your delivery fees for accurate math.
                                </p>
                                <ul className="gs-checklist">
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Real-Time &amp; Wholesale Tariffs:</strong> ComEd Hourly Pricing (BESH), Ameren Power Smart Pricing, Texas ERCOT (Tesla Electric Dynamic), and Eversource VPP.</span>
                                    </li>
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Time-of-Use &amp; Net Billing:</strong> PG&amp;E, SCE, SDG&amp;E, Xcel, Duke Energy, SRP, APS, Hawaiian Electric, Octopus Energy, and many more.</span>
                                    </li>
                                </ul>
                            </div>

                            <div className="gs-ui-preview" aria-hidden="true">
                                <div className="gs-ui-topbar">
                                    <div className="gs-ui-dots">
                                        <span /><span /><span />
                                    </div>
                                    <span className="gs-ui-url">Settings • Utility Service</span>
                                </div>
                                <div className="gs-mock-form-row">
                                    <span className="gs-mock-label">Utility Provider</span>
                                    <div className="gs-mock-select">
                                        <span>Commonwealth Edison (ComEd)</span>
                                        <span style={{ color: '#94a3b8' }}>▾</span>
                                    </div>
                                </div>
                                <div className="gs-mock-form-row">
                                    <span className="gs-mock-label">Rate Plan</span>
                                    <div className="gs-mock-select">
                                        <span>Hourly Pricing Program (BESH)</span>
                                        <span style={{ color: '#94a3b8' }}>▾</span>
                                    </div>
                                </div>
                                <div className="gs-mock-form-row">
                                    <span className="gs-mock-label">Rate Class</span>
                                    <div className="gs-mock-select">
                                        <span>Residential Single Family Without Electric Space Heat</span>
                                        <span style={{ color: '#94a3b8' }}>▾</span>
                                    </div>
                                </div>
                            </div>
                        </div>

                        {/* Step 3 */}
                        <div className="gs-step-card">
                            <div className="gs-step-copy">
                                <div className="gs-step-meta">
                                    <span className="gs-step-number">Step 3</span>
                                    <span className="gs-step-duration">~45 Seconds</span>
                                </div>
                                <h2>Link Your Battery System</h2>
                                <p>
                                    Choose your battery brand and sign in to link your system. RateRudder uses the same official operating modes available in your manufacturer mobile app, so <strong>your factory warranty and built-in hardware protections stay completely intact</strong>.
                                </p>
                                <ul className="gs-checklist">
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Tesla Powerwall (2, +, &amp; 3):</strong> Sign in with Tesla OAuth to link your Powerwall site and authorize energy commands.</span>
                                    </li>
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>FranklinWH (aGate &amp; aPower):</strong> Connect with your FranklinWH account credentials and select your gateway.</span>
                                    </li>
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Enphase IQ Battery:</strong> Link your Enphase Enlighten account for automated tariff dispatch.</span>
                                    </li>
                                </ul>
                            </div>

                            <div className="gs-ui-preview" aria-hidden="true">
                                <div className="gs-ui-topbar">
                                    <div className="gs-ui-dots">
                                        <span /><span /><span />
                                    </div>
                                    <span className="gs-ui-url">Settings • Energy Storage System</span>
                                </div>
                                <div className="gs-mock-form-row">
                                    <span className="gs-mock-label">System Type</span>
                                    <div className="gs-mock-select">
                                        <span>Tesla Powerwall</span>
                                        <span style={{ color: '#94a3b8' }}>▾</span>
                                    </div>
                                </div>
                                <div className="gs-mock-form-row" style={{ marginTop: '0.25rem' }}>
                                    <div className="gs-mock-tesla-login-btn">
                                        Login with Tesla
                                    </div>
                                </div>
                            </div>
                        </div>

                        {/* Step 4 */}
                        <div className="gs-step-card">
                            <div className="gs-step-copy">
                                <div className="gs-step-meta">
                                    <span className="gs-step-number">Step 4</span>
                                    <span className="gs-step-duration">~30 Seconds</span>
                                </div>
                                <h2>Customize Reserves, Alerts &amp; Relax</h2>
                                <p>
                                    Once you save your settings, RateRudder builds your 24-hour <strong>SmartPlan</strong> automatically. You can leave everything on default autopilot, or customize time-based backup reserves and optional push notifications.
                                </p>
                                <ul className="gs-checklist">
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Time-Based Backup Reserves:</strong> Keep a higher storm reserve overnight, then automatically lower your reserve floor during peak hours to unlock extra battery capacity.</span>
                                    </li>
                                    <li>
                                        <span className="gs-check-badge">✓</span>
                                        <span><strong>Proactive Smart Alerts:</strong> Optional push notifications for morning solar and battery outlooks, price spikes, and grid outages.</span>
                                    </li>
                                </ul>
                            </div>

                            <div className="gs-ui-preview" aria-hidden="true">
                                <div className="gs-ui-topbar">
                                    <div className="gs-ui-dots">
                                        <span /><span /><span />
                                    </div>
                                    <span className="gs-ui-url">Settings • Battery &amp; Notifications</span>
                                </div>
                                <div className="gs-mock-form-row">
                                    <span className="gs-mock-label">Backup Reserve Schedule</span>
                                    <div className="gs-mock-select">
                                        <span>20% Overnight • 5% Peak Window (5:00 PM to 9:00 PM)</span>
                                        <span style={{ color: '#38bdf8', fontSize: '0.72rem', fontWeight: 700 }}>Time-based</span>
                                    </div>
                                </div>
                                <div className="gs-mock-form-row">
                                    <span className="gs-mock-label">Push Notifications</span>
                                    <div className="gs-mock-select">
                                        <span>Morning Summary, Price Spikes &amp; Grid Outage Alerts</span>
                                        <span style={{ color: '#34d399', fontSize: '0.72rem', fontWeight: 700 }}>Enabled</span>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* Bottom Call to Action */}
            <section className="mp-section alt-bg">
                <div className="content-container">
                    <div className="mp-bottom-cta">
                        <h2>Ready to Automate Your Battery?</h2>
                        <p>
                            Join homeowners saving over 60% more than default Self-Consumption, completely free during our public beta.
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

export default GettingStartedPage;
