import React, { Suspense } from 'react';
import { Link, useLocation } from 'wouter';
import { useFeatureFlag } from '../utils/features';
import { getRoutePreloadProps } from '../utils/routePreload';
import './Header.css';
import type { UserSite } from '../api';

const HeaderSiteSelect = React.lazy(() => import('./HeaderSiteSelect'));

interface HeaderProps {
    loggedIn: boolean;
    sites: UserSite[];
    selectedSiteID: string;
    onSiteChange: (siteID: string) => void;
    onLogout: () => void;
    onOpenNotifications?: () => void;
}

const Header: React.FC<HeaderProps> = ({ loggedIn, sites, selectedSiteID, onSiteChange, onLogout, onOpenNotifications }) => {
    const [isMenuOpen, setIsMenuOpen] = React.useState(false);
    const [openSelectOnLoad, setOpenSelectOnLoad] = React.useState(false);
    const [location] = useLocation();

    const showNotifications = useFeatureFlag('notifications');

    const toggleMenu = () => {
        setIsMenuOpen(!isMenuOpen);
    };

    const mobileNavTarget = location === '/dashboard' ? '/forecast' : '/dashboard';
    const selectedSiteLabel =
        selectedSiteID === 'ALL'
            ? 'Overview'
            : sites.find((site) => site.id === selectedSiteID)?.name || selectedSiteID;

    return (
        <header className={`raterudder-header ${loggedIn ? 'logged-in' : 'logged-out'}`}>
            <div className="content-container header-inner">
                <div className="header-left">
                    <Link to="/" className="brand-logo" onClick={() => setIsMenuOpen(false)}>
                        <img src="/logo.svg" alt="RateRudder Logo" className="header-logo-img" />
                        RateRudder
                    </Link>
                    {loggedIn && selectedSiteID !== 'ALL' && (
                        <Link
                            key={location === '/dashboard' ? 'forecast' : 'dashboard'}
                            to={mobileNavTarget}
                            className="nav-link mobile-top-nav-link hide-on-desktop"
                            onClick={() => setIsMenuOpen(false)}
                            {...getRoutePreloadProps(mobileNavTarget)}
                            data-testid="mobile-top-nav-link"
                        >
                            {location === '/dashboard' ? 'Forecast' : 'Dashboard'}
                        </Link>
                    )}
                </div>

                {loggedIn && (
                    <button
                        className={`mobile-menu-toggle ${isMenuOpen ? 'open' : ''}`}
                        onClick={toggleMenu}
                        aria-label={isMenuOpen ? "Close navigation menu" : "Open navigation menu"}
                        aria-expanded={isMenuOpen}
                        aria-controls="mobile-menu-content">
                        <span className="hamburger-line" aria-hidden="true"></span>
                        <span className="hamburger-line" aria-hidden="true"></span>
                        <span className="hamburger-line" aria-hidden="true"></span>
                    </button>
                )}

                <div id="mobile-menu-content" className={`header-content ${isMenuOpen ? 'open' : ''}`}>
                    {loggedIn && sites.length > 0 && (
                        <div className="header-site-container">
                            {sites.length > 1 ? (
                                <Suspense
                                    fallback={
                                        <button
                                            type="button"
                                            className="site-selector-header"
                                            aria-label="Select Site"
                                            onClick={() => setOpenSelectOnLoad(true)}
                                        >
                                            <span>{selectedSiteLabel}</span>
                                            <span style={{ display: 'flex', alignItems: 'center' }}>
                                                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                    <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                                                </svg>
                                            </span>
                                        </button>
                                    }
                                >
                                    <HeaderSiteSelect
                                        sites={sites}
                                        selectedSiteID={selectedSiteID}
                                        defaultOpen={openSelectOnLoad}
                                        onOpenChange={(open) => {
                                            if (!open) {
                                                setOpenSelectOnLoad(false);
                                            }
                                        }}
                                        onSelectSite={(value) => {
                                            setOpenSelectOnLoad(false);
                                            onSiteChange(value);
                                            setIsMenuOpen(false);
                                        }}
                                    />
                                </Suspense>
                            ) : (
                                <div className="site-badge-header" data-testid="header-site-name">
                                    {sites[0].name || sites[0].id}
                                </div>
                            )}
                        </div>
                    )}
                    <nav className="header-nav">
                        {loggedIn ? (
                            <>
                                <Link
                                    to="/dashboard"
                                    className={`nav-link ${location === '/dashboard' ? 'active' : ''}`}
                                    aria-current={location === '/dashboard' ? 'page' : undefined}
                                    onClick={() => setIsMenuOpen(false)}
                                    {...getRoutePreloadProps('/dashboard')}
                                >
                                    Dashboard
                                </Link>
                                {selectedSiteID !== 'ALL' && (
                                    <>
                                        <Link
                                            to="/forecast"
                                            className={`nav-link ${location === '/forecast' ? 'active' : ''}`}
                                            aria-current={location === '/forecast' ? 'page' : undefined}
                                            onClick={() => setIsMenuOpen(false)}
                                            {...getRoutePreloadProps('/forecast')}
                                        >
                                            Forecast
                                        </Link>
                                        <Link
                                            to="/settings"
                                            className={`nav-link ${location === '/settings' ? 'active' : ''}`}
                                            aria-current={location === '/settings' ? 'page' : undefined}
                                            onClick={() => setIsMenuOpen(false)}
                                            {...getRoutePreloadProps('/settings')}
                                        >
                                            Settings
                                        </Link>
                                    </>
                                )}
                            </>
                        ) : (
                            <span className="nav-empty-spacer"></span>
                        )}
                    </nav>

                    <div className="header-right">
                        {loggedIn ? (
                            <>
                                {showNotifications && (
                                    <button
                                        type="button"
                                        className="notification-bell-btn"
                                        onClick={() => {
                                            onOpenNotifications?.();
                                            setIsMenuOpen(false);
                                        }}
                                        aria-label="Notification settings"
                                        data-testid="header-bell-btn"
                                        title="Notification settings"
                                    >
                                        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"></path>
                                            <path d="M13.73 21a2 2 0 0 1-3.46 0"></path>
                                        </svg>
                                    </button>
                                )}
                                <button onClick={() => { onLogout(); setIsMenuOpen(false); }} className="logout-link">Log Out</button>
                            </>
                        ) : (
                            <Link
                                to="/login"
                                className="login-link"
                                onClick={() => setIsMenuOpen(false)}
                                {...getRoutePreloadProps('/login')}
                            >
                                <span className="hide-on-mobile">Log In / Sign Up</span>
                                <span className="hide-on-desktop">Get Started</span>
                            </Link>
                        )}
                    </div>
                </div>
            </div>
        </header>
    );
};

export default Header;
