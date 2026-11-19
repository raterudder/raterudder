import React from 'react';

const RELOAD_STORAGE_KEY = 'raterudder:chunk-preload-reload';
const RELOAD_COOLDOWN_MS = 15_000;

function isChunkLoadError(error: unknown): boolean {
    if (!error) return false;
    const message = error instanceof Error ? error.message : String(error);
    const name = error instanceof Error ? error.name : '';
    return (
        name === 'ChunkLoadError' ||
        /Failed to fetch dynamically imported module/i.test(message) ||
        /Importing a module script failed/i.test(message) ||
        /error loading dynamically imported module/i.test(message) ||
        /Unable to preload CSS/i.test(message) ||
        /Loading chunk [\w-]+ failed/i.test(message)
    );
}

function shouldAttemptAutoReload(pathname: string, now = Date.now()): boolean {
    try {
        const raw = window.sessionStorage.getItem(RELOAD_STORAGE_KEY);
        if (raw) {
            const parsed = JSON.parse(raw) as { path?: string; ts?: number };
            if (
                parsed &&
                parsed.path === pathname &&
                typeof parsed.ts === 'number' &&
                now - parsed.ts < RELOAD_COOLDOWN_MS
            ) {
                return false;
            }
        }
        window.sessionStorage.setItem(
            RELOAD_STORAGE_KEY,
            JSON.stringify({ path: pathname, ts: now })
        );
        return true;
    } catch {
        return false;
    }
}

export interface ChunkErrorBoundaryProps {
    children: React.ReactNode;
    onReload?: () => void;
}

interface ChunkErrorBoundaryState {
    hasError: boolean;
}

export class ChunkErrorBoundary extends React.Component<
    ChunkErrorBoundaryProps,
    ChunkErrorBoundaryState
> {
    state: ChunkErrorBoundaryState = {
        hasError: false,
    };

    private triggerReload = () => {
        if (this.props.onReload) {
            this.props.onReload();
        } else {
            window.location.reload();
        }
    };

    private handleVitePreloadError = (event: Event) => {
        event.preventDefault();
        const pathname = typeof window !== 'undefined' ? window.location.pathname : '/';
        if (shouldAttemptAutoReload(pathname)) {
            this.triggerReload();
        } else {
            this.setState({ hasError: true });
        }
    };

    componentDidMount(): void {
        if (typeof window !== 'undefined') {
            window.addEventListener('vite:preloadError', this.handleVitePreloadError);
        }
    }

    componentWillUnmount(): void {
        if (typeof window !== 'undefined') {
            window.removeEventListener('vite:preloadError', this.handleVitePreloadError);
        }
    }

    static getDerivedStateFromError(): ChunkErrorBoundaryState {
        return { hasError: true };
    }

    componentDidCatch(error: Error): void {
        if (typeof window !== 'undefined' && isChunkLoadError(error)) {
            const pathname = window.location.pathname || '/';
            if (shouldAttemptAutoReload(pathname)) {
                this.triggerReload();
            }
        }
    }

    render(): React.ReactNode {
        if (this.state.hasError) {
            return (
                <div className="loading-screen" role="alert" data-testid="chunk-error-fallback">
                    <p>A newer version of RateRudder is available.</p>
                    <button
                        type="button"
                        className="btn btn-primary"
                        onClick={() => {
                            try {
                                window.sessionStorage.removeItem(RELOAD_STORAGE_KEY);
                            } catch {
                                // ignore storage errors
                            }
                            this.triggerReload();
                        }}
                    >
                        Reload Page
                    </button>
                </div>
            );
        }

        return this.props.children;
    }
}

export default ChunkErrorBoundary;
