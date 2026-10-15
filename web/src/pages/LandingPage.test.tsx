
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Router } from 'wouter';
import LandingPage from './LandingPage';
import App from '../App';
import { describe, it, expect, vi } from 'vitest';
import * as api from '../api';

vi.mock('../api', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../api')>();
    return {
        ...actual,
        fetchAuthStatus: vi.fn(),
    };
});

const { fetchAuthStatus } = api;

describe('LandingPage Component', () => {
    it('renders marketing copy, accuracy badges, and feature sections', () => {
        render(
            <Router>
                <LandingPage />
            </Router>
        );

        // Check for hero headline, subhead, and compact badges
        expect(screen.getByRole('heading', { level: 1, name: /Maximize Battery Savings.*Zero Effort/i })).toBeInTheDocument();
        expect(screen.getByText(/RateRudder automatically charges your battery when power is cheap/i)).toBeInTheDocument();
        expect(screen.getByText('Powerwall')).toBeInTheDocument();
        expect(screen.getByText('FranklinWH')).toBeInTheDocument();
        expect(screen.getByText('Enphase')).toBeInTheDocument();
        expect(screen.getByRole('link', { name: /50\+ Utilities/i })).toHaveAttribute('href', '/utilities');

        // Check Decision Factors and impact ribbon
        expect(screen.getByText('Decision Factors')).toBeInTheDocument();
        expect(screen.getByText('Zero-Config Solar Forecasting')).toBeInTheDocument();
        expect(screen.getByText('Adaptive Home Load Prediction')).toBeInTheDocument();
        expect(screen.getByText('Automated EV Charging Detection')).toBeInTheDocument();
        expect(screen.getByText('Direct Solar & Battery Peak Export')).toBeInTheDocument();
        expect(screen.getByText('Time-Windowed Backup Reserves')).toBeInTheDocument();
        expect(screen.getByText('Over 65%')).toBeInTheDocument();

        // Check for smart charging feature text and new feature cards
        expect(screen.getByText((content) => content.startsWith('RateRudder learns your home'))).toBeInTheDocument();
        expect(screen.getByText('100% Hands-Free Automation')).toBeInTheDocument();
        expect(screen.getByText('Proactive Smart Notifications')).toBeInTheDocument();
        expect(screen.getByText('Free & Open Source')).toBeInTheDocument();

        // Check for Daily Optimization and comparison callouts
        expect(screen.getByText('Daily Optimization')).toBeInTheDocument();
        expect(screen.getByText('✓ With RateRudder')).toBeInTheDocument();
        expect(screen.getByText('✕ Self-Consumption & Manual Rules')).toBeInTheDocument();

        // Check for FAQ section and specific questions
        expect(screen.getByText('Common Questions')).toBeInTheDocument();
        expect(screen.getByText('Does using RateRudder void my battery warranty?')).toBeInTheDocument();
        expect(screen.getByText("What happens if there's a storm or blackout?")).toBeInTheDocument();
    });

    it('cycles through spotlight carousel slides and updates phase comparisons', async () => {
        const user = userEvent.setup();
        render(
            <Router>
                <LandingPage />
            </Router>
        );

        // Initial slide is the +65% savings boost
        expect(screen.getByText('Squeeze 65%+ More Out of the Battery You Already Own')).toBeInTheDocument();

        // Click next slide -> 144% bill elimination
        await user.click(screen.getByRole('button', { name: 'Next highlight' }));
        expect(screen.getByText('Turning a $116 Electric Bill Into a $50 Utility Credit')).toBeInTheDocument();

        // Click next slide -> 95% / 90% forecast accuracy
        await user.click(screen.getByRole('button', { name: 'Next highlight' }));
        expect(screen.getByText('Auto-Calibrated to Your Roof, Shading & Household Routine')).toBeInTheDocument();

        // Switch Daily Optimization phase to Evening Grid Offset
        await user.click(screen.getByRole('button', { name: /Evening Grid Offset/i }));
        expect(screen.getByText(/lower 5% reserve strictly for the 5-9 PM peak/i)).toBeInTheDocument();
    });

    it('does not have a login CTA button', () => {
        render(
            <Router>
                <LandingPage />
            </Router>
        );

        expect(screen.queryByText('Login / Dashboard')).not.toBeInTheDocument();
    });

    it('does not call fetchAuthStatus on initial landing page load', async () => {
        render(<App />);
        expect(fetchAuthStatus).not.toHaveBeenCalled();
    });

    it('shows limited beta badge and fleet MWh stats on raterudder.com and staging.raterudder.com', async () => {
        const user = userEvent.setup();
        const originalLocation = window.location;

        for (const host of ['raterudder.com', 'staging.raterudder.com']) {
            const locationProxy = new Proxy(originalLocation, {
                get(target, prop) {
                    if (prop === 'hostname') return host;
                    return (target as any)[prop];
                },
            });
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: locationProxy,
            });

            const { unmount } = render(
                <Router>
                    <LandingPage />
                </Router>
            );
            expect(screen.getByText('Limited Beta Now Open')).toBeInTheDocument();
            expect(screen.getByText('1.25+ MWh')).toBeInTheDocument();
            await user.click(screen.getByRole('tab', { name: /1\.25\+ MWh/i }));
            expect(screen.getByText('65+ MWh')).toBeInTheDocument();
            unmount();
        }

        Object.defineProperty(window, 'location', {
            configurable: true,
            value: originalLocation,
        });
    });

    it('hides limited beta badge and fleet MWh stats on localhost domain', () => {
        const originalLocation = window.location;
        const locationProxy = new Proxy(originalLocation, {
            get(target, prop) {
                if (prop === 'hostname') return 'localhost';
                return (target as any)[prop];
            },
        });
        Object.defineProperty(window, 'location', {
            configurable: true,
            value: locationProxy,
        });

        try {
            render(
                <Router>
                    <LandingPage />
                </Router>
            );
            expect(screen.queryByText('Limited Beta Now Open')).not.toBeInTheDocument();
            expect(screen.queryByText('1.25+ MWh')).not.toBeInTheDocument();
            expect(screen.queryByText('65+ MWh')).not.toBeInTheDocument();
            expect(screen.getByText('100% Free')).toBeInTheDocument();
        } finally {
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: originalLocation,
            });
        }
    });
});


