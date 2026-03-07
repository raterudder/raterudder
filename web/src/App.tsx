
import React, { useEffect, useState } from 'react';
import { Route, Switch, Redirect, useLocation, Router } from 'wouter';

import Header from './components/Header';
import Footer from './components/Footer';
import FeedbackWidget from './components/FeedbackWidget';
import './App.css';
import { fetchAuthStatus, login, logout, type AuthStatus, type UserSite } from './api';

import LandingPage from './pages/LandingPage';
import Dashboard from './pages/Dashboard';
import Settings from './pages/Settings';
import Forecast from './pages/Forecast';
import LoginPage from './pages/LoginPage';
import JoinSitePage from './pages/JoinSitePage';
import NewSitePage from './pages/NewSitePage';
import BetaInterstitialPage from './pages/BetaInterstitialPage';
import AdminPage from './pages/AdminPage';
import PrivacyPolicy from './pages/PrivacyPolicy';
import TermsOfService from './pages/TermsOfService';

// Protected Route Wrapper
const ProtectedRoute = ({ children, loggedIn, loading }: { children: React.ReactElement, loggedIn: boolean, loading: boolean }) => {
    const [location] = useLocation();

    if (loading) {
        return <div className="loading-screen">Loading...</div>; // Could be a nicer spinner
    }

    if (!loggedIn) {
         // Redirect them to the login page, but save the current location they were trying to go to
        return <Redirect to={`/login?from=${encodeURIComponent(location)}`} replace />;
    }

    return children;
};

function AppContent() {
    const [authRequired, setAuthRequired] = useState(false);
    const [loggedIn, setLoggedIn] = useState(false);
    const [clientIDs, setClientIDs] = useState<Record<string, string>>({});
    const [sites, setSites] = useState<UserSite[]>([]);
    const [selectedSiteID, setSelectedSiteID] = useState<string>("");
    const [viewSiteOverride, setViewSiteOverride] = useState<string | null>(() => {
        const queryParams = new URLSearchParams(window.location.search);
        return queryParams.get('viewSite');
    });
    const [loading, setLoading] = useState(true);
    const [hasAttemptedFetch, setHasAttemptedFetch] = useState(false);

    const [location, navigate] = useLocation();
    const isHome = location === '/';

    const applyStatus = React.useCallback((status: AuthStatus, redirectOnLogin: boolean) => {
        setAuthRequired(status.authRequired);
        setLoggedIn(status.loggedIn);
        setClientIDs(status.clientIDs || {});

        const newSites = status.sites || [];
        setSites(newSites);

        // Default select first site if not selected or invalid
        if (newSites.length > 0) {
            setSelectedSiteID(current => {
                if (!current || !newSites.some(site => site.id === current)) {
                    return newSites[0].id;
                }
                return current;
            });
        }

        setHasAttemptedFetch(true);

        if (redirectOnLogin && status.loggedIn) {
            navigate('/dashboard');
        }
    }, [navigate]);

    // Initial auth check — runs once on mount. Sets loading=true to gate
    // the first render until we know whether the user is authenticated.
    useEffect(() => {
        // Skip auth check if we're on the landing page.
        // We'll trigger it later if they navigate away.
        if (window.location.pathname === '/') {
            setLoading(false);
            return;
        }

        fetchAuthStatus()
            .then(status => {
                applyStatus(status, false);
            })
            .catch(err => {
                console.error(err);
                setHasAttemptedFetch(true);
            })
            .finally(() => {
                setLoading(false);
            });
    }, [applyStatus]);

    // Trigger auth check if user navigates to a non-home page and hasn't checked yet.
    useEffect(() => {
        if (!isHome && !hasAttemptedFetch && !loading) {
            setLoading(true);
            fetchAuthStatus()
                .then(status => {
                    applyStatus(status, false);
                })
                .catch(err => {
                    console.error(err);
                    setHasAttemptedFetch(true);
                })
                .finally(() => {
                    setLoading(false);
                });
        }
    }, [location, hasAttemptedFetch, loading, isHome, applyStatus]);

    // Re-check auth after login/logout without toggling loading, so child
    // components stay mounted and don't re-fire their own data fetches.
    const checkStatus = (redirectOnLogin = false) => {
        fetchAuthStatus()
            .then(status => {
                applyStatus(status, redirectOnLogin);
            })
            .catch(err => {
                console.error(err);
            });
    };

    const handleLoginSuccess = async (credentialResponse: { credential?: string }, client?: string) => {
        try {
            if (credentialResponse.credential) {
                await login(credentialResponse.credential, client);
                checkStatus(true); // Redirect to dashboard on success
            }
        } catch (err) {
            console.error("Login failed", err);
        }
    };

    const handleLogout = async () => {
        try {
            await logout();
            checkStatus();
            setSites([]);
            setSelectedSiteID("");
            navigate('/'); // Go back to landing page on logout
        } catch (err) {
            console.error("Logout failed", err);
        }
    };

    const showLoading = (loading || (!isHome && !hasAttemptedFetch)) && !isHome;

    const effectiveSiteID = viewSiteOverride || selectedSiteID;
    const effectiveSites = viewSiteOverride && !sites.some(site => site.id === viewSiteOverride)
        ? [...sites, { id: viewSiteOverride, name: "" }]
        : sites;

    const handleSiteChange = (id: string) => {
        if (viewSiteOverride) setViewSiteOverride(null);
        setSelectedSiteID(id);
        if (id === 'ALL') {
            navigate('/dashboard');
        }
    };

    return (
        <>
            <div className={isHome ? "app-container-home" : "app-container"}>
                <Header
                    loggedIn={loggedIn}
                    sites={effectiveSites}
                    selectedSiteID={effectiveSiteID}
                    onSiteChange={handleSiteChange}
                    onLogout={handleLogout}
                />

                <main className="main-content">
                    {viewSiteOverride && (
                        <div className="admin-site-banner" style={{ background: '#f59e0b', color: '#fff', padding: '0.5rem', textAlign: 'center', fontWeight: 'bold' }}>
                            Admin Mode: Viewing Site {viewSiteOverride}
                        </div>
                    )}
                    {showLoading ? (
                        <div className="loading-screen">Loading...</div>
                    ) : (
                        <Switch>
                            <Route path="/" component={LandingPage} />
                            <Route path="/privacy" component={PrivacyPolicy} />
                            <Route path="/terms" component={TermsOfService} />
                            <Route path="/login">
                                {loggedIn ? <Redirect to="/dashboard" replace /> :
                                <LoginPage
                                    onLoginSuccess={handleLoginSuccess}
                                    onLoginError={() => console.log('Login Failed')}
                                    authEnabled={authRequired}
                                    clientIDs={clientIDs}
                                />}
                            </Route>

                            {/* Protected Routes */}
                            <Route path="/welcome">
                                <ProtectedRoute loggedIn={loggedIn} loading={loading}>
                                    {effectiveSites.length > 0 ? (
                                        <Redirect to="/dashboard" replace />
                                    ) : (
                                        <BetaInterstitialPage />
                                    )}
                                </ProtectedRoute>
                            </Route>
                            <Route path="/new-site">
                                <ProtectedRoute loggedIn={loggedIn} loading={loading}>
                                    <NewSitePage onJoinSuccess={() => checkStatus(true)} />
                                </ProtectedRoute>
                            </Route>
                            <Route path="/join-site">
                                <ProtectedRoute loggedIn={loggedIn} loading={loading}>
                                    <JoinSitePage onJoinSuccess={() => checkStatus(true)} />
                                </ProtectedRoute>
                            </Route>
                            <Route path="/dashboard">
                                <ProtectedRoute loggedIn={loggedIn} loading={loading}>
                                    {!effectiveSiteID && effectiveSites.length === 0 ? (
                                        <Redirect to="/welcome" replace />
                                    ) : (
                                        <Dashboard siteID={effectiveSiteID} />
                                    )}
                                </ProtectedRoute>
                            </Route>
                            <Route path="/forecast">
                                <ProtectedRoute loggedIn={loggedIn} loading={loading}>
                                    {!effectiveSiteID && effectiveSites.length === 0 ? (
                                        <Redirect to="/welcome" replace />
                                    ) : (
                                        <Forecast siteID={effectiveSiteID} />
                                    )}
                                </ProtectedRoute>
                            </Route>
                            <Route path="/settings">
                                <ProtectedRoute loggedIn={loggedIn} loading={loading}>
                                    {!effectiveSiteID && effectiveSites.length === 0 ? (
                                        <Redirect to="/welcome" replace />
                                    ) : (
                                        <Settings siteID={effectiveSiteID} />
                                    )}
                                </ProtectedRoute>
                            </Route>
                            <Route path="/admin">
                                <ProtectedRoute loggedIn={loggedIn} loading={loading}>
                                    <AdminPage />
                                </ProtectedRoute>
                            </Route>

                            {/* Fallback */}
                            <Route>
                                <Redirect to="/" replace />
                            </Route>
                        </Switch>
                    )}
                </main>

                <Footer />

                {loggedIn && (
                    <FeedbackWidget siteID={effectiveSiteID} />
                )}
            </div>
        </>
    );
}


function App() {
  return (
    <Router>
        <AppContent />
    </Router>
  );
}

export default App;
