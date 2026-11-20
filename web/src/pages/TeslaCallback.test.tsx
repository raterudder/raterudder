import { render, screen, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import TeslaCallback from './TeslaCallback';

describe('TeslaCallback', () => {
    const originalOpener = window.opener;
    const originalClose = window.close;

    beforeEach(() => {
        vi.useFakeTimers();
        window.close = vi.fn();
    });

    afterEach(() => {
        vi.useRealTimers();
        Object.defineProperty(window, 'opener', {
            value: originalOpener,
            writable: true,
            configurable: true,
        });
        window.close = originalClose;
        window.history.pushState(null, '', '/');
    });

    it('posts OAUTH_CODE to opener and closes popup on success', () => {
        const postMessage = vi.fn();
        Object.defineProperty(window, 'opener', {
            value: { postMessage },
            writable: true,
            configurable: true,
        });
        window.history.pushState(null, '', '/callback/tesla?code=abc123&state=site-1');

        render(<TeslaCallback />);

        expect(postMessage).toHaveBeenCalledWith(
            { type: 'OAUTH_CODE', code: 'abc123', state: 'site-1' },
            window.location.origin
        );
        act(() => {
            vi.advanceTimersByTime(500);
        });
        expect(window.close).toHaveBeenCalled();
    });

    it('posts OAUTH_ERROR to opener and closes popup when user rejects permissions', () => {
        const postMessage = vi.fn();
        Object.defineProperty(window, 'opener', {
            value: { postMessage },
            writable: true,
            configurable: true,
        });
        window.history.pushState(null, '', '/callback/tesla?error=access_denied&error_description=User+cancelled&state=site-1');

        render(<TeslaCallback />);

        expect(postMessage).toHaveBeenCalledWith(
            {
                type: 'OAUTH_ERROR',
                error: 'access_denied',
                errorDescription: 'User cancelled',
                state: 'site-1',
            },
            window.location.origin
        );
        act(() => {
            vi.advanceTimersByTime(300);
        });
        expect(window.close).toHaveBeenCalled();
    });

    it('posts OAUTH_ERROR with missing_code when neither code nor error is present', () => {
        const postMessage = vi.fn();
        Object.defineProperty(window, 'opener', {
            value: { postMessage },
            writable: true,
            configurable: true,
        });
        window.history.pushState(null, '', '/callback/tesla?state=site-1');

        render(<TeslaCallback />);

        expect(postMessage).toHaveBeenCalledWith(
            {
                type: 'OAUTH_ERROR',
                error: 'missing_code',
                errorDescription: undefined,
                state: 'site-1',
            },
            window.location.origin
        );
    });

    it('shows return-to-settings message when window.opener is missing', () => {
        Object.defineProperty(window, 'opener', {
            value: null,
            writable: true,
            configurable: true,
        });
        window.history.pushState(null, '', '/callback/tesla?error=access_denied');

        render(<TeslaCallback />);

        expect(screen.getByText(/Authorization was not completed. Please return to the settings page and try again./i)).toBeInTheDocument();
    });
});
