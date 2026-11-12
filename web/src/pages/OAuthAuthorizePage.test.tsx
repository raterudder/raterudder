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

    it('renders unverified warning for DCR client and approves authorization', async () => {
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
            redirectURI: 'http://127.0.0.1:9999/cb',
            verified: false,
        });
        (api.approveOAuthClient as any).mockResolvedValue({
            redirectURL: 'http://127.0.0.1:9999/cb?code=rro_123&state=xyz',
        });

        render(<App />);

        expect(await screen.findByText(/Connect Claude Desktop/i)).toBeInTheDocument();
        const warning = screen.getByTestId('oauth-unverified-warning');
        expect(warning).toHaveTextContent(/Unverified application/i);
        expect(warning).toHaveTextContent('127.0.0.1:9999');
        expect(screen.queryByTestId('oauth-client-domain')).not.toBeInTheDocument();

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

    it('renders domain for CIMD client without warnings when redirect origin matches', async () => {
        const cimdURL = 'https://claude.ai/oauth/client-metadata.json';
        window.history.pushState(
            null,
            '',
            `/oauth/authorize?client_id=${encodeURIComponent(cimdURL)}&redirect_uri=${encodeURIComponent('https://claude.ai/api/mcp/auth_callback')}&code_challenge=ch_abc&code_challenge_method=S256`
        );

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [{ id: 'site1', name: 'Home Site' }],
        });
        (api.fetchOAuthClientInfo as any).mockResolvedValue({
            clientName: 'Claude',
            redirectURI: 'https://claude.ai/api/mcp/auth_callback',
            clientDomain: 'claude.ai',
            verified: true,
        });

        render(<App />);

        expect(await screen.findByText(/Connect Claude/i)).toBeInTheDocument();
        const domainBadge = screen.getByTestId('oauth-client-domain');
        expect(domainBadge).toHaveTextContent('Domain: claude.ai');
        expect(screen.queryByTestId('oauth-unverified-warning')).not.toBeInTheDocument();
        expect(screen.queryByTestId('oauth-redirect-mismatch-warning')).not.toBeInTheDocument();
    });

    it('warns when CIMD client redirect_uri origin differs from client domain', async () => {
        const cimdURL = 'https://app.example.com/oauth/client.json';
        window.history.pushState(
            null,
            '',
            `/oauth/authorize?client_id=${encodeURIComponent(cimdURL)}&redirect_uri=${encodeURIComponent('https://other.example.net:8443/cb')}&code_challenge=ch_abc&code_challenge_method=S256`
        );

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            sites: [{ id: 'site1', name: 'Home Site' }],
        });
        (api.fetchOAuthClientInfo as any).mockResolvedValue({
            clientName: 'Example App',
            redirectURI: 'https://other.example.net:8443/cb',
            clientDomain: 'app.example.com',
            verified: true,
        });

        render(<App />);

        expect(await screen.findByText(/Connect Example App/i)).toBeInTheDocument();
        expect(screen.getByTestId('oauth-client-domain')).toHaveTextContent('Domain: app.example.com');
        const mismatchWarning = screen.getByTestId('oauth-redirect-mismatch-warning');
        expect(mismatchWarning).toHaveTextContent('app.example.com');
        expect(mismatchWarning).toHaveTextContent('other.example.net:8443');
    });

    it('ignores viewSite query parameter on /oauth/authorize for multi-site admins', async () => {
        const cimdURL = 'https://claude.ai/oauth/client-metadata.json';
        window.history.pushState(
            null,
            '',
            `/oauth/authorize?viewSite=victim-site&client_id=${encodeURIComponent(cimdURL)}&redirect_uri=${encodeURIComponent('https://claude.ai/api/mcp/auth_callback')}&code_challenge=ch_abc&code_challenge_method=S256`
        );

        (api.fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: true,
            multiSiteAdmin: true,
            sites: [{ id: 'admin-own-site', name: 'Admin Home' }],
        });
        (api.fetchOAuthClientInfo as any).mockResolvedValue({
            clientName: 'Claude',
            redirectURI: 'https://claude.ai/api/mcp/auth_callback',
            clientDomain: 'claude.ai',
            verified: true,
        });
        (api.approveOAuthClient as any).mockResolvedValue({
            redirectURL: 'https://claude.ai/api/mcp/auth_callback?code=rro_123',
        });

        render(<App />);

        expect(await screen.findByText(/Connect Claude/i)).toBeInTheDocument();
        fireEvent.click(screen.getByTestId('oauth-approve-btn'));

        await waitFor(() => {
            expect(api.approveOAuthClient).toHaveBeenCalledWith(
                expect.objectContaining({
                    siteID: 'admin-own-site',
                })
            );
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
