
import { render, screen } from '@testing-library/react';
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
    it('renders marketing copy', () => {
        render(
            <Router>
                <LandingPage />
            </Router>
        );

        // Check for new hero text
        expect(screen.getByText((content) => content.startsWith('RateRudder learns your home'))).toBeInTheDocument();

        // Check for new simulator and chart sections
        expect(screen.getByText('Decision Factors')).toBeInTheDocument();
        expect(screen.getByText('Daily Optimization')).toBeInTheDocument();

        // Check for FAQ section
        expect(screen.getByText('Common Questions')).toBeInTheDocument();
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
});
