import React from 'react';
import { Link } from 'wouter';
import { usePageSEO, PUBLIC_PAGES_SEO } from '../utils/seo';
import './MarketingPages.css';
import './Policies.css';

const TermsOfService: React.FC = () => {
    usePageSEO(PUBLIC_PAGES_SEO.terms);
    return (
        <div className="marketing-subpage">
            <section className="mp-hero">
                <div className="content-container mp-hero-inner">
                    <span className="mp-hero-eyebrow">LEGAL &amp; TERMS</span>
                    <h1>Terms of Service</h1>
                    <p>Last Updated: November 12, 2026</p>
                </div>
            </section>

            <section className="mp-section">
                <div className="content-container">
                    <div className="policy-container">
                        <p>
                            Welcome to RateRudder! These Terms of Service (&quot;Terms&quot;) are a legal agreement between you and <strong>RateRudder</strong> (&quot;RateRudder,&quot; &quot;we,&quot; &quot;us,&quot; or &quot;our,&quot; meaning the operator providing this hosted RateRudder Service) covering your use of the RateRudder website, APIs, and automated home energy management service (the &quot;Service&quot;). We wrote these Terms in plain English so you clearly know how the Service works and where responsibilities lie.
                        </p>
                        <p>
                            By signing in, creating or joining a site, or using RateRudder, you agree to these Terms and our <Link to="/privacy">Privacy Policy</Link>. If you don&apos;t agree, please don&apos;t use the Service.
                        </p>

                        <h2>1. Who Can Use RateRudder</h2>
                        <p>
                            You must be at least 18 years old, legally able to agree to these Terms, and either own the home and energy equipment you connect, or have explicit permission from the owner to let RateRudder monitor and manage it.
                        </p>

                        <h2>2. What You Authorize RateRudder to Do</h2>
                        <p>
                            When you connect a battery or solar system, you authorize RateRudder to connect to your hardware provider&apos;s cloud API on your behalf and continuously read your system&apos;s energy and status data (including while automation is paused). Whenever automation is not paused, you also authorize RateRudder to automatically change operating modes, Time-of-Use schedules, charging behavior, export settings, and backup reserve levels.
                        </p>
                        <p>
                            You are always in the driver&apos;s seat: you can <strong>pause automation</strong> or <strong>delete your site</strong> in RateRudder Settings at any time.
                        </p>

                        <h2>3. Important Safety, Hardware &amp; Utility Disclaimers</h2>
                        <p>
                            Please read this section carefully—managing real home batteries involves real-world trade-offs:
                        </p>
                        <ul>
                            <li>
                                <strong>Do Not Use With Life-Support or Critical Equipment:</strong> RateRudder is <strong>not</strong> designed for homes that rely on powered medical devices or life-support equipment.
                            </li>
                            <li>
                                <strong>No Backup Power Guarantee:</strong> RateRudder checks and updates sites periodically over the internet. Internet outages, cloud downtime, weather or load forecast errors, utility Virtual Power Plant (VPP) events, or manufacturer API failures can leave your battery at a lower charge level than expected—even below your normal reserve—when a storm or power outage hits.
                            </li>
                            <li>
                                <strong>Independent Project &amp; Manufacturer Rules:</strong> RateRudder is an independent project and is <strong>not affiliated with, endorsed by, or sponsored by Tesla, FranklinWH, Enphase, or any electric utility</strong>. Some integrations rely on account credentials or cloud interfaces that manufacturers can rate-limit, change, or block at any time without notice. Automated charging and discharging also causes normal battery wear and cycling. You connect your hardware at your own risk, and we aren&apos;t responsible for locked accounts, broken integrations, battery degradation, or any manufacturer warranty or incentive disputes.
                            </li>
                            <li>
                                <strong>Your Utility &amp; Interconnection Rules:</strong> Utilities and tax-credit programs often have strict rules about whether you are allowed to charge your battery from the grid or export battery energy back to the grid. <strong>You are solely responsible</strong> for making sure your RateRudder settings (especially custom grid-charging and grid-export settings) and rate plan selections comply with your utility tariff, interconnection agreement, and local laws.
                            </li>
                            <li>
                                <strong>Savings &amp; Rates Are Estimates Only:</strong> Electricity rates, solar/usage forecasts, and &quot;Estimated Savings&quot; shown in RateRudder are best-effort simulations for informational purposes. Actual utility bills depend on meter readings, taxes, riders, and delayed wholesale settlements, and we never guarantee financial savings or that RateRudder won&apos;t occasionally increase your electricity cost during unexpected conditions.
                            </li>
                        </ul>

                        <h2>4. Shared Sites, Connected Apps &amp; Acceptable Use</h2>
                        <ul>
                            <li>
                                <strong>Shared Sites:</strong> Anyone you give your Site ID and Invite Code to will have <strong>full co-owner access</strong> to that site—including viewing history, changing battery settings, and deleting the site. Only invite people you trust.
                            </li>
                            <li>
                                <strong>Connected Apps &amp; API Keys:</strong> If you generate API keys, share summary URLs, or approve external apps, keep those tokens secret. You are responsible for any third-party app or tool you connect until you revoke its access in Settings.
                            </li>
                            <li>
                                <strong>Acceptable Use:</strong> Use RateRudder only to manage legitimate residential energy systems. Don&apos;t probe or bypass our security or rate limits, interfere with other users, or misuse our APIs or third-party integrations.
                            </li>
                        </ul>

                        <h2>5. Free Beta &amp; Service Changes</h2>
                        <p>
                            RateRudder is currently offered as a <strong>free public beta</strong>. Because it is a beta project, we may change, limit, pause, or shut down features, supported utilities, or hardware integrations at any time. We will never surprise-charge you—if we ever introduce paid plans in the future, we will give you clear advance notice and you will only be billed if you choose to opt in.
                        </p>

                        <h2>6. Open-Source Code vs. Hosted Service</h2>
                        <p>
                            RateRudder&apos;s source code is licensed under the <strong>GNU General Public License v3.0 (GPLv3)</strong> on GitHub, and nothing in these Terms limits your freedoms under the GPLv3 for the code itself. However, the <strong>RateRudder</strong> name, logo, and branding belong to us, and access to our hosted cloud Service, configurations, and databases isn&apos;t transferred under the open-source license.
                        </p>

                        <h2>7. &quot;As-Is&quot; Service (No Warranties)</h2>
                        <p>
                            <strong>RateRudder is provided &quot;AS IS&quot; and &quot;AS AVAILABLE,&quot; without warranties of any kind—express or implied.</strong> To the fullest extent allowed by law, we disclaim all implied warranties (including merchantability, fitness for a particular purpose, and non-infringement). We don&apos;t promise that RateRudder will be uninterrupted, bug-free, compatible with your hardware forever, or save you money.
                        </p>

                        <h2>8. Limitation of Liability</h2>
                        <p>
                            To the fullest extent allowed by law:
                        </p>
                        <ul>
                            <li>
                                <strong>No Indirect or Consequential Damages:</strong> RateRudder, along with its operator, owner, creator, developers, affiliates, and open-source contributors (the &quot;RateRudder Parties&quot;), will <strong>not</strong> be liable for any indirect, incidental, special, consequential, or punitive damages. That includes higher electric bills, missed export credits or VPP payouts, battery wear or hardware issues, warranty disputes, power loss during an outage, spoiled food, property damage, or lost data arising from your use of (or inability to use) the Service.
                            </li>
                            <li>
                                <strong>Cap on Total Liability:</strong> For any claim where liability cannot legally be excluded, the <strong>total combined liability of the RateRudder Parties for all claims is capped at the greater of (a) the total amount you paid us for the Service in the 12 months before the claim arose, or (b) $100 USD.</strong>
                            </li>
                        </ul>
                        <p>
                            Some jurisdictions don&apos;t allow certain warranty disclaimers or liability caps, so parts of Sections 7 and 8 may not apply to you to that extent.
                        </p>

                        <h2>9. Indemnification (Covering Claims Caused by Misuse)</h2>
                        <p>
                            If someone (like a utility, hardware manufacturer, landlord, or co-resident) brings a claim against the RateRudder Parties because you (a) connected equipment without permission, (b) violated your utility interconnection agreement or third-party terms, or (c) broke these Terms or applicable law, you agree to cover and hold the RateRudder Parties harmless from resulting losses, liabilities, and reasonable legal fees.
                        </p>

                        <h2>10. Ending Your Use or Ours</h2>
                        <p>
                            You can stop using RateRudder at any time by pausing automation and deleting your site and account in Settings (remember to also double-check your battery&apos;s operating mode in your manufacturer&apos;s app after disconnecting!). We may also suspend or end access to the Service at any time—for example, if these Terms are violated, an integration is no longer sustainable, or the beta ends. Sections 3, 6, 7, 8, 9, and 11 continue to apply even after your account ends.
                        </p>

                        <h2>11. Resolving Disputes &amp; Governing Law</h2>
                        <ul>
                            <li>
                                <strong>Talk to Us First:</strong> Most concerns can be fixed quickly without lawyers. Before filing any formal claim, you agree to email <a href="mailto:legal@raterudder.com">legal@raterudder.com</a> with a description of the issue and give us 30 days to try to resolve it informally.
                            </li>
                            <li>
                                <strong>Governing Law &amp; Courts:</strong> These Terms are governed by the laws of the <strong>State of Illinois</strong>, USA, without regard to conflict-of-law rules. Any formal dispute that isn&apos;t eligible for small-claims court must be brought exclusively in the state or federal courts located in <strong>Illinois</strong>, and both you and RateRudder consent to jurisdiction there.
                            </li>
                        </ul>

                        <h2>12. Updates to These Terms</h2>
                        <p>
                            We may update these Terms as RateRudder evolves. When we make material changes, we will update the &quot;Last Updated&quot; date above and notify active users via email or an in-app prompt. If you keep using RateRudder after the updated Terms take effect (or click to accept them in the app), you agree to the updated Terms; if you don&apos;t agree, simply pause automation and delete your site in Settings or contact us for deletion.
                        </p>

                        <h2>13. General</h2>
                        <p>
                            If a court finds part of these Terms unenforceable, that part will be narrowed only as much as needed so the rest stays in effect. Our failure to enforce a rule right away doesn&apos;t waive our right to enforce it later. Together with our Privacy Policy, these Terms are the entire agreement between you and RateRudder for the Service (and automatically apply to any successor company or entity we form to operate RateRudder).
                        </p>

                        <h2>14. Contact Us</h2>
                        <p>
                            Questions? Reach out anytime at <a href="mailto:legal@raterudder.com">legal@raterudder.com</a>.
                        </p>
                    </div>
                </div>
            </section>
        </div>
    );
};

export default TermsOfService;
