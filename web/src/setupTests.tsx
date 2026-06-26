
import '@testing-library/jest-dom';
import { vi } from 'vitest';
import React from 'react';

// Mock ResizeObserver which is used by ResponsiveContainer
globalThis.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// Mock Recharts components widely to prevent rendering warnings
vi.mock('recharts', async () => {
    const OriginalModule = await vi.importActual('recharts');
    return {
        ...OriginalModule,
        // ResponsiveContainer must render its children (the chart)
        ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
            <div className="recharts-responsive-container" style={{ width: 800, height: 800 }}>
                {children}
            </div>
        ),
        // Chart components should NOT render children to avoid rendering SVG elements (like <stop>)
        // directly into a <div> (since we are mocking these as divs), which causes browser/jsdom warnings.
        // We render a simple div with a data-testid for identification if needed.
        AreaChart: ({ children }: { children: React.ReactNode }) => (
            <div data-testid="area-chart">
                {React.Children.toArray(children).filter(
                    (child: any) => child && child.type !== 'defs'
                )}
            </div>
        ),
        LineChart: () => <div data-testid="line-chart" />,
        BarChart: () => <div data-testid="bar-chart" />,
        PieChart: () => <div data-testid="pie-chart" />,
        // We can create dummy components for other elements if they are used elsewhere
        Area: () => <div data-testid="area" />,
        Line: () => <div data-testid="line" />,
        Bar: () => <div data-testid="bar" />,
        Pie: () => <div data-testid="pie" />,
        XAxis: () => <div data-testid="x-axis" />,
        YAxis: () => <div data-testid="y-axis" />,
        CartesianGrid: () => <div data-testid="cartesian-grid" />,
        Tooltip: () => <div data-testid="tooltip" />,
        Legend: () => <div data-testid="legend" />,
        ReferenceArea: ({ label }: any) => (
            <div data-testid="reference-area">
                {label && typeof label === 'object' ? label.value : label}
            </div>
        ),
        ReferenceLine: ({ label }: any) => (
            <div data-testid="reference-line">
                {label && typeof label === 'object' ? label.value : label}
            </div>
        ),
    };
});
