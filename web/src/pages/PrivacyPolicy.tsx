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
                    <p>Last Updated: February 17, 2026</p>
                </div>
            </section>

            <section className="mp-section">
                <div className="content-container">
                    <div className="policy-container">
                        <h2>1. Data We Collect</h2>
                        <p>
                            RateRudder (&quot;we&quot;, &quot;our&quot;, or &quot;us&quot;) collects information necessary to operate the Service:
                        </p>
                        <ul>
                            <li><strong>Account Credentials:</strong> Email address, authentication details, and profile information.</li>
                            <li><strong>Energy Telemetry:</strong> Home energy consumption, solar production, battery state, and other hardware performance data.</li>
                            <li><strong>Utility Integration Data:</strong> Credentials for utility providers, pricing data, and billing history.</li>
                            <li><strong>Technical Metadata:</strong> IP addresses and interaction logs for service stability and security.</li>
                        </ul>

                        <h2>2. How We Use Your Data</h2>
                        <p>
                            We use collected data to provide, maintain, and improve the Service:
                        </p>
                        <ul>
                            <li><strong>Service Delivery:</strong> Operating your automated energy optimization and communicating with your hardware and utility provider APIs.</li>
                            <li><strong>Improvement:</strong> Using usage patterns and system responses to improve our optimization algorithms and machine learning models. By using the Service, you consent to this use.</li>
                            <li><strong>Communications:</strong> Sending critical alerts about your energy system or service changes.</li>
                            <li><strong>Security:</strong> Monitoring for unauthorized access or hardware manipulation.</li>
                        </ul>

                        <h2>3. Cookies</h2>
                        <p>
                            RateRudder uses a single, essential cookie for session authentication, which is required to keep you logged in. We do not use analytics cookies, advertising trackers, or other tracking technologies. Our infrastructure providers (<strong>Cloudflare</strong> and <strong>Google Cloud Platform</strong>) may set strictly necessary cookies for security, bot protection, and load balancing; these are not used for advertising or tracking.
                        </p>

                        <h2>4. Aggregated and De-identified Data</h2>
                        <p>
                            We may create anonymized, aggregated data that cannot reasonably be linked back to you. <strong>We reserve the right to share this data publicly and with third parties</strong>, for example, publishing average savings metrics or grid-impact reports for marketing, research, or industry analysis. We retain a perpetual, royalty-free right to use such de-identified data.
                        </p>

                        <h2>5. Third-Party Disclosures</h2>
                        <p>
                            We do not sell your personal data. Your data is processed through essential third-party channels:
                        </p>
                        <ul>
                            <li><strong>Cloud Infrastructure:</strong> The Service is hosted on <strong>Google Cloud Platform (GCP)</strong>, subject to their industry-standard security protocols.</li>
                            <li><strong>API Partners:</strong> We transmit necessary tokens to utility provider and hardware APIs to operate the Service on your behalf.</li>
                            <li><strong>Legal Compliance:</strong> We may disclose information if required by law or to protect the safety, rights, or property of RateRudder, our users, or the public.</li>
                        </ul>

                        <h2>6. Data Security and Retention</h2>
                        <p>
                            We use administrative and technical safeguards, including encryption of sensitive credentials, to protect your data. No internet transmission is 100% secure, and we cannot guarantee absolute security. We retain personal data only as long as necessary to provide the Service or comply with legal obligations. Upon account deletion, your personal data will be removed within a reasonable timeframe except where retention is required by law.
                        </p>

                        <h2>7. Data Breach Notification</h2>
                        <p>
                            If a data breach affects your personal information, we will notify you and relevant authorities promptly, as required by applicable law.
                        </p>

                        <h2>8. Your Rights</h2>
                        <p>
                            You may request access to, correction of, or deletion of your personal information at any time by contacting us. We will not discriminate against you for exercising your data rights.
                        </p>

                        <h2>9. Children&apos;s Privacy</h2>
                        <p>
                            The Service is not directed to individuals under 18. We do not knowingly collect personal information from children; if we learn we have, we will delete it promptly.
                        </p>

                        <h2>10. Updates to This Policy</h2>
                        <p>
                            We may update this Privacy Policy at any time by updating the &quot;Last Updated&quot; date above. Where practicable, we will also notify you via email or through the Service. Continued use after changes constitutes acceptance of the revised policy.
                        </p>

                        <h2>11. Contact Us</h2>
                        <p>
                            Questions about this policy or your data rights? Contact us at <a href="mailto:privacy@raterudder.com">privacy@raterudder.com</a>.
                        </p>
                    </div>
                </div>
            </section>
        </div>
    );
};

export default PrivacyPolicy;
