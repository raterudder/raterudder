import React from 'react';
import { usePageSEO, PUBLIC_PAGES_SEO } from '../utils/seo';
import './MarketingPages.css';
import './Policies.css';

const PrivacyPolicy: React.FC = () => {
    usePageSEO(PUBLIC_PAGES_SEO.privacy);
    return (
        <div className="marketing-subpage">
            <section className="mp-hero">
                <div className="content-container mp-hero-inner">
                    <span className="mp-hero-eyebrow">LEGAL &amp; PRIVACY</span>
                    <h1>Privacy Policy</h1>
                    <p>Last Updated: November 12, 2026</p>
                </div>
            </section>

            <section className="mp-section">
                <div className="content-container">
                    <div className="policy-container">
                        <p>
                            RateRudder is built to lower your electric bill without selling you out. This policy explains in plain English what information we collect, why we need it, how we protect it, and how you can delete it.
                        </p>

                        <h2>1. What We Collect</h2>
                        <p>
                            We only collect what we need to run, secure, and improve RateRudder:
                        </p>
                        <ul>
                            <li>
                                <strong>Account &amp; Site Info:</strong> When you sign in with Google or Apple, we receive your email address and a unique account ID from your sign-in provider (we never see or store your Google or Apple password). We also store the site names, utility and rate plan settings, and preferences you configure, along with when you accepted our Terms.
                            </li>
                            <li>
                                <strong>Battery System Credentials:</strong> To monitor your system and adjust modes on your behalf, we store connection credentials for your Energy Storage System, such as OAuth tokens, site ID, and serial number, or your encrypted account login, session tokens, and gateway/system ID depending on your hardware provider.
                            </li>
                            <li>
                                <strong>Home Energy &amp; System Data:</strong> Battery state of charge, capacity, power flows (solar generation, home usage, battery charge/discharge, grid import/export), operating modes, hardware alarms, storm alerts, Virtual Power Plant (VPP) events, and the schedule or mode updates RateRudder makes. We continue reading energy and system data while your system is connected, even when automation is paused, so your dashboard, history, and forecasts stay up to date.
                            </li>
                            <li>
                                <strong>Optional Location Data:</strong> Any location details you optionally choose to enter (such as country, postal code, or more specific location details if you opt in), along with resolved coordinates, city, elevation, and time zone used to generate local solar and weather forecasts. Providing location data is optional, and we only collect location details that you explicitly provide.
                            </li>
                            <li>
                                <strong>Information You Choose to Submit:</strong> Any contact details, utility/hardware requests, comments, or preferences you voluntarily submit through forms, waitlists, surveys, or in-app feedback tools (along with basic context like the current page path or browser type when submitting feedback).
                            </li>
                            <li>
                                <strong>Feature Tokens &amp; Technical Identifiers:</strong> If you enable optional features or integrations (such as notifications, API access, or connected apps), we store the non-identifying device or browser tokens, subscription endpoints, cryptographic keys, or hashed credentials needed to make those features work, along with basic delivery and interaction logs to verify reliability.
                            </li>
                            <li>
                                <strong>Security &amp; Diagnostic Logs:</strong> Like most web services, our servers log basic request details, such as IP address, browser user-agent, and automatic browser error reports—to prevent abuse and fix bugs.
                            </li>
                        </ul>

                        <h2>2. How We Use Your Data</h2>
                        <ul>
                            <li>
                                <strong>Running RateRudder:</strong> Checking utility rates and local weather, forecasting your home&apos;s solar production and electricity usage, monitoring your energy history, periodically adjusting your battery and solar modes when automation is active, and calculating your estimated savings.
                            </li>
                            <li>
                                <strong>Optional Features &amp; Communications:</strong> Operating optional features and integrations you turn on, responding to requests or feedback you submit, and sending important account, security, or policy updates.
                            </li>
                            <li>
                                <strong>Improving RateRudder:</strong> Analyzing real-world system behavior, forecast accuracy, and edge cases so we can make our scheduling algorithms smarter and more reliable.
                            </li>
                            <li>
                                <strong>Security &amp; Reliability:</strong> Debugging issues, preventing unauthorized access, and rate-limiting abusive traffic.
                            </li>
                        </ul>

                        <h2>3. Cookies &amp; Local Storage</h2>
                        <p>
                            RateRudder uses <strong>one essential login cookie</strong> (<code>session_token</code>) to keep you signed in, plus basic browser <code>localStorage</code> for UI preferences. <strong>We do not use advertising trackers, third-party analytics cookies, or tracking pixels.</strong> Our hosting and sign-in providers (such as Cloudflare, Google Cloud, Google Sign-In, and Apple Sign-In) may use strictly necessary cookies or local storage for security, bot protection, and authentication.
                        </p>

                        <h2>4. Who We Share Data With</h2>
                        <p>
                            <strong>We never sell your personal data.</strong> We only share data with the services required to operate RateRudder or integrations you choose to use:
                        </p>
                        <ul>
                            <li>
                                <strong>Infrastructure, Security &amp; Sign-In Providers:</strong> Cloud hosting, database, network security, font, and authentication providers (such as <strong>Google Cloud Platform</strong>, <strong>Cloudflare</strong>, <strong>Google</strong>, and <strong>Apple</strong>).
                            </li>
                            <li>
                                <strong>Battery &amp; Energy Hardware Providers:</strong> Your connected hardware provider&apos;s cloud API (such as <strong>Tesla</strong>, <strong>FranklinWH</strong>, or <strong>Enphase</strong>) using your stored credentials/tokens to read system data and update operating modes.
                            </li>
                            <li>
                                <strong>Weather &amp; Location Services:</strong> Weather and geocoding providers (such as <strong>Open-Meteo</strong> and <strong>Google Maps APIs</strong>). We only send the location details you entered (or resolved coordinates) for location resolution and weather forecasting—never your name, email, account ID, or anything else that could tie that location back to another piece of personal information.
                            </li>
                            <li>
                                <strong>Notification Delivery Services:</strong> Browser or device push services (such as <strong>Google</strong>, <strong>Apple</strong>, or <strong>Mozilla</strong>) to deliver encrypted alerts when you enable notifications.
                            </li>
                            <li>
                                <strong>Connected Apps &amp; Integrations You Authorize:</strong> If you connect an external app, AI assistant, or API client, it can access the RateRudder data you authorize until you revoke it in Settings. That service&apos;s own privacy policy governs how it handles your data.
                            </li>
                            <li>
                                <strong>Shared Sites:</strong> Anyone you invite using your Site ID and Invite Code gains full co-owner access to view and manage that site&apos;s history and settings (note that automatically generated Site IDs may include the prefix of the creator&apos;s email address).
                            </li>
                            <li>
                                <strong>Legal, Safety &amp; Continuity:</strong> We may disclose information if required by law, subpoena, or court order, or when necessary to protect the safety, security, or rights of RateRudder, our users, or the public. If operation of the hosted Service is ever transferred to a new entity or maintainer (such as forming a dedicated company), your data will remain governed by this policy.
                            </li>
                        </ul>

                        <h2>5. Aggregated or De-Identified Data</h2>
                        <p>
                            Because RateRudder is an open-source project, we may create <strong>aggregated statistics</strong> (like total energy managed or average savings) or <strong>de-identified site datasets</strong> (such as multi-week hourly energy, battery, price, and weather histories with personal identifiers, credentials, and identifying location details removed). We may use and share this aggregated or de-identified data publicly—for example, on our website, in research, or in our public GitHub repository to test and benchmark our optimization engine—and we will not attempt to re-identify it.
                        </p>

                        <h2>6. How We Protect &amp; Retain Your Data</h2>
                        <ul>
                            <li>
                                <strong>Security:</strong> Sensitive battery credentials and tokens are encrypted at rest using per-site AES-256-GCM encryption before being stored in Google Cloud Firestore, and all network traffic is encrypted over HTTPS. Still, no online service can guarantee 100% security.
                            </li>
                            <li>
                                <strong>Retention &amp; Self-Service Deletion:</strong> We keep your account and site data while you use RateRudder so your history, savings, and forecasts work properly. You can permanently delete your site (including all energy history and encrypted battery credentials) and your user account at any time in <strong>Settings &rarr; Danger Zone</strong>.
                            </li>
                            <li>
                                <strong>Logs &amp; Other Records:</strong> Deleting your site and account in Settings erases your active database records right away. Routine server and security logs are retained temporarily and age out automatically over time. If you ever want us to delete a form submission, feedback message, or an account that doesn&apos;t have a site yet, email us and we&apos;ll remove it promptly.
                            </li>
                            <li>
                                <strong>Data Breaches:</strong> If a security breach ever exposes your personal information (of which we store very little) and encrypted credentials, we will notify affected users and any required authorities promptly in accordance with applicable law.
                            </li>
                        </ul>

                        <h2>7. Your Privacy Rights</h2>
                        <p>
                            Wherever you live, you can ask to view, export, correct, or delete your personal information, revoke connected integrations, or pause automation at any time, either directly in Settings or by emailing us. We will never penalize you for exercising your privacy rights.
                        </p>

                        <h2>8. Children&apos;s Privacy</h2>
                        <p>
                            RateRudder is for adults managing household energy systems and is not directed to anyone under 18. We do not knowingly collect personal data from children under 18.
                        </p>

                        <h2>9. Changes to This Policy</h2>
                        <p>
                            If we make meaningful changes to how we handle your data, we will update the &quot;Last Updated&quot; date above and notify active users by email or an in-app prompt before or when the changes take effect. Continuing to use RateRudder after an update takes effect means you accept the updated policy.
                        </p>

                        <h2>10. Contact Us</h2>
                        <p>
                            Questions or deletion requests? Email us anytime at <a href="mailto:privacy@raterudder.com">privacy@raterudder.com</a>.
                        </p>
                    </div>
                </div>
            </section>
        </div>
    );
};

export default PrivacyPolicy;
