import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import App from '../App';
import * as api from '../api';
import { setupDefaultApiMocks, defaultAuthStatus } from '../test/apiMocks';

vi.mock('../api');

vi.mock('@react-oauth/google', () => ({
    GoogleOAuthProvider: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    GoogleLogin: ({ onSuccess }: { onSuccess: (res: any) => void }) => (
        <button onClick={() => onSuccess({ credential: 'test-token' })}>
            Google Sign In
        </button>
    ),
}));

describe('OAuthAuthorizePage', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('renders client info and approves authorization', async () => {
        window.history.pushState(
            null,
            '',
            '/oauth/authorize?client_id=rrc_123&redirect_uri=http%3A%2F%2F127.0.0.1%3A9999%2Fcb&code_challenge=ch_abc&code_challenge_method=S256&state=xyz'
        );

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [
                { id: 'site1', name: 'Home Site' },
                { id: 'site2', name: 'Cabin Site' },
            ],
        });
        (api.fetchOAuthClientInfo as any).mockResolvedValue({
            clientName: 'Claude Desktop',
        });
        (api.approveOAuthClient as any).mockResolvedValue({
            redirectURL: 'http://127.0.0.1:9999/cb?code=rro_123&state=xyz',
        });

        render(<App />);

        expect(await screen.findByText(/Connect Claude Desktop/i)).toBeInTheDocument();

        const approveBtn = screen.getByTestId('oauth-approve-btn');
        fireEvent.click(approveBtn);

        await waitFor(() => {
            expect(api.approveOAuthClient).toHaveBeenCalledWith({
                siteID: 'site1',
                clientID: 'rrc_123',
                redirectURI: 'http://127.0.0.1:9999/cb',
                responseType: 'code',
                codeChallenge: 'ch_abc',
                codeChallengeMethod: 'S256',
                state: 'xyz',
            });
        });
    });

    it('displays error when fetchOAuthClientInfo rejects', async () => {
        window.history.pushState(null, '', '/oauth/authorize?client_id=rrc_bad');
        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
        });
        (api.fetchOAuthClientInfo as any).mockRejectedValue(new Error('invalid redirect_uri'));

        render(<App />);

        expect(await screen.findByTestId('oauth-error')).toHaveTextContent('invalid redirect_uri');
    });

    it('attempts to close window and shows close window message when cancelled', async () => {
        window.history.pushState(
            null,
            '',
            '/oauth/authorize?client_id=rrc_123&redirect_uri=http%3A%2F%2F127.0.0.1%3A9999%2Fcb&code_challenge=ch_abc&code_challenge_method=S256'
        );

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [{ id: 'site1', name: 'Home Site' }],
        });
        (api.fetchOAuthClientInfo as any).mockResolvedValue({
            clientName: 'Claude Desktop',
        });

        const closeSpy = vi.spyOn(window, 'close').mockImplementation(() => {});

        render(<App />);

        expect(await screen.findByText(/Connect Claude Desktop/i)).toBeInTheDocument();

        const cancelBtn = screen.getByRole('button', { name: /Cancel/i });
        fireEvent.click(cancelBtn);

        expect(closeSpy).toHaveBeenCalledOnce();
        expect(screen.getByTestId('oauth-close-window-message')).toHaveTextContent('You may now close this window.');

        closeSpy.mockRestore();
    });
});
