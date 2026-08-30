import React, { useState, useEffect, useRef } from 'react';
import { Accordion } from '@base-ui/react/accordion';
import {
    ResponsiveContainer,
    AreaChart,
    Area,
    XAxis,
    YAxis,
    Tooltip as RechartsTooltip,
    ReferenceArea,
    CartesianGrid
} from 'recharts';
import './LandingPage.css';

// 24-hour simulation data
const simulationData = [
    { hour: "12 AM", price: 0.04, soc: 30, phase: 0 },
    { hour: "1 AM", price: 0.03, soc: 30, phase: 0 },
    { hour: "2 AM", price: 0.02, soc: 45, phase: 0 },
    { hour: "3 AM", price: 0.02, soc: 60, phase: 0 },
    { hour: "4 AM", price: 0.02, soc: 75, phase: 0 },
    { hour: "5 AM", price: 0.03, soc: 80, phase: 0 },
    { hour: "6 AM", price: 0.05, soc: 80, phase: 1 },
    { hour: "7 AM", price: 0.07, soc: 80, phase: 1 },
    { hour: "8 AM", price: 0.09, soc: 85, phase: 1 },
    { hour: "9 AM", price: 0.11, soc: 90, phase: 1 },
    { hour: "10 AM", price: 0.12, soc: 95, phase: 1 },
    { hour: "11 AM", price: 0.14, soc: 100, phase: 1 },
    { hour: "12 PM", price: 0.16, soc: 100, phase: 2 },
    { hour: "1 PM", price: 0.22, soc: 100, phase: 2 },
    { hour: "2 PM", price: 0.28, soc: 100, phase: 2 },
    { hour: "3 PM", price: 0.35, soc: 100, phase: 2 },
    { hour: "4 PM", price: 0.38, soc: 95, phase: 2 },
    { hour: "5 PM", price: 0.42, soc: 90, phase: 3 },
    { hour: "6 PM", price: 0.45, soc: 82, phase: 3 },
    { hour: "7 PM", price: 0.45, soc: 72, phase: 3 },
    { hour: "8 PM", price: 0.40, soc: 62, phase: 3 },
    { hour: "9 PM", price: 0.30, soc: 52, phase: 3 },
    { hour: "10 PM", price: 0.15, soc: 42, phase: 3 },
    { hour: "11 PM", price: 0.08, soc: 35, phase: 3 }
];

const phases = [
    {
        id: 0,
        title: "Late Night Grid Charge",
        action: "Grid Charging",
        icon: "🔌",
        timeRange: "12 AM - 6 AM",
        description: "RateRudder monitors grid rates, solar forecast, and home load to charging from the grid during the cheapest off-peak hours, ensuring your battery starts the day with just enough energy to cover the morning.",
        priceInfo: "$0.02 / kWh",
        socInfo: "Charging: 30% → 80%",
        color: "var(--primary)"
    },
    {
        id: 1,
        title: "Morning Solar Focus",
        action: "Solar Self-Consumption",
        icon: "☀️",
        timeRange: "6 AM - 12 PM",
        description: "Grid rates start to rise and solar panels begin generating electricity. RateRudder will prioritize charging the battery with solar and stop drawing from the grid.",
        priceInfo: "$0.08 / kWh",
        socInfo: "Topping up: 80% → 100%",
        color: "var(--warning)"
    },
    {
        id: 2,
        title: "Afternoon Peak Export",
        action: "Smart Solar Export",
        icon: "💰",
        timeRange: "12 PM - 5 PM",
        description: "Once the battery is fully charged, RateRudder exports surplus solar generation to the grid during high-credit hours, maximizing utility credits.",
        priceInfo: "$0.28 - $0.38 / kWh",
        socInfo: "Maintained: 100% capacity",
        color: "var(--accent)"
    },
    {
        id: 3,
        title: "Evening Grid Offset",
        action: "Battery Discharging",
        icon: "🌙",
        timeRange: "5 PM - 11 PM",
        description: "The sun has set but evening electricity pricing remains at its absolute peak. RateRudder discharges the battery to run your home, avoiding expensive imports.",
        priceInfo: "$0.40 - $0.45 / kWh",
        socInfo: "Offsetting: 100% → 35%",
        color: "#ff007a"
    }
];

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

const LandingPage: React.FC = () => {
    const [activePhase, setActivePhase] = useState(0);
    const [isAutoplay, setIsAutoplay] = useState(true);
    const autoplayRef = useRef<number | null>(null);

    // Auto-cycle through the 4 phases every 4.5 seconds
    useEffect(() => {
        if (isAutoplay) {
            autoplayRef.current = window.setInterval(() => {
                setActivePhase((prev) => (prev + 1) % 4);
            }, 4500);
        }
        return () => {
            if (autoplayRef.current) {
                clearInterval(autoplayRef.current);
            }
        };
    }, [isAutoplay]);

    const handlePhaseSelect = (phaseId: number) => {
        setIsAutoplay(false); // Stop autoplay when user interacts
        setActivePhase(phaseId);
    };

    const isRateRudder = typeof window !== 'undefined' && (window.location.hostname === 'raterudder.com' || window.location.hostname.endsWith('.raterudder.com'));

    // Get current phase details
    const currentPhase = phases[activePhase];

    return (
        <div className="landing-page">
            <section className="hero-section">
                <div className="content-container hero-layout">
                    <div className="hero-content">
                        {isRateRudder && (
                            <div className="badge">Limited Beta Now Open</div>
                        )}
                        <h1>Your Battery, Just <span className="highlight">Smarter.</span></h1>
                        <p>
                            RateRudder transforms your home battery into a powerful financial asset.
                            We intelligently schedule your energy storage to charge on cheap grid power, offset peak rates, and export solar when credits are highest—automatically.
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

                    <div className="hero-visual-wrapper">
                        {/* Decision Factors Card */}
                        <div className="decision-factors-card">
                            <div className="card-header">
                                <span className="pulse-dot"></span>
                                <h3>Decision Factors</h3>
                            </div>
                            <p className="card-sub">What RateRudder analyzes before making decisions:</p>
                            <ul className="factors-list">
                                <li className="factor-item-check">
                                    <span className="check-icon">✓</span>
                                    <div className="factor-details">
                                        <strong>Solar Generation Projections</strong>
                                        <span>Predicts using sun angle and weather forecast.</span>
                                    </div>
                                </li>
                                <li className="factor-item-check">
                                    <span className="check-icon">✓</span>
                                    <div className="factor-details">
                                        <strong>Historical Home Usage</strong>
                                        <span>Learns patterns to calculate battery needs.</span>
                                    </div>
                                </li>
                                <li className="factor-item-check">
                                    <span className="check-icon">✓</span>
                                    <div className="factor-details">
                                        <strong>Weather & Temperature</strong>
                                        <span>Forecasts consumption using weather forecast.</span>
                                    </div>
                                </li>
                                <li className="factor-item-check">
                                    <span className="check-icon">✓</span>
                                    <div className="factor-details">
                                        <strong>Future Utility Rates</strong>
                                        <span>Tracks utility rates hours in advance.</span>
                                    </div>
                                </li>
                                <li className="factor-item-check">
                                    <span className="check-icon">✓</span>
                                    <div className="factor-details">
                                        <strong>Battery State-of-Charge</strong>
                                        <span>Respects reserve and avoids unnecessary cycles.</span>
                                    </div>
                                </li>
                            </ul>
                            <div className="card-footer-banner">
                                <span>⚡ Automatically in the background 24/7 to save you money.</span>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* Redesigned Interactive Chart Section */}
            <section className="chart-section">
                <div className="content-container">
                    <div className="section-header">
                        <h2>Daily Optimization</h2>
                        <p>
                            Hover or click the phases below to see examples of how RateRudder controls your battery.
                        </p>
                    </div>

                    <div className="interactive-chart-container">
                        {/* Selector Tabs */}
                        <div className="phase-selectors">
                            {phases.map((phase) => (
                                <button
                                    key={phase.id}
                                    className={`phase-btn ${activePhase === phase.id ? 'active' : ''}`}
                                    style={{
                                        borderColor: activePhase === phase.id ? phase.color : 'transparent',
                                        backgroundColor: activePhase === phase.id ? 'var(--surface-container-high)' : 'transparent'
                                    }}
                                    onClick={() => handlePhaseSelect(phase.id)}
                                >
                                    <span className="btn-icon">{phase.icon}</span>
                                    <div className="btn-text">
                                        <span className="btn-title">{phase.title}</span>
                                        <span className="btn-time">{phase.timeRange}</span>
                                    </div>
                                </button>
                            ))}
                        </div>

                        {/* Recharts Graphical Display */}
                        <div className="chart-visual-container" onMouseEnter={() => setIsAutoplay(false)}>
                            <ResponsiveContainer width="100%" height={320}>
                                <AreaChart data={simulationData} margin={{ top: 20, right: -5, left: -25, bottom: 0 }}>
                                    <defs>
                                        <linearGradient id="priceGradient" x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="5%" stopColor="var(--warning)" stopOpacity={0.2}/>
                                            <stop offset="95%" stopColor="var(--warning)" stopOpacity={0}/>
                                        </linearGradient>
                                        <linearGradient id="socGradient" x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="5%" stopColor="var(--primary)" stopOpacity={0.15}/>
                                            <stop offset="95%" stopColor="var(--primary)" stopOpacity={0}/>
                                        </linearGradient>
                                    </defs>
                                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
                                    <XAxis
                                        dataKey="hour"
                                        stroke="var(--text-muted)"
                                        fontSize={11}
                                        tickLine={false}
                                        axisLine={false}
                                    />
                                    <YAxis
                                        yAxisId="left"
                                        stroke="var(--warning)"
                                        fontSize={10}
                                        tickLine={false}
                                        axisLine={false}
                                        tickFormatter={(val) => `$${val.toFixed(2)}`}
                                    />
                                    <YAxis
                                        yAxisId="right"
                                        orientation="right"
                                        stroke="var(--primary)"
                                        fontSize={10}
                                        tickLine={false}
                                        axisLine={false}
                                        tickFormatter={(val) => `${val}%`}
                                        domain={[0, 100]}
                                    />
                                    <RechartsTooltip
                                        contentStyle={{
                                            backgroundColor: 'var(--surface-container-high)',
                                            borderColor: 'rgba(255,255,255,0.1)',
                                            borderRadius: '8px',
                                            color: 'var(--on-surface)'
                                        }}
                                        formatter={(value, name) => {
                                            if (name === "price") return [`$${Number(value).toFixed(2)}/kWh`, "Grid Price"];
                                            if (name === "soc") return [`${value}%`, "Battery Charge"];
                                            return [value, name];
                                        }}
                                    />
                                    {/* Grid Price curve */}
                                    <Area
                                        yAxisId="left"
                                        type="monotone"
                                        dataKey="price"
                                        stroke="var(--warning)"
                                        strokeWidth={2}
                                        fillOpacity={1}
                                        fill="url(#priceGradient)"
                                        name="price"
                                    />
                                    {/* Battery charge curve */}
                                    <Area
                                        yAxisId="right"
                                        type="monotone"
                                        dataKey="soc"
                                        stroke="var(--primary)"
                                        strokeWidth={2.5}
                                        fillOpacity={1}
                                        fill="url(#socGradient)"
                                        name="soc"
                                    />
                                    {/* Highlight region for active phase */}
                                    {activePhase === 0 && <ReferenceArea yAxisId="right" x1="12 AM" x2="5 AM" fill="rgba(75, 142, 255, 0.08)" strokeOpacity={0.3} />}
                                    {activePhase === 1 && <ReferenceArea yAxisId="right" x1="6 AM" x2="11 AM" fill="rgba(255, 184, 0, 0.08)" strokeOpacity={0.3} />}
                                    {activePhase === 2 && <ReferenceArea yAxisId="right" x1="12 PM" x2="4 PM" fill="rgba(0, 255, 194, 0.08)" strokeOpacity={0.3} />}
                                    {activePhase === 3 && <ReferenceArea yAxisId="right" x1="5 PM" x2="11 PM" fill="rgba(255, 0, 122, 0.08)" strokeOpacity={0.3} />}
                                </AreaChart>
                            </ResponsiveContainer>
                        </div>

                        {/* Interactive Phase Info Panel */}
                        <div className="phase-info-panel" style={{ borderLeftColor: currentPhase.color }}>
                            <div className="panel-header">
                                <span className="panel-badge" style={{ backgroundColor: `${currentPhase.color}15`, color: currentPhase.color }}>
                                    {currentPhase.action}
                                </span>
                                <h3>{currentPhase.title} <span className="time-sub">({currentPhase.timeRange})</span></h3>
                            </div>
                            <p className="panel-desc">{currentPhase.description}</p>
                            <div className="panel-metrics">
                                <div className="metric-pill">
                                    <span className="pill-label">Grid Price</span>
                                    <span className="pill-val text-warning">{currentPhase.priceInfo}</span>
                                </div>
                                <div className="metric-pill">
                                    <span className="pill-label">Battery SOC</span>
                                    <span className="pill-val text-primary">{currentPhase.socInfo}</span>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            <section className="setup-section">
                <div className="content-container">
                    <div className="section-header">
                        <h2>Simple Setup</h2>
                        <p>RateRudder integrates with your battery system in minutes.</p>
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
                            <h3>Secure Sign-in</h3>
                            <p>Authenticate with Google or Apple.</p>
                        </div>
                        <div className="setup-connector" aria-hidden="true"></div>
                        <div className="setup-step">
                            <div className="step-badge">2</div>
                            <div className="step-icon-container">
                                <svg className="step-icon" viewBox="0 0 24 24" width="32" height="32" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                    <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
                                </svg>
                            </div>
                            <h3>Choose Utility Plan</h3>
                            <p>Select your rate plan (like ComEd Hourly or PG&E E-ELEC). We monitor rates in real-time.</p>
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
                            <h3>Link Your Battery</h3>
                            <p>Connect to your Tesla Powerwall or FranklinWH system.</p>
                        </div>
                    </div>
                </div>
            </section>

            {/* Benefits & Features Grid */}
            <section className="features-strip">
                <div className="content-container">
                    <div className="section-header">
                        <h2>Smart Arbitrage</h2>
                        <p>Get the most value out of your home energy storage with automated cost optimization.</p>
                    </div>
                    <div className="features-grid">
                        <div className="feature-item arbitrage">
                            <div className="icon" aria-hidden="true">⚡</div>
                            <h3>Automated Arbitrage</h3>
                            <p>Our algorithms track utility rates in real-time, charging your battery when prices bottom out and discharging when they peak.</p>
                        </div>
                        <div className="feature-item intelligence">
                            <div className="icon" aria-hidden="true">🧠</div>
                            <h3>Smart Charging</h3>
                            <p>RateRudder learns your home's unique energy footprint and solar forecast to charge the battery to exactly what is needed, avoiding unnecessary grid imports before solar starts.</p>
                        </div>
                        <div className="feature-item grid">
                            <div className="icon" aria-hidden="true">🛡️</div>
                            <h3>Set & Forget</h3>
                            <p>RateRudder runs 24/7 in the cloud to manage your battery automatically, keeping your energy cost optimization completely hands-free.</p>
                        </div>
                    </div>
                </div>
            </section>

            <section className="faq-section">
                <div className="content-container">
                    <div className="section-header">
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
                    <p className="marketing-disclaimer">
                        *Actual savings vary by utility plan, battery capacity, solar generation, and household usage.
                    </p>
                </div>
            </section>
        </div>
    );
};

export default LandingPage;

