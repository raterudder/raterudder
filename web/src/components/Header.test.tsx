import { render, screen, fireEvent } from '@testing-library/react';
import { Router } from 'wouter';
import { memoryLocation } from 'wouter/memory-location';
import Header from './Header';
import { describe, it, expect, vi } from 'vitest';

describe('Header Component', () => {
    const mockOnSiteChange = vi.fn();
    const mockOnLogout = vi.fn();

    const renderHeader = (path: string, loggedIn: boolean) => {
        const { hook } = memoryLocation({ static: true, path: path });
        return render(
            <Router hook={hook}>
                <Header
                    loggedIn={loggedIn}
                    sites={[{ id: 'site1', name: 'Site 1' }]}
                    selectedSiteID="site1"
                    onSiteChange={mockOnSiteChange}
                    onLogout={mockOnLogout}
                />
            </Router>
        );
    };

    it('renders correctly when logged out on homepage', () => {
        renderHeader('/', false);

        expect(screen.getByText('RateRudder')).toBeInTheDocument();
        expect(screen.queryByText('Dashboard')).not.toBeInTheDocument();
        expect(screen.getByText(/Log In/)).toBeInTheDocument();
    });

    it('shows nav links when logged in on dashboard', () => {
        renderHeader('/dashboard', true);

        expect(screen.getByText('Dashboard')).toBeInTheDocument();
        expect(screen.getByText('Forecast')).toBeInTheDocument();
        expect(screen.getByText('Settings')).toBeInTheDocument();
    });

    it('calls onLogout when logout button is clicked', () => {
        renderHeader('/dashboard', true);
        fireEvent.click(screen.getByText('Log Out'));
        expect(mockOnLogout).toHaveBeenCalledTimes(1);
    });
});
