import React, { useState } from 'react';
import { Link } from 'wouter';
import {
    ResponsiveContainer,
    AreaChart,
    Area,
    Line,
    XAxis,
    YAxis,
    CartesianGrid,
    ReferenceLine,
} from 'recharts';
import { usePageSEO, PUBLIC_PAGES_SEO } from '../utils/seo';
import './MarketingPages.css';

interface TrapScenario {
    id: number;
    numLabel: string;
    title: string;
    chartTitle: string;
    chartCaption: string;
    naiveTitle: string;
    naiveBody: string;
    smartTitle: string;
    smartBody: string;
    data: { time: string; smartSoc: number; naiveSoc: number }[];
}

const trapScenarios: TrapScenario[] = [
    {
        id: 0,
        numLabel: "Trap #1 • Timing & Latency",
        title: "Flat-Rate Procrastination",
        chartTitle: "Overnight Pre-Charge Timing Ahead of a Morning Peak",
        chartCaption: "SmartPlan pairs a gentle holding cost with a pre-peak completion buffer so charging finishes comfortably before peak rates start.",
        naiveTitle: "✕ Naive Price Solver: Either Too Early or Too Late",
        naiveBody: "When overnight rates are flat for several hours, every window looks identical to a basic price solver. It either charges to 100% right at midnight (leaving your cells sitting full all night with no room if rates drop negative later), or waits until the last minute and gets caught short when inverter ramp delays prevent the battery from finishing before the morning peak.",
        smartTitle: "✓ SmartPlan: Just-In-Time Charging with a Pre-Peak Buffer",
        smartBody: "RateRudder applies a small holding cost so the planner naturally waits rather than charging hours early, paired with a pre-peak completion buffer before any material rate jump. Your battery finishes charging with time to spare right before the morning peak begins.",
        data: [
            { time: '12 AM', smartSoc: 25, naiveSoc: 25 },
            { time: '1 AM', smartSoc: 25, naiveSoc: 25 },
            { time: '2 AM', smartSoc: 25, naiveSoc: 25 },
            { time: '3 AM', smartSoc: 35, naiveSoc: 25 },
            { time: '4 AM', smartSoc: 55, naiveSoc: 25 },
            { time: '5 AM', smartSoc: 78, naiveSoc: 32 },
            { time: '6 AM', smartSoc: 80, naiveSoc: 52 },
            { time: '7 AM', smartSoc: 60, naiveSoc: 35 },
        ]
    },
    {
        id: 1,
        numLabel: "Trap #2 • Hardware Stability",
        title: "Sub-Cent Relay Chattering",
        chartTitle: "Mode Stability Across Jittery Wholesale Rates (10 AM → 5 PM)",
        chartCaption: "Transition penalties and action inertia prevent inverter contactors from flipping back and forth over tiny price fluctuations.",
        naiveTitle: "✕ Naive Price Solver: Flipping Modes Over Tiny Price Jitter",
        naiveBody: "Real-time utility prices constantly wiggle up and down by fractions of a cent. A purely mathematical optimizer treats every tiny difference as a signal to act, toggling your inverter between Standby, Self-Consumption, and Grid Charge throughout the afternoon and wearing out hardware relays.",
        smartTitle: "✓ SmartPlan: Contiguous Dispatch & Action Inertia",
        smartBody: "During forward search, RateRudder applies a nominal penalty to every mode switch so the solver groups actions into clean, continuous blocks. Before sending a command to your inverter, it also requires a meaningful savings improvement over your current operating mode.",
        data: [
            { time: '10 AM', smartSoc: 50, naiveSoc: 50 },
            { time: '11 AM', smartSoc: 65, naiveSoc: 44 },
            { time: '12 PM', smartSoc: 80, naiveSoc: 62 },
            { time: '1 PM', smartSoc: 80, naiveSoc: 53 },
            { time: '2 PM', smartSoc: 80, naiveSoc: 71 },
            { time: '3 PM', smartSoc: 80, naiveSoc: 60 },
            { time: '4 PM', smartSoc: 90, naiveSoc: 78 },
            { time: '5 PM', smartSoc: 75, naiveSoc: 62 },
        ]
    },
    {
        id: 2,
        numLabel: "Trap #3 • Battery Longevity",
        title: "Phantom Arbitrage & Cell Wear",
        chartTitle: "Marginal Spread Filtering Across Mild Price Swings",
        chartCaption: "SmartPlan refuses to cycle your battery when the price spread does not beat inverter conversion losses and battery wear.",
        naiveTitle: "✕ Naive Price Solver: Losing Money on Narrow Spreads",
        naiveBody: "If electricity is slightly cheaper at 2 AM than at 9 AM, a naive price solver sees a paper profit and forces a full grid charge and discharge cycle. Once you account for AC/DC inverter conversion losses and battery cycle wear, that trade actually loses money.",
        smartTitle: "✓ SmartPlan: Efficiency & Degradation Hurdles",
        smartBody: "Every candidate action in SmartPlan must overcome physical round-trip efficiency losses, your battery degradation cost, and a minimum price spread. If a peak is not wide enough to pay for conversion losses and cell wear, RateRudder leaves your battery at rest.",
        data: [
            { time: '1 AM', smartSoc: 40, naiveSoc: 40 },
            { time: '2 AM', smartSoc: 40, naiveSoc: 65 },
            { time: '3 AM', smartSoc: 40, naiveSoc: 90 },
            { time: '5 AM', smartSoc: 40, naiveSoc: 90 },
            { time: '7 AM', smartSoc: 40, naiveSoc: 65 },
            { time: '9 AM', smartSoc: 40, naiveSoc: 40 },
            { time: '11 AM', smartSoc: 70, naiveSoc: 70 },
            { time: '1 PM', smartSoc: 95, naiveSoc: 95 },
        ]
    },
    {
        id: 3,
        numLabel: "Trap #4 • Reserve Protection",
        title: "Eroding Backup Reserves",
        chartTitle: "Evening Peak Export vs. Backup Reserve Floor (4 PM → 10 PM)",
        chartCaption: "SmartPlan enforces a prohibitive penalty below your reserve floor and keeps a safety buffer during grid exports.",
        naiveTitle: "✕ Naive Price Solver: Draining Emergency Backup for Arbitrage",
        naiveBody: "Without strict reserve penalties, an optimizer chasing a high evening export rate will dump your battery right down to the edge of your backup floor, or dip into your storm reserve when home load spikes right as the export window ends.",
        smartTitle: "✓ SmartPlan: Prohibitive Reserve Shield & Export Headroom",
        smartBody: "Dipping below your backup reserve incurs a prohibitive penalty ensuring the solver treats your emergency reserve as off-limits for arbitrage. High-power grid and solar exports also require extra headroom above your reserve floor so unexpected home loads never dip into backup power.",
        data: [
            { time: '4 PM', smartSoc: 90, naiveSoc: 90 },
            { time: '5 PM', smartSoc: 65, naiveSoc: 55 },
            { time: '6 PM', smartSoc: 40, naiveSoc: 22 },
            { time: '7 PM', smartSoc: 28, naiveSoc: 14 },
            { time: '8 PM', smartSoc: 25, naiveSoc: 12 },
            { time: '9 PM', smartSoc: 25, naiveSoc: 12 },
            { time: '10 PM', smartSoc: 25, naiveSoc: 12 },
        ]
    }
];

const HowItWorksPage: React.FC = () => {
    usePageSEO(PUBLIC_PAGES_SEO['how-it-works']);
    const [activeTrap, setActiveTrap] = useState(0);
    const currentTrap = trapScenarios[activeTrap];

    return (
        <div className="marketing-subpage">
            {/* Hero Banner */}
            <section className="mp-hero">
                <div className="content-container mp-hero-inner">
                    <span className="mp-hero-eyebrow">UNDER THE HOOD • SMARTPLAN</span>
                    <h1>
                        Why Battery Optimization Is Harder Than <span className="highlight">&ldquo;Buy Low, Sell High&rdquo;</span>
                    </h1>
                    <p>
                        At first glance, automating a home battery sounds like a simple math problem: charge when rates are low and discharge when rates are high. Here is what actually happens when you run a naive price optimizer on a real home battery, and how RateRudder&apos;s <strong>SmartPlan</strong> engine solves it.
                    </p>
                    <div className="mp-hero-pills">
                        <span className="mp-hero-pill">Sub-Hour Resolution</span>
                        <span className="mp-hero-pill">24-Hour Lookahead</span>
                        <span className="mp-hero-pill">Evaluates Thousands of Models</span>
                    </div>
                </div>
            </section>

            {/* Section 1: Interactive Simulator */}
            <section className="mp-section">
                <div className="content-container">
                    <div className="mp-section-header">
                        <span className="mp-eyebrow">REAL-WORLD PHYSICS VS. NAIVE MATH</span>
                        <h2>The 4 Traps of Naive Price Optimization</h2>
                        <p>
                            When we first built RateRudder&apos;s dynamic programming planner, optimizing purely for electricity price produced plans that looked great on a spreadsheet but made no sense in a real home. Click any scenario below to see why:
                        </p>
                    </div>

                    <div className="smartplan-lab-card">
                        <div className="trap-selector-tabs" role="tablist" aria-label="Optimization Trap Scenarios">
                            {trapScenarios.map((scenario, idx) => (
                                <button
                                    key={scenario.id}
                                    type="button"
                                    role="tab"
                                    aria-selected={activeTrap === idx}
                                    className={`trap-tab-btn ${activeTrap === idx ? 'active' : ''}`}
                                    onClick={() => setActiveTrap(idx)}
                                >
                                    <span className="trap-tab-num">{scenario.numLabel}</span>
                                    <span className="trap-tab-title">{scenario.title}</span>
                                </button>
                            ))}
                        </div>

                        <div className="trap-stage-grid">
                            <div className="trap-chart-box" aria-hidden="true">
                                <div className="trap-chart-header">
                                    <span className="trap-chart-title">{currentTrap.chartTitle}</span>
                                    <div className="trap-chart-legend">
                                        <span className="trap-legend-item">
                                            <span className="trap-line-smart" />
                                            SmartPlan
                                        </span>
                                        <span className="trap-legend-item">
                                            <span className="trap-line-naive" />
                                            Naive Price Solver
                                        </span>
                                    </div>
                                </div>

                                <ResponsiveContainer width="100%" height={300}>
                                    <AreaChart data={currentTrap.data} margin={{ top: 10, right: 16, left: -12, bottom: 0 }}>
                                        <defs>
                                            <linearGradient id="smartPlanFill" x1="0" y1="0" x2="0" y2="1">
                                                <stop offset="5%" stopColor="#10b981" stopOpacity={0.36} />
                                                <stop offset="95%" stopColor="#10b981" stopOpacity={0.02} />
                                            </linearGradient>
                                        </defs>
                                        <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.06)" vertical={false} />
                                        <XAxis
                                            dataKey="time"
                                            stroke="rgba(255,255,255,0.4)"
                                            fontSize={12}
                                            tickLine={false}
                                            tickMargin={6}
                                        />
                                        <YAxis
                                            stroke="rgba(255,255,255,0.4)"
                                            fontSize={12}
                                            tickLine={false}
                                            axisLine={false}
                                            domain={[0, 100]}
                                            ticks={[0, 20, 40, 60, 80, 100]}
                                            unit="%"
                                        />
                                        <ReferenceLine y={20} stroke="rgba(255,255,255,0.2)" strokeDasharray="3 3" />
                                        <Line
                                            type="monotone"
                                            dataKey="naiveSoc"
                                            stroke="#f87171"
                                            strokeWidth={2.5}
                                            strokeDasharray="6 4"
                                            dot={{ r: 3, fill: '#f87171' }}
                                            isAnimationActive={false}
                                        />
                                        <Area
                                            type="monotone"
                                            dataKey="smartSoc"
                                            stroke="#10b981"
                                            strokeWidth={3}
                                            fillOpacity={1}
                                            fill="url(#smartPlanFill)"
                                            isAnimationActive={false}
                                        />
                                    </AreaChart>
                                </ResponsiveContainer>

                                <span className="trap-chart-caption">{currentTrap.chartCaption}</span>
                            </div>

                            <div className="trap-explanation-col">
                                <div className="trap-card-naive">
                                    <span className="trap-card-tag">What Goes Wrong Without Guardrails</span>
                                    <h4>{currentTrap.naiveTitle}</h4>
                                    <p>{currentTrap.naiveBody}</p>
                                </div>

                                <div className="trap-card-smart">
                                    <span className="trap-card-tag">How SmartPlan Solves It</span>
                                    <h4>{currentTrap.smartTitle}</h4>
                                    <p>{currentTrap.smartBody}</p>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* Section 2: Guardrails & Penalty Architecture */}
            <section className="mp-section alt-bg">
                <div className="content-container">
                    <div className="mp-section-header">
                        <span className="mp-eyebrow">DYNAMIC PROGRAMMING ARCHITECTURE</span>
                        <h2>Inside the SmartPlan Engine</h2>
                        <p>
                            Every hour, RateRudder divides the upcoming 24-hour lookahead into sub-hour intervals and evaluates thousands of candidate battery trajectories, guided by physical and economic guardrails:
                        </p>
                    </div>

                    <div className="guardrails-grid">
                        <div className="guardrail-card">
                            <div className="guardrail-icon" aria-hidden="true">⏱️</div>
                            <h3>1. Just-In-Time Holding &amp; Pre-Peak Buffer</h3>
                            <p>
                                A small holding penalty breaks flat-rate ties so the planner charges closer to when energy is needed, keeping battery headroom open for negative rates or unexpected midday solar. At the same time, a pre-peak completion buffer penalizes last-minute charging right before a rate jump so inverter ramp delays never leave you short.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <div className="guardrail-icon" aria-hidden="true">🛡️</div>
                            <h3>2. Reserve Deficit Shield &amp; Export Margin</h3>
                            <p>
                                Your backup reserve is sacred. Dipping below your reserve floor incurs a prohibitive penalty ensuring the solver never borrows emergency backup power for arbitrage. High-power grid and solar exports also require extra headroom above your reserve floor before discharging to the grid.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <div className="guardrail-icon" aria-hidden="true">🔒</div>
                            <h3>3. Mode-Switch Penalty &amp; Action Inertia</h3>
                            <p>
                                Every mode transition in the forward search carries a nominal penalty, naturally grouping fragmented sub-hour steps into smooth, continuous charging or discharging blocks. RateRudder also requires a meaningful savings improvement before switching your active inverter mode, eliminating unnecessary relay chatter.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <div className="guardrail-icon" aria-hidden="true">🔋</div>
                            <h3>4. Cycle Degradation &amp; Headroom Guards</h3>
                            <p>
                                Grid charging and battery export plans must overcome both physical AC/DC round-trip efficiency losses and your battery wear cost. RateRudder also requires at least a little bit of empty headroom before starting a new grid charge, preventing brief top-off short-cycling when the battery is already nearly full.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <div className="guardrail-icon" aria-hidden="true">🧠</div>
                            <h3>5. Self-Calibrating Solar &amp; Load Forecasting</h3>
                            <p>
                                A plan is only as good as its inputs. RateRudder calibrates to your roof orientation, seasonal tree shading, and inverter clipping limits automatically, while filtering out high-draw EV charging spikes and adapting to weekend morning routines.
                            </p>
                        </div>

                        <div className="guardrail-card">
                            <div className="guardrail-icon" aria-hidden="true">⚡</div>
                            <h3>6. Time-Based Reserves &amp; VPP Readiness</h3>
                            <p>
                                Keep a higher backup reserve overnight for storms, then automatically lower your reserve floor during expensive peak windows so your battery can cover more of your evening home load. Before utility Virtual Power Plant (VPP) events, SmartPlan automatically pre-charges your battery so you are ready to dispatch.
                            </p>
                        </div>
                    </div>
                </div>
            </section>

            {/* Bottom Call to Action */}
            <section className="mp-section">
                <div className="content-container">
                    <div className="mp-bottom-cta">
                        <h2>See SmartPlan on Your Own Battery</h2>
                        <p>
                            Connect your Tesla Powerwall, FranklinWH, or Enphase system in under two minutes and preview your 24-hour optimization plan.
                        </p>
                        <div className="mp-cta-actions">
                            <Link to="/login" className="mp-btn-primary">
                                Get Started for Free
                            </Link>
                            <Link to="/getting-started" className="mp-btn-secondary">
                                View 2-Minute Setup Guide →
                            </Link>
                        </div>
                    </div>
                </div>
            </section>
        </div>
    );
};

export default HowItWorksPage;
