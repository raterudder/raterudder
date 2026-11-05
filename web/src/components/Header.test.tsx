import { render, screen, fireEvent } from '@testing-library/react';
import { Router } from 'wouter';
import { memoryLocation } from 'wouter/memory-location';
import Header from './Header';
import { FeaturesProvider } from '../utils/features';
import { describe, it, expect, beforeEach, vi } from 'vitest';

describe('Header Component', () => {
    const mockOnSiteChange = vi.fn();
    const mockOnLogout = vi.fn();
    const mockOnOpenNotifications = vi.fn();

    beforeEach(() => {
        vi.clearAllMocks();
        vi.restoreAllMocks();
    });

    const renderHeader = (path: string, loggedIn: boolean, features?: Record<string, boolean>) => {
        const { hook } = memoryLocation({ static: true, path: path });
        return render(
            <FeaturesProvider features={features}>
                <Router hook={hook}>
                    <Header
                        loggedIn={loggedIn}
                        sites={[{ id: 'site1', name: 'Site 1' }]}
                        selectedSiteID="site1"
                        onSiteChange={mockOnSiteChange}
                        onLogout={mockOnLogout}
                        onOpenNotifications={mockOnOpenNotifications}
                    />
                </Router>
            </FeaturesProvider>
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
        const forecastLinks = screen.getAllByText('Forecast');
        expect(forecastLinks).toHaveLength(2);
        const mobileTopLink = screen.getByTestId('mobile-top-nav-link');
        expect(mobileTopLink).toHaveClass('hide-on-desktop');
        expect(mobileTopLink).toHaveTextContent('Forecast');
        expect(mobileTopLink).toHaveAttribute('href', '/forecast');
        expect(screen.getByText('Settings')).toBeInTheDocument();
    });

    it('switches mobile top link to Dashboard when on the forecast page', () => {
        renderHeader('/forecast', true);

        const mobileTopLink = screen.getByTestId('mobile-top-nav-link');
        expect(mobileTopLink).toHaveTextContent('Dashboard');
        expect(mobileTopLink).toHaveAttribute('href', '/dashboard');
        expect(screen.getByText('Forecast')).toHaveClass('active');
    });

    it('links mobile top link to Dashboard on the landing page and settings page', () => {
        const { unmount } = renderHeader('/', true);
        let mobileTopLink = screen.getByTestId('mobile-top-nav-link');
        expect(mobileTopLink).toHaveTextContent('Dashboard');
        expect(mobileTopLink).toHaveAttribute('href', '/dashboard');
        unmount();

        renderHeader('/settings', true);
        mobileTopLink = screen.getByTestId('mobile-top-nav-link');
        expect(mobileTopLink).toHaveTextContent('Dashboard');
        expect(mobileTopLink).toHaveAttribute('href', '/dashboard');
    });

    it('shows active styling and aria-current for the current route', () => {
        renderHeader('/dashboard', true);
        const dashboardLink = screen.getByText('Dashboard');
        expect(dashboardLink).toHaveClass('active');
        expect(dashboardLink).toHaveAttribute('aria-current', 'page');

        const forecastLinks = screen.getAllByText('Forecast');
        for (const forecastLink of forecastLinks) {
            expect(forecastLink).not.toHaveClass('active');
            expect(forecastLink).not.toHaveAttribute('aria-current');
        }
    });

    it('calls onLogout when logout button is clicked', () => {
        renderHeader('/dashboard', true);
        fireEvent.click(screen.getByText('Log Out'));
        expect(mockOnLogout).toHaveBeenCalledTimes(1);
    });

    it('has correct aria attributes on mobile menu button', () => {
        renderHeader('/dashboard', true);
        const menuButton = screen.getByLabelText('Open navigation menu');
        expect(menuButton).toBeInTheDocument();
        expect(menuButton).toHaveAttribute('aria-controls', 'mobile-menu-content');
        expect(menuButton).toHaveAttribute('aria-expanded', 'false');
    });

    it('shows static site badge inside mobile menu content when there is only one site', () => {
        const { hook } = memoryLocation({ static: true, path: '/dashboard' });
        render(
            <Router hook={hook}>
                <Header
                    loggedIn={true}
                    sites={[{ id: 'site1', name: 'Only Site' }]}
                    selectedSiteID="site1"
                    onSiteChange={mockOnSiteChange}
                    onLogout={mockOnLogout}
                />
            </Router>
        );

        const siteNameElement = screen.getByTestId('header-site-name');
        expect(siteNameElement).toBeInTheDocument();
        expect(siteNameElement).toHaveTextContent('Only Site');
        expect(siteNameElement.closest('#mobile-menu-content')).not.toBeNull();
        expect(screen.queryByLabelText('Select Site')).not.toBeInTheDocument();
    });

    it('shows site selector dropdown inside mobile menu content when there are multiple sites', () => {
        const { hook } = memoryLocation({ static: true, path: '/dashboard' });
        render(
            <Router hook={hook}>
                <Header
                    loggedIn={true}
                    sites={[
                        { id: 'site1', name: 'Site 1' },
                        { id: 'site2', name: 'Site 2' },
                    ]}
                    selectedSiteID="site1"
                    onSiteChange={mockOnSiteChange}
                    onLogout={mockOnLogout}
                />
            </Router>
        );

        expect(screen.queryByTestId('header-site-name')).not.toBeInTheDocument();
        const selectTrigger = screen.getByLabelText('Select Site');
        expect(selectTrigger).toBeInTheDocument();
        expect(selectTrigger.closest('#mobile-menu-content')).not.toBeNull();
    });

    it('does not render notification bell by default', () => {
        renderHeader('/dashboard', true);
        expect(screen.queryByTestId('header-bell-btn')).not.toBeInTheDocument();
    });

    it('does not render notification bell when notifications feature flag is false', () => {
        renderHeader('/dashboard', true, { notifications: false });
        expect(screen.queryByTestId('header-bell-btn')).not.toBeInTheDocument();
    });

    it('renders notification bell when notifications feature flag is enabled', () => {
        renderHeader('/dashboard', true, { notifications: true });
        expect(screen.getByTestId('header-bell-btn')).toBeInTheDocument();
    });

    it('calls onOpenNotifications when bell button is clicked', async () => {
        renderHeader('/dashboard', true, { notifications: true });
        const bellBtn = screen.getByTestId('header-bell-btn');
        fireEvent.click(bellBtn);

        expect(mockOnOpenNotifications).toHaveBeenCalledTimes(1);
    });
});
