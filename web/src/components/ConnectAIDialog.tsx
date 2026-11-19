import { useEffect, useState } from 'react';
import { Dialog } from '@base-ui/react/dialog';
import { Button } from '@base-ui/react/button';
import { Field } from '@base-ui/react/field';
import { Input } from '@base-ui/react/input';
import type { UserApp } from '../api';
import {
    buildGeminiSummaryPrompt,
    buildMcpPrompt,
    buildOtherSummaryPrompt,
    buildSummaryUrl,
    getNextGeminiAppName,
    hasExistingGeminiApp,
} from '../utils/connectAIUtils';
import './ConnectAIDialog.css';

export type AssistantOption = 'mcp' | 'gemini' | 'other';

interface GeneratedKeyPrompt {
    name: string;
    summaryUrl: string;
}

export interface ConnectAIDialogProps {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    apps: UserApp[];
    onCreateApp: (name: string) => Promise<{ token: string; app: UserApp }>;
}

const ASSISTANT_OPTIONS: { id: AssistantOption; title: string; subtitle: string }[] = [
    {
        id: 'mcp',
        title: 'Claude, ChatGPT, Antigravity, Muse',
        subtitle: 'MCP via OAuth (Recommended)',
    },
    {
        id: 'gemini',
        title: 'Gemini',
        subtitle: 'Private Summary URL',
    },
    {
        id: 'other',
        title: 'Other Assistant',
        subtitle: 'MCP or Summary URL',
    },
];

export const ConnectAIDialog = ({
    open,
    onOpenChange,
    apps,
    onCreateApp,
}: ConnectAIDialogProps) => {
    const [selectedOption, setSelectedOption] = useState<AssistantOption>('mcp');
    const [otherFallbackOpen, setOtherFallbackOpen] = useState(false);
    const [otherAppName, setOtherAppName] = useState('');
    const [geminiGenerated, setGeminiGenerated] = useState<GeneratedKeyPrompt | null>(null);
    const [otherGenerated, setOtherGenerated] = useState<GeneratedKeyPrompt | null>(null);
    const [isCreating, setIsCreating] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [copiedKey, setCopiedKey] = useState<string | null>(null);

    useEffect(() => {
        if (!open) {
            setSelectedOption('mcp');
            setOtherFallbackOpen(false);
            setOtherAppName('');
            setGeminiGenerated(null);
            setOtherGenerated(null);
            setIsCreating(false);
            setError(null);
            setCopiedKey(null);
        }
    }, [open]);

    const origin = window.location.origin;
    const mcpUrl = `${origin}/mcp`;
    const mcpPrompt = buildMcpPrompt(origin);
    const geminiExists = hasExistingGeminiApp(apps);
    const nextGeminiName = getNextGeminiAppName(apps);

    const handleCopy = async (key: string, text: string) => {
        if (!navigator.clipboard?.writeText) return;
        try {
            await navigator.clipboard.writeText(text);
            setCopiedKey(key);
            setTimeout(() => {
                setCopiedKey(prev => (prev === key ? null : prev));
            }, 2000);
        } catch {
            setCopiedKey(null);
        }
    };

    const handleCreateKey = async (target: 'gemini' | 'other', name: string) => {
        const trimmedName = name.trim();
        if (!trimmedName || isCreating) return;
        setIsCreating(true);
        setError(null);
        try {
            const res = await onCreateApp(trimmedName);
            const summaryUrl = buildSummaryUrl(origin, res.token);
            const generated = { name: res.app.name || trimmedName, summaryUrl };
            if (target === 'gemini') {
                setGeminiGenerated(generated);
            } else {
                setOtherGenerated(generated);
                setOtherAppName('');
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to create API key');
        } finally {
            setIsCreating(false);
        }
    };

    return (
        <Dialog.Root open={open} onOpenChange={onOpenChange}>
            <Dialog.Portal>
                <Dialog.Backdrop className="dialog-backdrop" />
                <Dialog.Popup className="dialog-popup connect-ai-dialog-popup" data-testid="connect-ai-dialog">
                    <div className="connect-ai-header">
                        <Dialog.Title className="dialog-title">Connect an AI Assistant</Dialog.Title>
                        <Dialog.Description className="dialog-description">
                            Choose your assistant to get a ready-to-paste setup prompt.
                        </Dialog.Description>
                    </div>

                    <div className="connect-ai-options" role="group" aria-label="Choose an AI assistant">
                        {ASSISTANT_OPTIONS.map(option => {
                            const active = selectedOption === option.id;
                            return (
                                <Button
                                    key={option.id}
                                    type="button"
                                    className={`connect-ai-option-card ${active ? 'active' : ''}`}
                                    aria-pressed={active}
                                    data-testid={`connect-ai-option-${option.id}`}
                                    onClick={() => {
                                        setSelectedOption(option.id);
                                        setError(null);
                                        if (option.id !== 'other') {
                                            setOtherFallbackOpen(false);
                                        }
                                    }}
                                >
                                    <span className="connect-ai-option-title">{option.title}</span>
                                    <span className="connect-ai-option-subtitle">{option.subtitle}</span>
                                </Button>
                            );
                        })}
                    </div>

                    {error && (
                        <div className="error-message" style={{ marginTop: 0, marginBottom: '0.875rem' }}>
                            {error}
                        </div>
                    )}

                    <div className="connect-ai-panel">
                        {selectedOption === 'mcp' && (
                            <>
                                <div className="connect-ai-info-row">
                                    <span className="connect-ai-badge">MCP + OAuth</span>
                                    <span>
                                        No API key needed — your assistant will prompt you to sign in to RateRudder.
                                    </span>
                                </div>
                                <div className="connect-ai-prompt-box" data-testid="connect-ai-mcp-prompt">
                                    {mcpPrompt}
                                </div>
                                <div className="connect-ai-actions">
                                    <Button
                                        type="button"
                                        className="btn btn-primary"
                                        onClick={() => {
                                            void handleCopy('mcp-prompt', mcpPrompt);
                                        }}
                                    >
                                        {copiedKey === 'mcp-prompt' ? 'Copied!' : 'Copy Prompt'}
                                    </Button>
                                    <Button
                                        type="button"
                                        className="connect-ai-link-btn"
                                        onClick={() => {
                                            void handleCopy('mcp-url', mcpUrl);
                                        }}
                                    >
                                        {copiedKey === 'mcp-url' ? 'Copied MCP URL!' : 'Copy MCP URL only'}
                                    </Button>
                                </div>
                            </>
                        )}

                        {selectedOption === 'gemini' && !geminiGenerated && (
                            <>
                                <div className="connect-ai-info-row">
                                    <span className="connect-ai-badge warning">Summary URL</span>
                                    <span>
                                        Gemini doesn&apos;t support remote MCP servers yet. Create a read-only key and
                                        we&apos;ll build a reusable-skill prompt with your private Summary URL.
                                    </span>
                                </div>
                                {geminiExists && (
                                    <div className="connect-ai-warning-box" data-testid="gemini-duplicate-warning">
                                        You already have a <strong>Gemini</strong> key connected. Creating another will
                                        add a new key named <strong>{nextGeminiName}</strong> (or revoke the old one in
                                        Connected Apps).
                                    </div>
                                )}
                                <div className="connect-ai-actions">
                                    <Button
                                        type="button"
                                        className="btn btn-primary"
                                        disabled={isCreating}
                                        data-testid="create-gemini-key-btn"
                                        onClick={() => {
                                            void handleCreateKey('gemini', nextGeminiName);
                                        }}
                                    >
                                        {isCreating
                                            ? 'Creating...'
                                            : `Create "${nextGeminiName}" Key & Build Prompt`}
                                    </Button>
                                </div>
                            </>
                        )}

                        {selectedOption === 'gemini' && geminiGenerated && (
                            <>
                                <div className="connect-ai-warning-box" data-testid="gemini-created-banner">
                                    Created key <strong>{geminiGenerated.name}</strong> — copy this prompt now; the
                                    private URL won&apos;t be shown again.
                                </div>
                                <div className="connect-ai-prompt-box" data-testid="connect-ai-gemini-prompt">
                                    {buildGeminiSummaryPrompt(geminiGenerated.summaryUrl)}
                                </div>
                                <div className="connect-ai-actions">
                                    <div className="connect-ai-actions-left">
                                        <Button
                                            type="button"
                                            className="btn btn-primary"
                                            onClick={() => {
                                                void handleCopy(
                                                    'gemini-prompt',
                                                    buildGeminiSummaryPrompt(geminiGenerated.summaryUrl)
                                                );
                                            }}
                                        >
                                            {copiedKey === 'gemini-prompt' ? 'Copied!' : 'Copy Prompt for Gemini'}
                                        </Button>
                                        <Button
                                            type="button"
                                            className="connect-ai-subtle-btn"
                                            onClick={() => {
                                                setGeminiGenerated(null);
                                                setError(null);
                                            }}
                                        >
                                            Create another
                                        </Button>
                                    </div>
                                    <Button
                                        type="button"
                                        className="connect-ai-link-btn"
                                        onClick={() => {
                                            void handleCopy('gemini-url', geminiGenerated.summaryUrl);
                                        }}
                                    >
                                        {copiedKey === 'gemini-url' ? 'Copied URL!' : 'Copy Summary URL only'}
                                    </Button>
                                </div>
                            </>
                        )}

                        {selectedOption === 'other' && (
                            <>
                                <div className="connect-ai-info-row">
                                    <span className="connect-ai-badge">Try MCP First</span>
                                    <span>Most modern AI agents support remote MCP via OAuth.</span>
                                </div>
                                <div className="connect-ai-prompt-box" data-testid="connect-ai-other-mcp-prompt">
                                    {mcpPrompt}
                                </div>
                                <div className="connect-ai-actions">
                                    <Button
                                        type="button"
                                        className="btn btn-primary"
                                        onClick={() => {
                                            void handleCopy('other-mcp-prompt', mcpPrompt);
                                        }}
                                    >
                                        {copiedKey === 'other-mcp-prompt' ? 'Copied!' : 'Copy MCP Prompt'}
                                    </Button>
                                    <Button
                                        type="button"
                                        className="connect-ai-link-btn"
                                        onClick={() => {
                                            void handleCopy('other-mcp-url', mcpUrl);
                                        }}
                                    >
                                        {copiedKey === 'other-mcp-url' ? 'Copied MCP URL!' : 'Copy MCP URL only'}
                                    </Button>
                                </div>

                                <div className="connect-ai-fallback">
                                    {!otherFallbackOpen && !otherGenerated && (
                                        <Button
                                            type="button"
                                            className="connect-ai-link-btn"
                                            data-testid="other-fallback-toggle"
                                            onClick={() => setOtherFallbackOpen(true)}
                                        >
                                            Assistant doesn&apos;t support MCP? Generate a Summary URL instead &rarr;
                                        </Button>
                                    )}

                                    {otherFallbackOpen && !otherGenerated && (
                                        <>
                                            <div style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--on-surface)' }}>
                                                Fallback: Generate a read-only Summary URL
                                            </div>
                                            <div className="connect-ai-fallback-form">
                                                <Field.Root className="form-group compact connect-ai-fallback-field">
                                                    <Field.Label htmlFor="otherAgentAppName">App Name</Field.Label>
                                                    <Input
                                                        id="otherAgentAppName"
                                                        type="text"
                                                        placeholder="e.g. Perplexity"
                                                        maxLength={50}
                                                        value={otherAppName}
                                                        onChange={e => setOtherAppName(e.target.value)}
                                                    />
                                                </Field.Root>
                                                <Button
                                                    type="button"
                                                    className="btn btn-primary"
                                                    disabled={isCreating || !otherAppName.trim()}
                                                    data-testid="create-other-key-btn"
                                                    onClick={() => {
                                                        void handleCreateKey('other', otherAppName);
                                                    }}
                                                >
                                                    {isCreating ? 'Creating...' : 'Create Key & Prompt'}
                                                </Button>
                                            </div>
                                        </>
                                    )}

                                    {otherGenerated && (
                                        <>
                                            <div className="connect-ai-warning-box" data-testid="other-created-banner">
                                                Created key <strong>{otherGenerated.name}</strong> — copy this prompt
                                                now; the private URL won&apos;t be shown again.
                                            </div>
                                            <div className="connect-ai-prompt-box" data-testid="connect-ai-other-url-prompt">
                                                {buildOtherSummaryPrompt(otherGenerated.summaryUrl)}
                                            </div>
                                            <div className="connect-ai-actions">
                                                <div className="connect-ai-actions-left">
                                                    <Button
                                                        type="button"
                                                        className="btn btn-primary"
                                                        onClick={() => {
                                                            void handleCopy(
                                                                'other-url-prompt',
                                                                buildOtherSummaryPrompt(otherGenerated.summaryUrl)
                                                            );
                                                        }}
                                                    >
                                                        {copiedKey === 'other-url-prompt' ? 'Copied!' : 'Copy URL Prompt'}
                                                    </Button>
                                                    <Button
                                                        type="button"
                                                        className="connect-ai-subtle-btn"
                                                        onClick={() => {
                                                            setOtherGenerated(null);
                                                            setOtherFallbackOpen(true);
                                                            setError(null);
                                                        }}
                                                    >
                                                        Create another
                                                    </Button>
                                                </div>
                                                <Button
                                                    type="button"
                                                    className="connect-ai-link-btn"
                                                    onClick={() => {
                                                        void handleCopy('other-url', otherGenerated.summaryUrl);
                                                    }}
                                                >
                                                    {copiedKey === 'other-url' ? 'Copied URL!' : 'Copy Summary URL only'}
                                                </Button>
                                            </div>
                                        </>
                                    )}
                                </div>
                            </>
                        )}
                    </div>

                    <div className="connect-ai-footer">
                        <Dialog.Close className="btn btn-secondary" type="button">
                            Done
                        </Dialog.Close>
                    </div>
                </Dialog.Popup>
            </Dialog.Portal>
        </Dialog.Root>
    );
};
