import React, { useState } from 'react';
import { Select } from '@base-ui/react/select';
import { Combobox } from '@base-ui/react/combobox';
import { submitInterest, type UtilityProviderInfo, type ESSProviderInfo } from '../api';

interface InterestFormProps {
    utilitiesList: UtilityProviderInfo[];
    essList?: ESSProviderInfo[];
    hideBattery?: boolean;
    alwaysShowDetails?: boolean;
    onContinue?: () => void;
    header?: React.ReactNode;
}

export const InterestForm: React.FC<InterestFormProps> = ({
    utilitiesList,
    essList = [],
    hideBattery = false,
    alwaysShowDetails = false,
    onContinue,
    header
}) => {
    const [utility, setUtility] = useState<string>("");
    const [battery, setBattery] = useState<string>("");

    // New fields for "Other" / Details
    const [utilityProviderName, setUtilityProviderName] = useState("");
    const [state, setState] = useState("");
    const [planName, setPlanName] = useState("");
    const [batteryName, setBatteryName] = useState("");
    const [comments, setComments] = useState("");

    const [isSubmitting, setIsSubmitting] = useState(false);
    const [isSubmitted, setIsSubmitted] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const isOther = utility === 'other' || (!hideBattery && battery === 'other');
    const showDetails = alwaysShowDetails ? !!utility : isOther;

    const handleSubmitInterest = async () => {
        setIsSubmitting(true);
        setError(null);
        try {
            await submitInterest({
                utility,
                battery: hideBattery ? 'none' : battery,
                utilityProviderName: utility === 'other' ? utilityProviderName : '',
                state,
                planName,
                batteryName: (!hideBattery && battery === 'other') ? batteryName : '',
                comments
            });
            setIsSubmitted(true);
        } catch (err: any) {
            setError(err.message || "Failed to submit interest");
        } finally {
            setIsSubmitting(false);
        }
    };

    const sortedUtilities = utilitiesList
        .filter(u => !u.hidden)
        .map(u => ({ label: u.name, value: u.id }))
        .sort((a, b) => a.label.localeCompare(b.label));
    sortedUtilities.push({ label: 'Other', value: 'other' });

    return (
        <div>
            {!isSubmitted && (
                <>
                    {header}
                    <div className="beta-interstitial-form">
                    <div>
                        <label id="utility-label" htmlFor="utility" className="beta-interstitial-label">Utility Provider</label>
                        <Combobox.Root
                            value={utility || ''}
                            onValueChange={(value) => setUtility(value as string)}
                            items={sortedUtilities}
                            itemToStringLabel={(val) => {
                                if (val === 'other') return 'Other';
                                return utilitiesList.find(u => u.id === val)?.name || val;
                            }}
                        >
                            <div className="combobox-input-wrapper select-trigger">
                                <Combobox.Input
                                    placeholder="Select your utility..."
                                    id="utility"
                                    className="combobox-input"
                                />
                                <Combobox.Trigger className="combobox-trigger" aria-label="Open popup">
                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
                                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                    </svg>
                                </Combobox.Trigger>
                            </div>
                            <Combobox.Portal>
                                <Combobox.Positioner style={{ zIndex: 3000, width: 'var(--anchor-width)' }}>
                                    <Combobox.Popup className="select-popup">
                                        <Combobox.Empty>
                                            <div className="select-item" style={{ pointerEvents: 'none' }}>No utilities found.</div>
                                        </Combobox.Empty>
                                        <Combobox.List>
                                            {(item: { label: string, value: string }) => (
                                                <Combobox.Item key={item.value} value={item.value} className="select-item">
                                                    {item.label}
                                                </Combobox.Item>
                                            )}
                                        </Combobox.List>
                                    </Combobox.Popup>
                                </Combobox.Positioner>
                            </Combobox.Portal>
                        </Combobox.Root>
                    </div>

                    {!hideBattery && (
                        <div>
                            <label id="battery-label" className="beta-interstitial-label">Battery System</label>
                            <Select.Root
                                value={battery}
                                onValueChange={(value) => setBattery(value as string)}
                            >
                                <Select.Trigger aria-labelledby="battery-label" className="select-trigger">
                                    <Select.Value placeholder="Select your battery...">
                                        {battery === 'other' ? 'Other' : essList.find(e => e.id === battery)?.name || 'Select your battery...'}
                                    </Select.Value>
                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                        </svg>
                                    </Select.Icon>
                                </Select.Trigger>
                                <Select.Portal>
                                    <Select.Positioner style={{ zIndex: 3000, width: 'var(--anchor-width)' }}>
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
                    )}
                </div>
                </>
            )}

            {showDetails && !isSubmitted && (
                <div className="beta-interstitial-form" style={{ marginTop: '1.5rem', borderTop: '1px solid var(--border)', paddingTop: '1.5rem' }}>
                    {utility === 'other' && (
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
                    )}

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
                            <label htmlFor="planName" className="beta-interstitial-label">Rate / Plan Name</label>
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

                    {!hideBattery && battery === 'other' && (
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

                    {error && <div className="error-message" style={{ marginBottom: '1rem' }}>{error}</div>}

                    <button
                        onClick={handleSubmitInterest}
                        disabled={isSubmitting}
                        className="btn beta-interstitial-continue-btn"
                        type="button"
                    >
                        {isSubmitting && <span className="loading-spinner" aria-hidden="true"></span>}
                        {isSubmitting ? 'Submitting...' : 'Express Interest'}
                    </button>
                </div>
            )}

            {isSubmitted && (
                <div className="beta-interstitial-feedback success" style={{ marginTop: '1rem' }}>
                    <p style={{ fontWeight: '700', marginBottom: '0.5rem', fontSize: '1.1rem' }}>Success!</p>
                    <p>
                        We've received your interest! We'll notify you via email when your utility and system are supported.
                    </p>
                </div>
            )}

            {!alwaysShowDetails && !isOther && utility && battery && !isSubmitted && onContinue && (
                <button
                    onClick={onContinue}
                    className="btn beta-interstitial-continue-btn"
                    type="button"
                >
                    You're all set! Let's start saving money 🚀
                </button>
            )}
        </div>
    );
};
