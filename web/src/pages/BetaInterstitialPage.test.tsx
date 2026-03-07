import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import BetaInterstitialPage from './BetaInterstitialPage';
import { describe, it, expect, vi } from 'vitest';
import { useLocation } from 'wouter';

// Mock wouter
vi.mock('wouter', () => ({
    useLocation: vi.fn(),
}));

describe('BetaInterstitialPage Component', () => {
    it('renders the initial state', () => {
        (useLocation as ReturnType<typeof vi.fn>).mockReturnValue(['/welcome', vi.fn()]);
        render(<BetaInterstitialPage />);

        expect(screen.getByText('RateRudder Beta')).toBeInTheDocument();
        expect(screen.getByText('Utility Provider')).toBeInTheDocument();
        expect(screen.getByText('Battery System')).toBeInTheDocument();

        // Neither waitlist nor continue button should be present initially
        expect(screen.queryByText(/We're currently in a limited beta/)).not.toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /start saving money/i })).not.toBeInTheDocument();
    });

    it('shows waitlist message when unsupported utility is selected', async () => {
        const user = userEvent.setup();
        (useLocation as ReturnType<typeof vi.fn>).mockReturnValue(['/welcome', vi.fn()]);
        render(<BetaInterstitialPage />);

        const utilitySelect = screen.getByRole('combobox', { name: /Utility Provider/i });
        await user.click(utilitySelect);

        const otherOption = screen.getByRole('option', { name: 'Other' });
        await user.click(otherOption);

        await waitFor(() => {
            expect(screen.getByText(/We're currently in a limited beta/)).toBeInTheDocument();
            // Since VITE_JOIN_FORM_URL is likely undefined in tests unless mocked, we just check the message
            expect(screen.getByText(/Please express your interest/i)).toBeInTheDocument();
        });
    });

    it('shows continue button when supported equipment is selected', async () => {
        const user = userEvent.setup();
        (useLocation as ReturnType<typeof vi.fn>).mockReturnValue(['/welcome', vi.fn()]);
        render(<BetaInterstitialPage />);

        const utilitySelect = screen.getByRole('combobox', { name: /Utility Provider/i });
        await user.click(utilitySelect);
        const amerenOption = screen.getByRole('option', { name: 'Ameren' });
        await user.click(amerenOption);

        const batterySelect = screen.getByRole('combobox', { name: /Battery System/i });
        await user.click(batterySelect);
        const franklinOption = screen.getByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /start saving money/i })).toBeInTheDocument();
            expect(screen.queryByText(/We're currently in a limited beta/)).not.toBeInTheDocument();
        });
    });

    it('navigates to /new-site when continue button is clicked', async () => {
        const user = userEvent.setup();
        const navigateMock = vi.fn();
        (useLocation as ReturnType<typeof vi.fn>).mockReturnValue(['/welcome', navigateMock]);
        render(<BetaInterstitialPage />);

        const utilitySelect = screen.getByRole('combobox', { name: /Utility Provider/i });
        await user.click(utilitySelect);
        const amerenOption = screen.getByRole('option', { name: 'Ameren' });
        await user.click(amerenOption);

        const batterySelect = screen.getByRole('combobox', { name: /Battery System/i });
        await user.click(batterySelect);
        const franklinOption = screen.getByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        const continueButton = await screen.findByRole('button', { name: /start saving money/i });
        await user.click(continueButton);

        expect(navigateMock).toHaveBeenCalledWith('/new-site');
    });
});
