import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { TermsAcceptanceModal } from './TermsAcceptanceModal';
import * as api from '../api';

vi.mock('../api', () => ({
    acceptTerms: vi.fn(),
}));

describe('TermsAcceptanceModal', () => {
    beforeEach(() => {
        vi.resetAllMocks();
    });

    it('does not render when closed', () => {
        render(
            <TermsAcceptanceModal
                open={false}
                onAccepted={vi.fn()}
                onLogout={vi.fn()}
            />
        );

        expect(screen.queryByTestId('terms-acceptance-modal')).not.toBeInTheDocument();
    });

    it('renders summary, policy links, and calls acceptTerms on Agree & Continue', async () => {
        const user = userEvent.setup();
        const onAccepted = vi.fn();
        const onLogout = vi.fn();
        (api.acceptTerms as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);

        render(
            <TermsAcceptanceModal
                open={true}
                onAccepted={onAccepted}
                onLogout={onLogout}
            />
        );

        expect(screen.getByText('Updated Terms & Privacy Policy')).toBeInTheDocument();

        const termsLink = screen.getByRole('link', { name: 'Terms of Service' });
        const privacyLink = screen.getByRole('link', { name: 'Privacy Policy' });
        expect(termsLink).toHaveAttribute('href', '/terms');
        expect(termsLink).toHaveAttribute('target', '_blank');
        expect(privacyLink).toHaveAttribute('href', '/privacy');
        expect(privacyLink).toHaveAttribute('target', '_blank');
        expect(screen.getByRole('link', { name: 'privacy@raterudder.com' })).toHaveAttribute('href', 'mailto:privacy@raterudder.com');

        await user.click(screen.getByRole('button', { name: 'Agree & Continue' }));

        await waitFor(() => {
            expect(api.acceptTerms).toHaveBeenCalledTimes(1);
            expect(onAccepted).toHaveBeenCalledTimes(1);
        });
    });

    it('displays error message when acceptTerms fails', async () => {
        const user = userEvent.setup();
        const onAccepted = vi.fn();
        (api.acceptTerms as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Network error'));

        render(
            <TermsAcceptanceModal
                open={true}
                onAccepted={onAccepted}
                onLogout={vi.fn()}
            />
        );

        await user.click(screen.getByRole('button', { name: 'Agree & Continue' }));

        expect(await screen.findByRole('alert')).toHaveTextContent('Network error');
        expect(onAccepted).not.toHaveBeenCalled();
    });

    it('calls onLogout when Log Out is clicked', async () => {
        const user = userEvent.setup();
        const onLogout = vi.fn();

        render(
            <TermsAcceptanceModal
                open={true}
                onAccepted={vi.fn()}
                onLogout={onLogout}
            />
        );

        await user.click(screen.getByRole('button', { name: 'Log Out' }));
        expect(onLogout).toHaveBeenCalledTimes(1);
    });
});
