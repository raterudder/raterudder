import React from 'react';
import { act, render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import ChunkErrorBoundary from './ChunkErrorBoundary';
import * as routePreload from '../utils/routePreload';

describe('ChunkErrorBoundary', () => {
    beforeEach(() => {
        window.sessionStorage.clear();
        window.history.pushState(null, '', '/dashboard');
        routePreload.resetRoutePreloadState();
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

    it('ignores background preloadRoute vite:preloadError while on another route without reloading or unmounting children', async () => {
        window.history.pushState(null, '', '/settings');
        const onReload = vi.fn();

        render(
            <ChunkErrorBoundary onReload={onReload}>
                <div>Unsaved Settings Form</div>
            </ChunkErrorBoundary>
        );

        const preloadEvents: Event[] = [];
        vi.spyOn(routePreload.routeImporters, '/forecast').mockImplementation(async () => {
            const event = new Event('vite:preloadError', { cancelable: true });
            preloadEvents.push(event);
            window.dispatchEvent(event);
            throw new TypeError('Failed to fetch dynamically imported module');
        });

        await act(async () => {
            routePreload.preloadRoute('/forecast?viewSite=site1');
            routePreload.preloadRoute('/forecast');
            await Promise.resolve();
        });

        expect(preloadEvents).toHaveLength(2);
        expect(preloadEvents[0].defaultPrevented).toBe(false);
        expect(preloadEvents[1].defaultPrevented).toBe(false);
        expect(onReload).not.toHaveBeenCalled();
        expect(screen.getByText('Unsaved Settings Form')).toBeInTheDocument();
        expect(screen.queryByTestId('chunk-error-fallback')).not.toBeInTheDocument();

        // Subsequent required chunk preload error still triggers auto-reload (cooldown was not consumed)
        const requiredEvent = new Event('vite:preloadError', { cancelable: true });
        act(() => {
            window.dispatchEvent(requiredEvent);
        });
        expect(requiredEvent.defaultPrevented).toBe(true);
        expect(onReload).toHaveBeenCalledTimes(1);
    });

    it('still recovers when vite:preloadError fires after navigating to the preloaded route', async () => {
        window.history.pushState(null, '', '/settings');
        const onReload = vi.fn();

        render(
            <ChunkErrorBoundary onReload={onReload}>
                <div>Route Content</div>
            </ChunkErrorBoundary>
        );

        let rejectPreload!: (reason?: unknown) => void;
        vi.spyOn(routePreload.routeImporters, '/forecast').mockImplementation(
            () =>
                new Promise((_, reject) => {
                    rejectPreload = reject;
                })
        );

        routePreload.preloadRoute('/forecast');
        // User clicks link and navigates before the preload fails
        window.history.pushState(null, '', '/forecast');

        const preloadEvent = new Event('vite:preloadError', { cancelable: true });
        await act(async () => {
            window.dispatchEvent(preloadEvent);
            rejectPreload(new TypeError('Failed to fetch dynamically imported module'));
            await Promise.resolve();
        });

        expect(preloadEvent.defaultPrevented).toBe(true);
        expect(onReload).toHaveBeenCalledTimes(1);
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
