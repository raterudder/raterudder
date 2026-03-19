import React, { useState } from 'react';
import { useLocation } from 'wouter';
import { Select } from '@base-ui/react/select';
import './LoginPage.css';

const BetaInterstitialPage: React.FC = () => {
    const [, navigate] = useLocation();
    const [utility, setUtility] = useState<string>("");
    const [battery, setBattery] = useState<string>("");

    const JOIN_FORM_URL = import.meta.env.VITE_JOIN_FORM_URL;

    const isSupported = (
        (utility === 'ameren' || utility === 'comed') &&
        (battery === 'franklin' || battery === 'tesla')
    );
    const isOther = (utility === 'other' || battery === 'other');

    const handleContinue = () => {
        navigate('/new-site');
    };

    return (
        <div className="auth-page">
            <div className="auth-card" style={{ maxWidth: '440px' }}>
                <h1 className="beta-interstitial-title">RateRudder Beta</h1>
                <p className="beta-interstitial-desc">
                    To get started, please confirm your equipment and utility provider.
                </p>

                <div className="beta-interstitial-form">
                    <div>
                        <label id="utility-label" className="beta-interstitial-label">Utility Provider</label>
                        <Select.Root
                            value={utility}
                            onValueChange={(value) => setUtility(value as string)}
                        >
                            <Select.Trigger aria-labelledby="utility-label" className="select-trigger">
                                <Select.Value placeholder="Select your utility...">
                                    {utility === 'ameren' ? 'Ameren' : utility === 'comed' ? 'ComEd' : utility === 'other' ? 'Other' : 'Select your utility...'}
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
                                        <Select.Item value="ameren" className="select-item">
                                            <Select.ItemText>Ameren</Select.ItemText>
                                        </Select.Item>
                                        <Select.Item value="comed" className="select-item">
                                            <Select.ItemText>ComEd</Select.ItemText>
                                        </Select.Item>
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
                            <Select.Trigger aria-labelledby="battery-label" className="select-trigger">
                                <Select.Value placeholder="Select your battery...">
                                    {battery === 'franklin' ? 'FranklinWH' : battery === 'tesla' ? 'Tesla' : battery === 'other' ? 'Other' : 'Select your battery...'}
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
                                        <Select.Item value="franklin" className="select-item">
                                            <Select.ItemText>FranklinWH</Select.ItemText>
                                        </Select.Item>
                                        <Select.Item value="tesla" className="select-item">
                                            <Select.ItemText>Tesla</Select.ItemText>
                                        </Select.Item>
                                        <Select.Item value="other" className="select-item">
                                            <Select.ItemText>Other</Select.ItemText>
                                        </Select.Item>
                                    </Select.Popup>
                                </Select.Positioner>
                            </Select.Portal>
                        </Select.Root>
                    </div>
                </div>

                {isOther && (
                    <div className="beta-interstitial-feedback">
                        <p style={{ marginBottom: '0.75rem' }}>
                            We're currently in a limited beta and expanding our support quickly based on demand.
                        </p>
                        <p>
                            {JOIN_FORM_URL ? (
                                <a
                                    href={JOIN_FORM_URL}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    className="beta-interstitial-feedback-link"
                                >
                                    Please express your interest so we know what to support next!
                                </a>
                            ) : (
                                <span style={{ fontWeight: '600' }}>Please express your interest so we know what to support next!</span>
                            )}
                        </p>
                    </div>
                )}

                {isSupported && (
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
