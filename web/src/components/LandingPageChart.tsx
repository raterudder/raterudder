import React from 'react';
import {
    ResponsiveContainer,
    AreaChart,
    XAxis,
    YAxis,
    CartesianGrid,
    ReferenceLine,
    ReferenceArea,
    Line,
} from 'recharts';
import { ALL_ZONES } from '../utils/planZones';

export interface LandingPageChartPhase {
    x1: string;
    x2: string;
    color: string;
}

export interface LandingPageChartProps {
    data: Record<string, string | number | null>[];
    isMobile: boolean;
    chartHeight: number;
    phase: LandingPageChartPhase;
}

const LandingPageChart: React.FC<LandingPageChartProps> = ({
    data,
    isMobile,
    chartHeight,
    phase,
}) => {
    return (
        <ResponsiveContainer width="100%" height={chartHeight}>
            <AreaChart
                data={data}
                margin={{
                    top: 10,
                    right: isMobile ? 8 : 20,
                    left: isMobile ? -25 : -10,
                    bottom: 0,
                }}
            >
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.06)" vertical={false} />
                <XAxis
                    dataKey="time"
                    stroke="rgba(255,255,255,0.35)"
                    fontSize={isMobile ? 10 : 12}
                    tickLine={false}
                    ticks={
                        isMobile
                            ? ['12 AM', '4 AM', '8 AM', '12 PM', '4 PM', '8 PM']
                            : ['12 AM', '3 AM', '6 AM', '9 AM', '12 PM', '3 PM', '6 PM', '9 PM', '11 PM']
                    }
                    tickMargin={6}
                />
                <YAxis
                    stroke="rgba(255,255,255,0.35)"
                    fontSize={isMobile ? 10 : 12}
                    tickLine={false}
                    axisLine={false}
                    domain={[0, 100]}
                    ticks={isMobile ? [0, 50, 100] : [0, 20, 40, 60, 80, 100]}
                    unit="%"
                />

                {/* Minimum Reserve Line */}
                <ReferenceLine y={20} stroke="rgba(255, 255, 255, 0.2)" strokeDasharray="3 3" />

                {/* Highlighted Phase Area */}
                <ReferenceArea
                    x1={phase.x1}
                    x2={phase.x2}
                    fill={phase.color}
                    fillOpacity={0.14}
                    stroke={phase.color}
                    strokeOpacity={0.35}
                />
                <ReferenceLine x={phase.x1} stroke={phase.color} strokeOpacity={0.7} strokeDasharray="2 2" />
                <ReferenceLine x={phase.x2} stroke={phase.color} strokeOpacity={0.7} strokeDasharray="2 2" />

                {/* Baseline Self-Consumption comparison curve */}
                <Line
                    type="monotone"
                    dataKey="manualSoc"
                    stroke="rgba(193, 198, 215, 0.55)"
                    strokeWidth={2}
                    strokeDasharray="5 5"
                    dot={false}
                    isAnimationActive={false}
                />

                {/* Multi-color RateRudder mode segments */}
                {ALL_ZONES.map((z) => (
                    <Line
                        key={z.key}
                        type="monotone"
                        dataKey={`soc_${z.key}`}
                        stroke={z.color}
                        strokeWidth={3}
                        dot={false}
                        connectNulls={false}
                        isAnimationActive={false}
                    />
                ))}
            </AreaChart>
        </ResponsiveContainer>
    );
};

export default LandingPageChart;
