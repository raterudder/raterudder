import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import JoinSitePage from './JoinSitePage';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as api from '../api';

vi.mock('../api', () => ({
    joinSite: vi.fn(),
}));

describe('JoinSitePage Component', () => {
    const mockOnJoinSuccess = vi.fn();

    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders the form', () => {
        render(<JoinSitePage onJoinSuccess={mockOnJoinSuccess} />);

        expect(screen.getByText('Join a Site')).toBeInTheDocument();
        expect(screen.getByLabelText('Site ID')).toBeInTheDocument();
        expect(screen.getByLabelText('Site Name')).toBeInTheDocument();
        expect(screen.getByLabelText('Invite Code')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Join Site' })).toBeInTheDocument();

        const termsLink = screen.getByRole('link', { name: 'Terms of Service' });
        const privacyLink = screen.getByRole('link', { name: 'Privacy Policy' });
        expect(termsLink).toHaveAttribute('href', '/terms');
        expect(termsLink).toHaveAttribute('target', '_blank');
        expect(privacyLink).toHaveAttribute('href', '/privacy');
        expect(privacyLink).toHaveAttribute('target', '_blank');
    });

    it('disables submit when fields are empty', () => {
        render(<JoinSitePage onJoinSuccess={mockOnJoinSuccess} />);

        const button = screen.getByRole('button', { name: 'Join Site' });
        expect(button).toBeDisabled();
    });

    it('enables submit when all fields are filled', () => {
        render(<JoinSitePage onJoinSuccess={mockOnJoinSuccess} />);

        fireEvent.change(screen.getByLabelText('Site ID'), { target: { value: 'my-site' } });
        fireEvent.change(screen.getByLabelText('Site Name'), { target: { value: 'My Home' } });
        fireEvent.change(screen.getByLabelText('Invite Code'), { target: { value: 'secret' } });

        const button = screen.getByRole('button', { name: 'Join Site' });
        expect(button).not.toBeDisabled();
    });

    it('calls joinSite and onJoinSuccess on successful submit', async () => {
        (api.joinSite as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);

        render(<JoinSitePage onJoinSuccess={mockOnJoinSuccess} />);

        fireEvent.change(screen.getByLabelText('Site ID'), { target: { value: 'my-site' } });
        fireEvent.change(screen.getByLabelText('Site Name'), { target: { value: 'My Home' } });
        fireEvent.change(screen.getByLabelText('Invite Code'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Join Site' }));

        await waitFor(() => {
            expect(api.joinSite).toHaveBeenCalledWith('my-site', 'secret', 'My Home');
            expect(mockOnJoinSuccess).toHaveBeenCalled();
        });
    });

    it('displays error on failed submit', async () => {
        (api.joinSite as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('invalid invite code'));

        render(<JoinSitePage onJoinSuccess={mockOnJoinSuccess} />);

        fireEvent.change(screen.getByLabelText('Site ID'), { target: { value: 'my-site' } });
        fireEvent.change(screen.getByLabelText('Site Name'), { target: { value: 'My Home' } });
        fireEvent.change(screen.getByLabelText('Invite Code'), { target: { value: 'wrong' } });
        fireEvent.click(screen.getByRole('button', { name: 'Join Site' }));

        await waitFor(() => {
            expect(screen.getByText('invalid invite code')).toBeInTheDocument();
        });
        expect(mockOnJoinSuccess).not.toHaveBeenCalled();
    });

    it('trims whitespace from inputs', async () => {
        (api.joinSite as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);

        render(<JoinSitePage onJoinSuccess={mockOnJoinSuccess} />);

        fireEvent.change(screen.getByLabelText('Site ID'), { target: { value: '  my-site  ' } });
        fireEvent.change(screen.getByLabelText('Site Name'), { target: { value: '  My Home  ' } });
        fireEvent.change(screen.getByLabelText('Invite Code'), { target: { value: '  secret  ' } });
        fireEvent.click(screen.getByRole('button', { name: 'Join Site' }));

        await waitFor(() => {
            expect(api.joinSite).toHaveBeenCalledWith('my-site', 'secret', 'My Home');
        });
    });
});
