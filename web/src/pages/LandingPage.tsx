import React from 'react';
import { Accordion } from '@base-ui/react/accordion';
import './LandingPage.css';

const timelineEvents = [
    {
        time: "02:00 AM",
        action: "Grid Charge",
        type: "charge",
        title: "Smart Grid Charging",
        description: "RateRudder triggers battery charging from the grid during off-peak hours, drawing only enough energy to power the home until solar generation takes over.",
        factors: [
            "📈 Grid Pricing Schedule",
            "🔋 Battery State-of-Charge",
            "☀️ Upcoming Weather Forecast",
            "🏠 Historical Home Demand"
        ]
    },
    {
        time: "08:00 AM",
        action: "Solar Focus",
        type: "solar",
        title: "Solar Self-Consumption",
        description: "Solar generation powers your home while charging the battery to its full capacity, maximizing clean self-consumption.",
        factors: [
            "🌤️ Real-Time Cloud Cover",
            "⚡ Live Solar Generation",
            "🏠 Current Household Load"
        ]
    },
    {
        time: "02:00 PM",
        action: "Smart Export",
        type: "export",
        title: "Peak Solar Export",
        description: "Utility rates are at their peak. Excess solar is exported to the grid for maximum credits.",
        factors: [
            "💰 High Peak-Tariff Credits",
            "🔋 Current battery state-of-charge",
            "⏱️ Peak Rate Duration Window"
        ]
    },
    {
        time: "07:00 PM",
        action: "Grid Offset",
        type: "offset",
        title: "Evening Battery Discharge",
        description: "The sun has set, but evening grid rates remain high. The battery powers the home, offsetting expensive evening grid energy costs.",
        factors: [
            "🌙 Evening Demand Projection",
            "⏱️ Remaining Peak Window"
        ]
    }
];

const LandingPage: React.FC = () => {


    const faqData = [
        {
            question: "How does RateRudder save me money?",
            answer: "RateRudder intelligently manages your battery to only charge when electricity is cheapest and only when charging is necessary."
        },
        {
            question: "Do I need specific hardware?",
            answer: "Yes, RateRudder currently supports Tesla Powerwall and FranklinWH aPower battery systems. We're looking for testers to help us add support for more battery types soon."
        },
        {
            question: "Which utilities are supported?",
            answer: "We support over 25 utility companies, including ComEd, Ameren, PG&E, Southern California Edison, Duke, and many others, with new providers and rates added regularly."
        },
        {
            question: "How much does it cost?",
            answer: "Nothing! RateRudder is currently free during public beta."
        },
        {
            question: "Is it safe for my battery and electrical system?",
            answer: "Absolutely. RateRudder requires zero physical hardware changes or electrical work. We communicate exclusively through manufacturer APIs to manage settings, just like their mobile apps do."
        }
    ];

    const JOIN_FORM_URL = import.meta.env.VITE_JOIN_FORM_URL;

    return (
        <div className="landing-page">
            <section className="hero-section">
                <div className="content-container hero-layout">
                    <div className="hero-content">
                        {JOIN_FORM_URL && (
                            <div className="badge">Limited Beta Now Open</div>
                        )}
                        <h1>Your Battery, Just <span className="highlight">Smarter.</span></h1>
                        <p>
                            RateRudder transforms your home battery into a powerful financial asset.
                            Intelligently managing your energy to buy low, sell high, and slash your bill—all while you sleep.
                        </p>
                        <div className="cta-wrapper">
                            <div className="cta-button-container">
                                <a href="/login" className="cta-button">
                                    Get Started
                                </a>
                                <span className="cta-note">Free to sign up during public beta</span>
                            </div>
                        </div>
                    </div>
                    <div className="hero-visual">
                        <div className="pulse-circle"></div>
                        <div className="floating-card">
                            <span>Estimated Savings</span>
                            <strong>$12.84</strong>
                            <small>Saved This Month*</small>
                            <div className="status-indicator">
                                <span className="dot" aria-hidden="true"></span> Optimized by RateRudder
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            <section className="setup-section">
                <div className="content-container">
                    <div className="section-header">
                        <h2>Setup in 3 Steps</h2>
                    </div>
                    <div className="setup-steps-wrapper">
                        <div className="setup-step">
                            <div className="step-badge">1</div>
                            <div className="step-icon-container">
                                <svg className="step-icon" viewBox="0 0 24 24" width="32" height="32" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                    <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
                                    <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                                </svg>
                            </div>
                            <h3>Secure Login</h3>
                            <p>Create an account using Google or Apple.</p>
                        </div>
                        <div className="setup-connector" aria-hidden="true"></div>
                        <div className="setup-step">
                            <div className="step-badge">2</div>
                            <div className="step-icon-container">
                                <svg className="step-icon" viewBox="0 0 24 24" width="32" height="32" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                    <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
                                </svg>
                            </div>
                            <h3>Choose Utility</h3>
                            <p>Select your pre-configured rate plan.</p>
                        </div>
                        <div className="setup-connector" aria-hidden="true"></div>
                        <div className="setup-step">
                            <div className="step-badge">3</div>
                            <div className="step-icon-container">
                                <svg className="step-icon" viewBox="0 0 24 24" width="32" height="32" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                    <rect x="2" y="7" width="16" height="10" rx="2" ry="2" />
                                    <path d="M22 11v2" />
                                    <path d="M9 10l-2 3h4l-2 3" />
                                </svg>
                            </div>
                            <h3>Connect Battery</h3>
                            <p>Link your Tesla Powerwall or FranklinWH system.</p>
                        </div>
                    </div>
                    <div className="setup-savings-banner">
                        <span className="sparkle" aria-hidden="true">✨</span>
                        <span className="highlight">That's it!</span> RateRudder automatically manages your energy flow to slash your bills.
                    </div>
                </div>
            </section>

            <section className="features-strip">
                <div className="content-container">
                    <div className="features-grid" onMouseMove={(e) => {
                        const target = e.currentTarget;
                        const items = target.getElementsByClassName('feature-item');
                        for (const item of items) {
                            const rect = item.getBoundingClientRect();
                            const x = e.clientX - rect.left;
                            const y = e.clientY - rect.top;
                            (item as HTMLElement).style.setProperty('--mouse-x', `${x}px`);
                            (item as HTMLElement).style.setProperty('--mouse-y', `${y}px`);
                        }
                    }}>
                        <div className="feature-item arbitrage">
                            <div className="icon" aria-hidden="true">⚡</div>
                            <h3>Automated Arbitrage</h3>
                            <p>Our algorithms track utility rates in real-time, charging your battery when prices bottom out and discharging when they peak.</p>
                        </div>
                        <div className="feature-item grid">
                            <div className="icon" aria-hidden="true">🛡️</div>
                            <h3>Grid Independence</h3>
                            <p>Maximize your solar self-consumption and insulate your home from rising grid costs and peak-hour surcharges.</p>
                        </div>
                        <div className="feature-item intelligence">
                            <div className="icon" aria-hidden="true">🧠</div>
                            <h3>Predictive Intelligence</h3>
                            <p>RateRudder learns your home's unique energy footprint and solar generation patterns to optimize for the days ahead.</p>
                        </div>
                        <div className="feature-item advanced">
                            <div className="icon" aria-hidden="true">🎛️</div>
                            <h3>Advanced Control</h3>
                            <p>RateRudder offers power users granular controls to customize battery reserves, charging priority, and discharge thresholds.</p>
                        </div>
                        <div className="feature-item rocket">
                            <div className="icon" aria-hidden="true">🚀</div>
                            <h3>Set & Forget</h3>
                            <p>Once configured, RateRudder works 24/7 in the background to secure your savings automatically with no manual effort.</p>
                        </div>
                        <div className="feature-item insights">
                            <div className="icon" aria-hidden="true">📊</div>
                            <h3>Energy Insights</h3>
                            <p>Visualize your impact with detailed reports on your energy savings, battery adjustments, and solar generation in real-time.</p>
                        </div>
                    </div>
                </div>
            </section>

            <section className="timeline-section">
                <div className="content-container">
                    <div className="section-header">
                        <h2>Example Day of Savings</h2>
                    </div>

                    <div className="timeline-container">
                        <div className="timeline-line"></div>
                        {timelineEvents.map((event, index) => (
                            <div key={index} className={`timeline-item ${event.type}`}>
                                <div className="timeline-meta">
                                    <div className="timeline-time">{event.time}</div>
                                    <div className="timeline-action-badge">{event.action}</div>
                                </div>
                                <div className="timeline-marker"></div>
                                <div className="timeline-card">
                                    <div className="timeline-card-header">
                                        <h3>{event.title}</h3>
                                    </div>
                                    <p className="timeline-desc">{event.description}</p>
                                    <div className="timeline-factors-section">
                                        <span className="factors-label">Intelligent Factors Analyzed:</span>
                                        <div className="timeline-factors">
                                            {event.factors.map((factor, fIdx) => (
                                                <span key={fIdx} className="factor-pill">{factor}</span>
                                            ))}
                                        </div>
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            </section>

            <section className="faq-section">
                <div className="content-container">
                    <div className="section-header">
                        <h2>Frequently Asked Questions</h2>
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
                    <p className="marketing-disclaimer">
                        *Actual savings vary by utility plan, battery capacity, and household usage.
                    </p>
                </div>
            </section>
        </div>

    );
};

export default LandingPage;
