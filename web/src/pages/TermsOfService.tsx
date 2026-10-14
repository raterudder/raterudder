import React from 'react';
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
                    <p>Last Updated: February 16, 2026</p>
                </div>
            </section>

            <section className="mp-section">
                <div className="content-container">
                    <div className="policy-container">
                        <h2>1. Acceptance of Terms</h2>
                        <p>
                            By accessing or using the RateRudder website and automated energy management service (the &quot;Service&quot;), you agree to be bound by these Terms of Service (&quot;Terms&quot;). If you disagree with any part of these Terms, you may not use the Service.
                        </p>

                        <h2>2. Eligibility</h2>
                        <p>
                            You must be at least 18 years old and have the legal capacity to enter into these Terms to use the Service.
                        </p>

                        <h2>3. Authorization of Automated Actions</h2>
                        <p>
                            By using RateRudder, you grant us authority to act as your agent to monitor and manage your grid-connected devices, including automatically adjusting energy storage, toggling hardware states, and interfacing with third-party providers. You represent that you have the legal right to grant such access for the premises and hardware involved.
                        </p>

                        <h2>4. Acceptable Use</h2>
                        <p>
                            You agree to use the Service only for personal energy management. You may not: breach any security or authentication measures; interfere with or disrupt any user, host, or network; or access or tamper with private data of the Service or our providers&apos; systems.
                        </p>

                        <h2>5. Intellectual Property and Open-Source License</h2>
                        <p>
                            RateRudder&apos;s source code is available under the <strong>GNU General Public License v3.0 (GPLv3)</strong>, which exclusively governs your rights to copy, modify, and distribute the code. Nothing in these Terms restricts your rights under the GPLv3. However, the RateRudder name, logo, and branding are our exclusive property and may not be used without prior written consent. The hosted Service, including its infrastructure, data, and operational configuration, is proprietary and is not conveyed by the open-source license.
                        </p>

                        <h2>6. Third-Party Services</h2>
                        <p>
                            The Service may interact with third-party websites, APIs, and platforms (including utility providers and hardware manufacturers) that we do not control. We are not responsible for any damage or loss resulting from your use of or reliance on such third-party services.
                        </p>

                        <h2>7. Disclaimer of Warranties</h2>
                        <p>
                            <strong>The Service is provided &quot;AS IS&quot; and &quot;AS AVAILABLE.&quot;</strong> We make no warranties, express or implied, regarding the Service&apos;s operation, accuracy, or results, including any specific financial savings or grid efficiency. To the fullest extent permitted by law, we disclaim all warranties, including implied warranties of merchantability and fitness for a particular purpose.
                        </p>

                        <h2>8. Limitation of Liability</h2>
                        <p>
                            RateRudder and its directors, employees, and affiliates shall not be liable for any indirect, incidental, special, consequential, or punitive damages, including loss of profits, data, or goodwill; hardware malfunctions; food spoilage; or other losses arising from your use of the Service or from automated management of your energy usage. Your sole remedy for dissatisfaction with the Service is to stop using it.
                        </p>

                        <h2>9. Indemnification</h2>
                        <p>
                            You agree to indemnify and hold harmless RateRudder from any claims, losses, or expenses (including reasonable attorneys&apos; fees) arising from: (a) your use of the Service; (b) your violation of these Terms or any third-party right; or (c) unauthorized modification of hardware or systems managed by the Service.
                        </p>

                        <h2>10. Termination</h2>
                        <p>
                            We may terminate or suspend your access at any time, without notice, for any reason, including a breach of these Terms. We also reserve the right to refuse service at our sole discretion, provided such refusal is not based on any legally protected characteristic. Provisions that by their nature should survive termination (including disclaimers, liability limits, and indemnification) shall survive.
                        </p>

                        <h2>11. Governing Law</h2>
                        <p>
                            These Terms are governed by the laws of the jurisdiction in which RateRudder&apos;s principal operator resides, without regard to conflict-of-law principles. Any dispute arising from these Terms or the Service shall be resolved in the courts of that jurisdiction, and you consent to their personal jurisdiction.
                        </p>

                        <h2>12. Changes to Terms</h2>
                        <p>
                            We may modify these Terms at any time by updating the &quot;Last Updated&quot; date above. Where practicable, we will also notify you via email or through the Service. Continued use after changes constitutes acceptance of the revised Terms.
                        </p>

                        <h2>13. General</h2>
                        <p>
                            If any provision of these Terms is found unenforceable, it will be modified to the minimum extent necessary, and the remaining provisions remain in full effect. These Terms and our Privacy Policy constitute the entire agreement between you and RateRudder regarding the Service.
                        </p>

                        <h2>14. Contact Us</h2>
                        <p>
                            Questions about these Terms? Contact us at <a href="mailto:legal@raterudder.com">legal@raterudder.com</a>.
                        </p>
                    </div>
                </div>
            </section>
        </div>
    );
};

export default TermsOfService;
