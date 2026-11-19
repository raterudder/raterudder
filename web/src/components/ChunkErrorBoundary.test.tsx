import React from 'react';
import { act, render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import ChunkErrorBoundary from './ChunkErrorBoundary';

describe('ChunkErrorBoundary', () => {
    beforeEach(() => {
        window.sessionStorage.clear();
        window.history.pushState(null, '', '/dashboard');
        vi.restoreAllMocks();
    });

    it('recovers from vite:preloadError by preventing default and reloading once, then showing fallback UI if triggered again', () => {
        const onReload = vi.fn();

        render(
            <ChunkErrorBoundary onReload={onReload}>
                <div>App Ready</div>
            </ChunkErrorBoundary>
        );

        expect(screen.getByText('App Ready')).toBeInTheDocument();

        const firstEvent = new Event('vite:preloadError', { cancelable: true });
        let notPrevented = true;
        act(() => {
            notPrevented = window.dispatchEvent(firstEvent);
        });

        expect(notPrevented).toBe(false);
        expect(firstEvent.defaultPrevented).toBe(true);
        expect(onReload).toHaveBeenCalledTimes(1);
        expect(screen.queryByTestId('chunk-error-fallback')).not.toBeInTheDocument();

        // Dispatching a second time within cooldown shows fallback UI instead of reload-looping
        const secondEvent = new Event('vite:preloadError', { cancelable: true });
        act(() => {
            window.dispatchEvent(secondEvent);
        });

        expect(onReload).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId('chunk-error-fallback')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: 'Reload Page' }));
        expect(onReload).toHaveBeenCalledTimes(2);
    });

    it('catches dynamic import chunk load errors thrown during render and triggers reload once', () => {
        vi.spyOn(console, 'error').mockImplementation(() => {});
        const onReload = vi.fn();

        const BrokenLazyRoute: React.FC = () => {
            throw new TypeError('Failed to fetch dynamically imported module: https://raterudder.com/assets/Dashboard-old.js');
        };

        render(
            <ChunkErrorBoundary onReload={onReload}>
                <BrokenLazyRoute />
            </ChunkErrorBoundary>
        );

        expect(onReload).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId('chunk-error-fallback')).toBeInTheDocument();
    });
});
