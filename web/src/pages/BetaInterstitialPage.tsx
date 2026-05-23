import React, { useState, useEffect } from 'react';
import { useLocation } from 'wouter';
import { Select } from '@base-ui/react/select';
import { submitInterest, fetchUtilities, fetchESSList, type UtilityProviderInfo, type ESSProviderInfo } from '../api';
import './LoginPage.css';

const BetaInterstitialPage: React.FC = () => {
    const [, navigate] = useLocation();
    const [utility, setUtility] = useState<string>("");
    const [battery, setBattery] = useState<string>("");

    // New fields for "Other"
    const [utilityProviderName, setUtilityProviderName] = useState("");
    const [state, setState] = useState("");
    const [planName, setPlanName] = useState("");
    const [batteryName, setBatteryName] = useState("");
    const [comments, setComments] = useState("");

    const [isSubmitting, setIsSubmitting] = useState(false);
    const [isSubmitted, setIsSubmitted] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const [utilitiesList, setUtilitiesList] = useState<UtilityProviderInfo[]>([]);
    const [essList, setEssList] = useState<ESSProviderInfo[]>([]);
    const [isLoadingData, setIsLoadingData] = useState(true);

    useEffect(() => {
        const load = async () => {
            try {
                const [utils, ess] = await Promise.all([
                    fetchUtilities(),
                    fetchESSList()
                ]);
                setUtilitiesList(utils);
                setEssList(ess);
            } catch (err) {
                console.error("Failed to load options", err);
            } finally {
                setIsLoadingData(false);
            }
        };
        load();
    }, []);

    const isOther = (utility === 'other' || battery === 'other');

    const handleContinue = () => {
        navigate('/new-site');
    };

    const handleSubmitInterest = async () => {
        setIsSubmitting(true);
        setError(null);
        try {
            await submitInterest({
                utility,
                battery,
                utilityProviderName,
                state,
                planName,
                batteryName,
                comments
            });
            setIsSubmitted(true);
        } catch (err: any) {
            setError(err.message || "Failed to submit interest");
        } finally {
            setIsSubmitting(false);
        }
    };

    return (
        <div className="auth-page">
            <div className="auth-card" style={{ maxWidth: '440px' }}>
                <h1 className="beta-interstitial-title">RateRudder Beta</h1>
                {!isSubmitted && (
                    <p className="beta-interstitial-desc">
                        To get started, please confirm your equipment and utility provider.
                    </p>
                )}

                {!isSubmitted && (
                    <div className="beta-interstitial-form">
                        <div>
                            <label id="utility-label" className="beta-interstitial-label">Utility Provider</label>
                            <Select.Root
                                value={utility}
                                onValueChange={(value) => setUtility(value as string)}
                            >
                                <Select.Trigger aria-labelledby="utility-label" className="select-trigger" disabled={isLoadingData}>
                                    <Select.Value placeholder={isLoadingData ? "Loading..." : "Select your utility..."}>
                                        {utility === 'other' ? 'Other' : utilitiesList.find(u => u.id === utility)?.name || (isLoadingData ? 'Loading...' : 'Select your utility...')}
                                    </Select.Value>
                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                        </svg>
                                    </Select.Icon>
                                </Select.Trigger>
                                <Select.Portal>
                                    <Select.Positioner style={{ zIndex: 1000, width: 'var(--anchor-width)' }}>
                                        <Select.Popup className="select-popup">
                                            {utilitiesList.filter(u => !u.hidden).map(u => (
                                                <Select.Item key={u.id} value={u.id} className="select-item">
                                                    <Select.ItemText>{u.name}</Select.ItemText>
                                                </Select.Item>
                                            ))}
                                            <Select.Item value="other" className="select-item">
                                                <Select.ItemText>Other</Select.ItemText>
                                            </Select.Item>
                                        </Select.Popup>
                                    </Select.Positioner>
                                </Select.Portal>
                            </Select.Root>
                        </div>

                        <div>
                            <label id="battery-label" className="beta-interstitial-label">Battery System</label>
                            <Select.Root
                                value={battery}
                                onValueChange={(value) => setBattery(value as string)}
                            >
                                <Select.Trigger aria-labelledby="battery-label" className="select-trigger" disabled={isLoadingData}>
                                    <Select.Value placeholder={isLoadingData ? "Loading..." : "Select your battery..."}>
                                        {battery === 'other' ? 'Other' : essList.find(e => e.id === battery)?.name || (isLoadingData ? 'Loading...' : 'Select your battery...')}
                                    </Select.Value>
                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                        </svg>
                                    </Select.Icon>
                                </Select.Trigger>
                                <Select.Portal>
                                    <Select.Positioner style={{ zIndex: 1000, width: 'var(--anchor-width)' }}>
                                        <Select.Popup className="select-popup">
                                            {essList.filter(e => !e.hidden).map(e => (
                                                <Select.Item key={e.id} value={e.id} className="select-item">
                                                    <Select.ItemText>{e.name}</Select.ItemText>
                                                </Select.Item>
                                            ))}
                                            <Select.Item value="other" className="select-item">
                                                <Select.ItemText>Other</Select.ItemText>
                                            </Select.Item>
                                        </Select.Popup>
                                    </Select.Positioner>
                                </Select.Portal>
                            </Select.Root>
                        </div>
                    </div>
                )}

                {isOther && !isSubmitted && (
                    <div className="beta-interstitial-form" style={{ marginTop: '1.5rem', borderTop: '1px solid var(--border-color)', paddingTop: '1.5rem' }}>
                        {utility === 'other' && (
                            <>
                                <div>
                                    <label htmlFor="utilityProviderName" className="beta-interstitial-label">Utility Provider Name</label>
                                    <input
                                        id="utilityProviderName"
                                        type="text"
                                        className="input"
                                        placeholder="e.g. PG&E"
                                        value={utilityProviderName}
                                        onChange={(e) => setUtilityProviderName(e.target.value)}
                                    />
                                </div>
                                <div style={{ display: 'flex', gap: '1rem' }}>
                                    <div style={{ flex: 1 }}>
                                        <label htmlFor="state" className="beta-interstitial-label">State</label>
                                        <input
                                            id="state"
                                            type="text"
                                            className="input"
                                            placeholder="e.g. CA"
                                            value={state}
                                            onChange={(e) => setState(e.target.value)}
                                        />
                                    </div>
                                    <div style={{ flex: 2 }}>
                                        <label htmlFor="planName" className="beta-interstitial-label">Plan Name</label>
                                        <input
                                            id="planName"
                                            type="text"
                                            className="input"
                                            placeholder="e.g. EV2-A"
                                            value={planName}
                                            onChange={(e) => setPlanName(e.target.value)}
                                        />
                                    </div>
                                </div>
                            </>
                        )}

                        {battery === 'other' && (
                            <div>
                                <label htmlFor="batteryName" className="beta-interstitial-label">Battery System</label>
                                <input
                                    id="batteryName"
                                    type="text"
                                    className="input"
                                    placeholder="e.g. Enphase"
                                    value={batteryName}
                                    onChange={(e) => setBatteryName(e.target.value)}
                                />
                            </div>
                        )}

                        <div>
                            <label htmlFor="comments" className="beta-interstitial-label">Anything else to share or comments?</label>
                            <textarea
                                id="comments"
                                className="input"
                                style={{ minHeight: '80px', paddingTop: '0.5rem' }}
                                placeholder="Your feedback helps us prioritize..."
                                value={comments}
                                onChange={(e) => setComments(e.target.value)}
                            />
                        </div>

                        {error && <div className="error-message" style={{ marginBottom: '1rem' }} role="alert">{error}</div>}

                        <button
                            onClick={handleSubmitInterest}
                            disabled={isSubmitting}
                            className="btn beta-interstitial-continue-btn"
                        >
                            {isSubmitting && <span className="loading-spinner" aria-hidden="true"></span>}
                            {isSubmitting ? 'Submitting...' : 'Express Interest'}
                        </button>
                    </div>
                )}

                {isSubmitted && (
                    <div className="beta-interstitial-feedback success" role="status">
                        <p style={{ fontWeight: '700', marginBottom: '0.5rem', fontSize: '1.1rem' }}>Success!</p>
                        <p>
                            We've received your interest! We'll reach out when we can support your configuration or if we have further questions.
                        </p>
                    </div>
                )}

                {!isOther && utility && battery && !isSubmitted && (
                    <button
                        onClick={handleContinue}
                        className="btn beta-interstitial-continue-btn"
                    >
                        You're all set! Let's start saving money 🚀
                    </button>
                )}
            </div>
        </div>
    );
};

export default BetaInterstitialPage;
