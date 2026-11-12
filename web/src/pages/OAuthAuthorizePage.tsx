import React, { useEffect, useState } from 'react';
import { Field } from '@base-ui/react/field';
import { Select } from '@base-ui/react/select';
import { fetchOAuthClientInfo, approveOAuthClient, type OAuthClientInfo, type UserSite } from '../api';
import './OAuthAuthorizePage.css';

interface OAuthAuthorizePageProps {
    sites: UserSite[];
    selectedSiteID: string;
}

const OAuthAuthorizePage: React.FC<OAuthAuthorizePageProps> = ({ sites, selectedSiteID }) => {
    const [siteID, setSiteID] = useState<string>(() => {
        if (selectedSiteID && selectedSiteID !== 'ALL') {
            return selectedSiteID;
        }
        return sites[0]?.id || '';
    });
    const [clientInfo, setClientInfo] = useState<OAuthClientInfo | null>(null);
    const [loading, setLoading] = useState<boolean>(true);
    const [approving, setApproving] = useState<boolean>(false);
    const [cancelled, setCancelled] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);

    const searchParams = new URLSearchParams(window.location.search);
    const clientID = searchParams.get('client_id') || '';
    const redirectURI = searchParams.get('redirect_uri') || '';
    const responseType = searchParams.get('response_type') || 'code';
    const codeChallenge = searchParams.get('code_challenge') || '';
    const codeChallengeMethod = searchParams.get('code_challenge_method') || 'S256';
    const state = searchParams.get('state') || '';

    useEffect(() => {
        if (!siteID && sites.length > 0) {
            setSiteID(selectedSiteID && selectedSiteID !== 'ALL' ? selectedSiteID : sites[0].id);
        }
    }, [sites, selectedSiteID, siteID]);

    useEffect(() => {
        let active = true;
        setLoading(true);
        setError(null);

        fetchOAuthClientInfo({
            clientID,
            redirectURI,
            responseType,
            codeChallenge,
            codeChallengeMethod,
        })
            .then((info) => {
                if (active) {
                    setClientInfo(info);
                }
            })
            .catch((err) => {
                if (active) {
                    setError(err instanceof Error ? err.message : 'Invalid authorization request');
                }
            })
            .finally(() => {
                if (active) {
                    setLoading(false);
                }
            });

        return () => {
            active = false;
        };
    }, [clientID, redirectURI, responseType, codeChallenge, codeChallengeMethod]);

    const handleApprove = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!siteID || approving) return;

        setApproving(true);
        setError(null);
        try {
            const res = await approveOAuthClient({
                siteID,
                clientID,
                redirectURI,
                responseType,
                codeChallenge,
                codeChallengeMethod,
                state: state || undefined,
            });
            const parsedUrl = new URL(res.redirectURL);
            if (parsedUrl.protocol !== 'https:' && parsedUrl.protocol !== 'http:') {
                throw new Error('Invalid redirect URL protocol');
            }
            window.location.href = res.redirectURL;
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to approve application');
            setApproving(false);
        }
    };

    const handleCancel = () => {
        setCancelled(true);
        window.close();
    };

    if (loading) {
        return (
            <div className="loading-screen">
                <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
                <span>Verifying application request...</span>
            </div>
        );
    }

    const clientName = clientInfo?.clientName || '';
    const clientDomain = clientInfo?.clientDomain || '';
    let redirectLabel: string;
    let redirectOrigin: string;
    try {
        const parsedRedirect = new URL(clientInfo?.redirectURI || redirectURI);
        if (parsedRedirect.protocol === 'https:') {
            redirectLabel = parsedRedirect.host;
            redirectOrigin = parsedRedirect.origin.toLowerCase();
        } else if (parsedRedirect.protocol === 'http:') {
            redirectLabel = parsedRedirect.origin;
            redirectOrigin = parsedRedirect.origin.toLowerCase();
        } else {
            redirectLabel = `${parsedRedirect.protocol}//${parsedRedirect.host}`;
            redirectOrigin = parsedRedirect.protocol.toLowerCase();
        }
    } catch {
        redirectLabel = '';
        redirectOrigin = '';
    }

    const expectedDomainOrigin = clientDomain
        ? `https://${clientDomain.toLowerCase()}`
        : '';
    const hasRedirectOriginMismatch = Boolean(
        clientInfo?.verified &&
        expectedDomainOrigin &&
        redirectOrigin &&
        redirectOrigin !== expectedDomainOrigin
    );

    return (
        <div className="auth-page" data-testid="oauth-authorize-page">
            <div className="auth-card">
                {cancelled ? (
                    <>
                        <h1>Authorization Cancelled</h1>
                        <p data-testid="oauth-close-window-message">You may now close this window.</p>
                    </>
                ) : (
                    <>
                        <h1>{clientName ? `Connect ${clientName}` : 'Authorize Connected App'}</h1>
                        {error && !clientName ? (
                            <>
                                <div className="join-error" data-testid="oauth-error">{error}</div>
                                <button type="button" className="btn btn-secondary oauth-close-btn" onClick={handleCancel}>
                                    Close Window
                                </button>
                            </>
                        ) : (
                            <>
                                <p className="oauth-intro">
                                    <strong>{clientName}</strong> is requesting read-only access to your RateRudder site energy data and forecasts.
                                </p>

                                {clientInfo?.verified && clientDomain ? (
                                    <>
                                        <div className="oauth-client-domain" data-testid="oauth-client-domain">
                                            <span>
                                                Domain: <strong>{clientDomain}</strong>
                                            </span>
                                        </div>
                                        {hasRedirectOriginMismatch && (
                                            <div className="oauth-unverified-warning" data-testid="oauth-redirect-mismatch-warning" role="alert">
                                                <span className="oauth-unverified-warning-title">Different redirect destination</span>
                                                This app is from <strong>{clientDomain}</strong>, but approving will send access to <strong>{redirectLabel}</strong>. Only approve if you trust both.
                                            </div>
                                        )}
                                    </>
                                ) : (
                                    <div className="oauth-unverified-warning" data-testid="oauth-unverified-warning" role="alert">
                                        <span className="oauth-unverified-warning-title">Unverified application</span>
                                        Anyone can dynamically register an app claiming the name &ldquo;{clientName}&rdquo;.
                                        {redirectLabel ? ` Approving will grant access to ${redirectLabel}.` : ''} Only approve if you initiated this connection yourself.
                                    </div>
                                )}

                                <form onSubmit={handleApprove} className="join-form">
                                    {sites.length === 0 && (
                                        <div className="join-error" data-testid="oauth-no-sites-error">
                                            You do not have any sites configured on your RateRudder account.
                                        </div>
                                    )}
                                    {sites.length > 1 && (
                                        <Field.Root className="join-field">
                                            <Field.Label htmlFor="oauth-site-select">Select Site</Field.Label>
                                            <Select.Root
                                                value={siteID}
                                                onValueChange={(val) => {
                                                    if (val) setSiteID(val as string);
                                                }}
                                            >
                                                <Select.Trigger className="select-trigger" id="oauth-site-select" aria-label="Select Site">
                                                    <Select.Value>
                                                        {sites.find((s) => s.id === siteID)?.name || siteID}
                                                    </Select.Value>
                                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                                        </svg>
                                                    </Select.Icon>
                                                </Select.Trigger>
                                                <Select.Portal>
                                                    <Select.Positioner className="select-positioner">
                                                        <Select.Popup className="select-popup">
                                                            {sites.map((site) => (
                                                                <Select.Item key={site.id} className="select-item" value={site.id}>
                                                                    <Select.ItemText>{site.name || site.id}</Select.ItemText>
                                                                </Select.Item>
                                                            ))}
                                                        </Select.Popup>
                                                    </Select.Positioner>
                                                </Select.Portal>
                                            </Select.Root>
                                        </Field.Root>
                                    )}

                                    {error && <div className="join-error" data-testid="oauth-error">{error}</div>}

                                    <div className="oauth-actions">
                                        <button
                                            type="button"
                                            className="btn btn-secondary"
                                            onClick={handleCancel}
                                            disabled={approving}
                                        >
                                            Cancel
                                        </button>
                                        <button
                                            type="submit"
                                            className="btn btn-primary"
                                            disabled={approving || !siteID}
                                            data-testid="oauth-approve-btn"
                                        >
                                            {approving && <span className="loading-spinner" aria-hidden="true"></span>}
                                            {approving ? 'Approving…' : 'Approve'}
                                        </button>
                                    </div>
                                </form>
                            </>
                        )}
                    </>
                )}
            </div>
        </div>
    );
};

export default OAuthAuthorizePage;
