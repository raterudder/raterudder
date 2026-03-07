import React from 'react';
import { Link } from 'wouter';
import { Select } from '@base-ui/react/select';
import './Header.css';
import type { UserSite } from '../api';

interface HeaderProps {
    loggedIn: boolean;
    sites: UserSite[];
    selectedSiteID: string;
    onSiteChange: (siteID: string) => void;
    onLogout: () => void;
}

const Header: React.FC<HeaderProps> = ({ loggedIn, sites, selectedSiteID, onSiteChange, onLogout }) => {
    const [isMenuOpen, setIsMenuOpen] = React.useState(false);

    const toggleMenu = () => {
        setIsMenuOpen(!isMenuOpen);
    };

    return (
        <header className={`raterudder-header ${loggedIn ? 'logged-in' : 'logged-out'}`}>
            <div className="content-container header-inner">
                <div className="header-left">
                    <Link to="/" className="brand-logo" onClick={() => setIsMenuOpen(false)}>
                        <img src="/logo.svg" alt="RateRudder Logo" className="header-logo-img" />
                        RateRudder
                    </Link>
                    {loggedIn && sites.length > 1 && (
                        <Select.Root
                            value={selectedSiteID}
                            items={{
                                ...Object.fromEntries(sites.map(site => [site.id, site.name || site.id])),
                                "ALL": "Overview"
                            }}
                            onValueChange={(value) => onSiteChange(value as string)}
                        >
                            <Select.Trigger className="site-selector-header">
                                <Select.Value />
                            </Select.Trigger>
                            <Select.Portal>
                                <Select.Positioner className="select-positioner">
                                    <Select.Popup className="select-popup">
                                        <Select.Item className="select-item" value="ALL">
                                            <Select.ItemText>Overview</Select.ItemText>
                                        </Select.Item>
                                        {sites.map(site => (
                                            <Select.Item key={site.id} className="select-item" value={site.id}>
                                                <Select.ItemText>{site.name || site.id}</Select.ItemText>
                                            </Select.Item>
                                        ))}
                                    </Select.Popup>
                                </Select.Positioner>
                            </Select.Portal>
                        </Select.Root>
                    )}
                </div>

                {loggedIn && (
                    <button className="mobile-menu-toggle" onClick={toggleMenu} aria-label="Toggle navigation">
                        <span className="hamburger-line"></span>
                        <span className="hamburger-line"></span>
                        <span className="hamburger-line"></span>
                    </button>
                )}

                <div className={`header-content ${isMenuOpen ? 'open' : ''}`}>
                    <nav className="header-nav">
                        {loggedIn ? (
                            <>
                                <Link to="/dashboard" className="nav-link" onClick={() => setIsMenuOpen(false)}>Dashboard</Link>
                                {selectedSiteID !== 'ALL' && (
                                    <>
                                        <Link to="/forecast" className="nav-link" onClick={() => setIsMenuOpen(false)}>Forecast</Link>
                                        <Link to="/settings" className="nav-link" onClick={() => setIsMenuOpen(false)}>Settings</Link>
                                    </>
                                )}
                            </>
                        ) : (
                            <span className="nav-empty-spacer"></span>
                        )}
                    </nav>

                    <div className="header-right">
                        {loggedIn ? (
                            <button onClick={() => { onLogout(); setIsMenuOpen(false); }} className="logout-link">Log Out</button>
                        ) : (
                            <Link to="/login" className="login-link" onClick={() => setIsMenuOpen(false)}>
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
