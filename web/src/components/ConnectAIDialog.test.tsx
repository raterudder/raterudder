import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AppType, type UserApp } from '../api';
import { ConnectAIDialog } from './ConnectAIDialog';
import {
    getNextGeminiAppName,
    hasExistingGeminiApp,
} from '../utils/connectAIUtils';

describe('ConnectAIDialog helpers', () => {
    it('detects existing Gemini apps and picks incremented names', () => {
        expect(hasExistingGeminiApp([])).toBe(false);
        expect(getNextGeminiAppName([])).toBe('Gemini');

        const withGemini: UserApp[] = [
            { id: '1', siteID: 's1', type: AppType.APIKey, name: 'Gemini', tsCreated: '2026-01-01T00:00:00Z' },
        ];
        expect(hasExistingGeminiApp(withGemini)).toBe(true);
        expect(getNextGeminiAppName(withGemini)).toBe('Gemini 2');

        const withGemini2: UserApp[] = [
            ...withGemini,
            { id: '2', siteID: 's1', type: AppType.APIKey, name: 'gemini 2', tsCreated: '2026-01-02T00:00:00Z' },
        ];
        expect(getNextGeminiAppName(withGemini2)).toBe('Gemini 3');
    });
});

describe('ConnectAIDialog', () => {
    const mockOnCreateApp = vi.fn();
    const mockOnOpenChange = vi.fn();

    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('shows the combined MCP option by default with the MCP prompt', () => {
        render(
            <ConnectAIDialog
                open={true}
                onOpenChange={mockOnOpenChange}
                apps={[]}
                onCreateApp={mockOnCreateApp}
            />
        );

        expect(screen.getByTestId('connect-ai-option-mcp')).toHaveAttribute('aria-pressed', 'true');
        expect(screen.getByTestId('connect-ai-mcp-prompt')).toHaveTextContent('/mcp');
        expect(screen.getByText(/Claude, ChatGPT, Antigravity, Muse/i)).toBeInTheDocument();
    });

    it('creates a Gemini key and embeds the summary URL in a reusable-skill prompt', async () => {
        const user = userEvent.setup();
        mockOnCreateApp.mockResolvedValue({
            token: 'rr_gemini_tok_1',
            app: { id: 'app-g1', siteID: 'site1', type: AppType.APIKey, name: 'Gemini', tsCreated: '2026-01-01T00:00:00Z' },
        });

        render(
            <ConnectAIDialog
                open={true}
                onOpenChange={mockOnOpenChange}
                apps={[]}
                onCreateApp={mockOnCreateApp}
            />
        );

        await user.click(screen.getByTestId('connect-ai-option-gemini'));
        expect(screen.queryByTestId('gemini-duplicate-warning')).not.toBeInTheDocument();

        const createBtn = screen.getByTestId('create-gemini-key-btn');
        expect(createBtn).toHaveTextContent('Create "Gemini" Key & Build Prompt');
        await user.click(createBtn);

        await waitFor(() => {
            expect(mockOnCreateApp).toHaveBeenCalledWith('Gemini');
            expect(screen.getByTestId('gemini-created-banner')).toHaveTextContent('Created key Gemini');
            const prompt = screen.getByTestId('connect-ai-gemini-prompt');
            expect(prompt).toHaveTextContent(/Create a reusable skill/i);
            expect(prompt).toHaveTextContent('/api/agent/summary?token=rr_gemini_tok_1');
        });
    });

    it('warns before creating a duplicate Gemini key and creates Gemini 2', async () => {
        const user = userEvent.setup();
        const existingApps: UserApp[] = [
            { id: 'app-g1', siteID: 'site1', type: AppType.APIKey, name: 'Gemini', tsCreated: '2026-01-01T00:00:00Z' },
        ];
        mockOnCreateApp.mockResolvedValue({
            token: 'rr_gemini_tok_2',
            app: { id: 'app-g2', siteID: 'site1', type: AppType.APIKey, name: 'Gemini 2', tsCreated: '2026-01-02T00:00:00Z' },
        });

        render(
            <ConnectAIDialog
                open={true}
                onOpenChange={mockOnOpenChange}
                apps={existingApps}
                onCreateApp={mockOnCreateApp}
            />
        );

        await user.click(screen.getByTestId('connect-ai-option-gemini'));

        const warning = screen.getByTestId('gemini-duplicate-warning');
        expect(warning).toHaveTextContent(/already have a Gemini key connected/i);
        expect(warning).toHaveTextContent(/Gemini 2/i);

        const createBtn = screen.getByTestId('create-gemini-key-btn');
        expect(createBtn).toHaveTextContent('Create "Gemini 2" Key & Build Prompt');
        await user.click(createBtn);

        await waitFor(() => {
            expect(mockOnCreateApp).toHaveBeenCalledWith('Gemini 2');
            expect(screen.getByTestId('gemini-created-banner')).toHaveTextContent('Created key Gemini 2');
            expect(screen.getByTestId('connect-ai-gemini-prompt')).toHaveTextContent('/api/agent/summary?token=rr_gemini_tok_2');
        });
    });

    it('starts Other Assistant with MCP and supports the named Summary URL fallback', async () => {
        const user = userEvent.setup();
        mockOnCreateApp.mockResolvedValue({
            token: 'rr_other_tok_1',
            app: { id: 'app-o1', siteID: 'site1', type: AppType.APIKey, name: 'Perplexity', tsCreated: '2026-01-03T00:00:00Z' },
        });

        render(
            <ConnectAIDialog
                open={true}
                onOpenChange={mockOnOpenChange}
                apps={[]}
                onCreateApp={mockOnCreateApp}
            />
        );

        await user.click(screen.getByTestId('connect-ai-option-other'));
        expect(screen.getByTestId('connect-ai-other-mcp-prompt')).toHaveTextContent('/mcp');

        await user.click(screen.getByTestId('other-fallback-toggle'));
        const nameInput = screen.getByLabelText(/App Name/i);
        await user.type(nameInput, 'Perplexity');
        await user.click(screen.getByTestId('create-other-key-btn'));

        await waitFor(() => {
            expect(mockOnCreateApp).toHaveBeenCalledWith('Perplexity');
            expect(screen.getByTestId('other-created-banner')).toHaveTextContent('Created key Perplexity');
            expect(screen.getByTestId('connect-ai-other-url-prompt')).toHaveTextContent('/api/agent/summary?token=rr_other_tok_1');
        });
    });

    it('shows an error message if creating a key fails', async () => {
        const user = userEvent.setup();
        mockOnCreateApp.mockRejectedValue(new Error('Quota exceeded'));

        render(
            <ConnectAIDialog
                open={true}
                onOpenChange={mockOnOpenChange}
                apps={[]}
                onCreateApp={mockOnCreateApp}
            />
        );

        await user.click(screen.getByTestId('connect-ai-option-gemini'));
        await user.click(screen.getByTestId('create-gemini-key-btn'));

        expect(await screen.findByText('Quota exceeded')).toBeInTheDocument();
    });
});
