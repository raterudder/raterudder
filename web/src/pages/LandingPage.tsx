import React from 'react';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, ResponsiveContainer } from 'recharts';
import { Accordion } from '@base-ui/react/accordion';
import './LandingPage.css';

const LandingPage: React.FC = () => {
    // Fake data for charts with more "flashy" and realistic variability
    const solarData = React.useMemo(() => {
        // Use a pseudo-random function so data is consistent between renders
        const pseudoRandom = (seed: number) => {
            const x = Math.sin(seed++) * 10000;
            return x - Math.floor(x);
        };

        return Array.from({ length: 24 }, (_, i) => {
            let base = i >= 6 && i <= 18 ? Math.sin((i - 6) * Math.PI / 12) * 7 : 0;
            // Add some "cloud" dips and atmospheric noise
            if (base > 0) {
                const noise = 0.9 + pseudoRandom(i) * 0.2;
                const clouds = (i === 10 || i === 14) ? 0.7 : 1;
                base = base * noise * clouds;
            }
            return {
                name: `${i}:00`,
                uv: parseFloat(base.toFixed(2)),
            };
        });
    }, []);

    const usageData = React.useMemo(() => {
        const pseudoRandom = (seed: number) => {
            const x = Math.sin(seed++) * 10000;
            return x - Math.floor(x);
        };
        return Array.from({ length: 24 }, (_, i) => ({
            name: `${i}:00`,
            usage: parseFloat((.8 + pseudoRandom(i + 100) * 0.5 + (i > 17 && i < 22 ? 2 : 0) + (i > 6 && i < 9 ? 1.5 : 0)).toFixed(2)),
        }));
    }, []);

    const batteryData = React.useMemo(() => {
        const pseudoRandom = (seed: number) => {
            const x = Math.sin(seed++) * 10000;
            return x - Math.floor(x);
        };
        return Array.from({ length: 24 }, (_, i) => {
            let level = 20;
            if (i > 8 && i < 17) level = 40 + (i - 8) * 7 + pseudoRandom(i + 200) * 5;
            else if (i >= 17) level = 95 - (i - 17) * 8;
            else level = 30 - i * 1.5;
            return { name: `${i}:00`, level: Math.min(100, Math.max(0, parseFloat(level.toFixed(1)))) };
        });
    }, []);

    const [isMobile, setIsMobile] = React.useState(window.innerWidth < 768);

    React.useEffect(() => {
        const handleResize = () => setIsMobile(window.innerWidth < 768);
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    const faqData = [
        {
            question: "How does RateRudder save me money?",
            answer: "RateRudder intelligently manages your battery to only charge when electricity is cheapest and only when charging is necessary."
        },
        {
            question: "Do I need specific hardware?",
            answer: "Currently, only FranklinWH aPower batteries are supported. We're looking for testers to help us add support for more battery types soon."
        },
        {
            question: "Which utilities are supported?",
            answer: "Currently only a limited set of utility provider rates are supported but new utilities are being added quickly based on demand."
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

    const chartMargin = { top: 10, right: 10, left: 0, bottom: 0 };
    const axisStyle = { fontSize: isMobile ? 10 : 12, fontFamily: 'Inter, sans-serif' };
    const yAxisWidth = isMobile ? 30 : 40;

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
                        {JOIN_FORM_URL && (
                            <div className="cta-wrapper">
                                <a href={JOIN_FORM_URL} target="_blank" rel="noopener noreferrer" className="cta-button">
                                    Request Early Access
                                </a>
                                <span className="cta-note">Tell us about your battery and utility to help skip the queue.</span>

                            </div>
                        )}
                    </div>
                    <div className="hero-visual">
                        <div className="pulse-circle"></div>
                        <div className="floating-card">
                            <span>Estimated Savings</span>
                            <strong>$12.84</strong>
                            <small>This Month*</small>
                            <div className="status-indicator">
                                <span className="dot"></span> Optimized by RateRudder
                            </div>
                        </div>
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
                            <div className="icon">⚡</div>
                            <h3>Automated Arbitrage</h3>
                            <p>Our algorithms track utility rates in real-time, charging your battery when prices bottom out and discharging when they peak.</p>
                        </div>
                        <div className="feature-item grid">
                            <div className="icon">🛡️</div>
                            <h3>Grid Independence</h3>
                            <p>Maximize your solar self-consumption and insulate your home from rising grid costs and peak-hour surcharges.</p>
                        </div>
                        <div className="feature-item intelligence">
                            <div className="icon">🧠</div>
                            <h3>Predictive Intelligence</h3>
                            <p>RateRudder learns your home's unique energy footprint and solar generation patterns to optimize for the days ahead.</p>
                        </div>
                        <div className="feature-item advanced">
                            <div className="icon">🎛️</div>
                            <h3>Advanced Control</h3>
                            <p>RateRudder offers power users granular controls to customize battery reserves, charging priority, and discharge thresholds.</p>
                        </div>
                        <div className="feature-item rocket">
                            <div className="icon">🚀</div>
                            <h3>Set & Forget</h3>
                            <p>Once configured, RateRudder works 24/7 in the background to secure your savings automatically with no manual effort.</p>
                        </div>
                        <div className="feature-item insights">
                            <div className="icon">📊</div>
                            <h3>Energy Insights</h3>
                            <p>Visualize your impact with detailed reports on your energy savings, battery adjustments, and solar generation in real-time.</p>
                        </div>
                    </div>
                </div>
            </section>

            <section className="live-demo-section">
                <div className="content-container">
                    <div className="section-header">
                        <h2>Intelligent Energy Forecast</h2>
                    </div>

                    <div className="charts-grid">
                        <div className="chart-card">
                            <div className="chart-header">
                                <h3>Solar Generation</h3>
                                <div className="chart-stat">Peak: 7.0kW</div>
                            </div>
                            <div className="chart-wrapper">
                                <ResponsiveContainer width="100%" height="100%">
                                    <AreaChart data={solarData} margin={chartMargin}>
                                        <defs>
                                            <linearGradient id="colorSolar" x1="0" y1="0" x2="0" y2="1">
                                                <stop offset="5%" stopColor="#ffb800" stopOpacity={0.8}/>
                                                <stop offset="95%" stopColor="#ffb800" stopOpacity={0}/>
                                            </linearGradient>
                                        </defs>
                                        <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="rgba(65, 71, 85, 0.1)" />
                                        <XAxis dataKey="name" tick={axisStyle} stroke="#414755" axisLine={false} tickLine={false} />
                                        <YAxis tick={axisStyle} width={yAxisWidth} stroke="#414755" axisLine={false} tickLine={false} />
                                        <Area type="monotone" dataKey="uv" stroke="#ffb800" strokeWidth={3} fillOpacity={1} fill="url(#colorSolar)" isAnimationActive={true} />
                                    </AreaChart>
                                </ResponsiveContainer>
                            </div>
                        </div>

                        <div className="chart-card">
                            <div className="chart-header">
                                <h3>Home Usage</h3>
                                <div className="chart-stat">Avg: 1.2kW</div>
                            </div>
                            <div className="chart-wrapper">
                                <ResponsiveContainer width="100%" height="100%">
                                    <AreaChart data={usageData} margin={chartMargin}>
                                        <defs>
                                            <linearGradient id="colorUsage" x1="0" y1="0" x2="0" y2="1">
                                                <stop offset="5%" stopColor="#4b8eff" stopOpacity={0.8}/>
                                                <stop offset="95%" stopColor="#4b8eff" stopOpacity={0}/>
                                            </linearGradient>
                                        </defs>
                                        <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="rgba(65, 71, 85, 0.1)" />
                                        <XAxis dataKey="name" tick={axisStyle} stroke="#414755" axisLine={false} tickLine={false} />
                                        <YAxis tick={axisStyle} width={yAxisWidth} stroke="#414755" axisLine={false} tickLine={false} />
                                        <Area type="monotone" dataKey="usage" stroke="#4b8eff" strokeWidth={3} fillOpacity={1} fill="url(#colorUsage)" isAnimationActive={true} />
                                    </AreaChart>
                                </ResponsiveContainer>
                            </div>
                        </div>

                        <div className="chart-card full-width">
                            <div className="chart-header">
                                <h3>Battery Capacity</h3>
                                <div className="chart-stat">SoC: 84%</div>
                            </div>
                            <div className="chart-wrapper">
                                <ResponsiveContainer width="100%" height="100%">
                                    <AreaChart data={batteryData} margin={chartMargin}>
                                        <defs>
                                            <linearGradient id="colorBattery" x1="0" y1="0" x2="0" y2="1">
                                                <stop offset="5%" stopColor="#00ffc2" stopOpacity={0.4}/>
                                                <stop offset="95%" stopColor="#00ffc2" stopOpacity={0}/>
                                            </linearGradient>
                                        </defs>
                                        <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="rgba(65, 71, 85, 0.1)" />
                                        <XAxis dataKey="name" tick={axisStyle} stroke="#414755" axisLine={false} tickLine={false} />
                                        <YAxis tick={axisStyle} width={yAxisWidth} stroke="#414755" axisLine={false} tickLine={false} />
                                        <Area type="monotone" dataKey="level" stroke="#00ffc2" strokeWidth={3} fillOpacity={1} fill="url(#colorBattery)" isAnimationActive={true} />
                                    </AreaChart>
                                </ResponsiveContainer>
                            </div>
                        </div>
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
                                        <span className="toggle-icon" />
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
