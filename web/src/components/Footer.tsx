import React from 'react';
import { Link } from 'wouter';
import { Separator } from '@base-ui/react/separator';
import { getRoutePreloadProps } from '../utils/routePreload';
import './Footer.css';

const Footer: React.FC = () => {
    const currentYear = new Date().getFullYear();

    return (
        <footer className="app-footer">
            <div className="footer-content">
                <div className="opensource-link">
                    Open Source Project. Contribute on <a href="https://github.com/raterudder/raterudder" target="_blank" rel="noopener noreferrer">GitHub</a>.
                </div>
                <div className="footer-links">
                    <Link to="/how-it-works" {...getRoutePreloadProps('/how-it-works')}>How It Works</Link>
                    <Separator className="separator" />
                    <Link to="/getting-started" {...getRoutePreloadProps('/getting-started')}>Getting Started</Link>
                    <Separator className="separator" />
                    <Link to="/utilities" {...getRoutePreloadProps('/utilities')}>Supported Utilities</Link>
                    <Separator className="separator" />
                    <Link to="/changelog" {...getRoutePreloadProps('/changelog')}>Changelog</Link>
                    <Separator className="separator" />
                    <Link to="/privacy" {...getRoutePreloadProps('/privacy')}>Privacy Policy</Link>
                    <Separator className="separator" />
                    <Link to="/terms" {...getRoutePreloadProps('/terms')}>Terms of Service</Link>
                </div>
                <div className="copyright">
                    &copy; {currentYear} RateRudder. All rights reserved.
                </div>
            </div>
        </footer>
    );
};

export default Footer;
