import { useEffect, useState, useRef } from 'react';
import { useLocation } from 'wouter';
import { updateSettings, fetchUtilities, fetchESSList, submitESSStage, type Settings as SettingsType, type UtilityProviderInfo, type UtilityRateOption, type ESSProviderInfo, type ESSCredentialField, type CredentialsPayload } from '../api';
import { Field } from '@base-ui/react/field';
import { Input } from '@base-ui/react/input';
import { Button } from '@base-ui/react/button';
import { Switch } from '@base-ui/react/switch';
import { Select } from '@base-ui/react/select';
import { Combobox } from '@base-ui/react/combobox';
import { Dialog } from '@base-ui/react/dialog';
import { InterestForm } from '../components/InterestForm';
import './Settings.css';

const countries = [
    { label: 'United States', value: 'US' },
    { label: 'United Kingdom', value: 'GB' },
    { label: 'Canada', value: 'CA' },
    { label: 'Australia', value: 'AU' },
    { label: 'Germany', value: 'DE' },
    { label: 'France', value: 'FR' },
    { label: 'Italy', value: 'IT' },
    { label: 'Spain', value: 'ES' },
    { label: 'Netherlands', value: 'NL' },
    { label: 'Belgium', value: 'BE' },
    { label: 'Switzerland', value: 'CH' },
    { label: 'Austria', value: 'AT' },
    { label: 'Sweden', value: 'SE' },
    { label: 'Norway', value: 'NO' },
    { label: 'Denmark', value: 'DK' },
    { label: 'Finland', value: 'FI' },
    { label: 'Ireland', value: 'IE' },
    { label: 'New Zealand', value: 'NZ' },
    { label: 'Japan', value: 'JP' },
    { label: 'South Korea', value: 'KR' },
    { label: 'Singapore', value: 'SG' },
    { label: 'Brazil', value: 'BR' },
    { label: 'Mexico', value: 'MX' },
    { label: 'India', value: 'IN' },
    { label: 'South Africa', value: 'ZA' }
];

interface LocationFormProps {
    settings: SettingsType;
    onChange: <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => void;
}

const LocationForm = ({ settings, onChange }: LocationFormProps) => {
    return (
        <>
            <Field.Root className="form-group">
                <Field.Label htmlFor="countryCode">Country</Field.Label>
                <Combobox.Root
                    value={settings.countryCode || ''}
                    onValueChange={(val) => onChange("countryCode", val as string)}
                    items={countries}
                    itemToStringLabel={(val) => countries.find(c => c.value === val)?.label || val}
                >
                    <div className="combobox-input-wrapper select-trigger">
                        <Combobox.Input placeholder="Select a country..." id="countryCode" className="combobox-input" />
                        <Combobox.Trigger className="combobox-trigger">
                            <svg width="15" height="15" viewBox="0 0 15 15" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                <path d="M4.18179 6.18181C4.35753 6.00608 4.64245 6.00608 4.81819 6.18181L7.49999 8.86362L10.1818 6.18181C10.3575 6.00608 10.6424 6.00608 10.8182 6.18181C10.9939 6.35755 10.9939 6.64247 10.8182 6.81821L7.81819 9.81821C7.73379 9.9026 7.61934 9.95001 7.49999 9.95001C7.38064 9.95001 7.26618 9.9026 7.18179 9.81821L4.18179 6.81821C4.00605 6.64247 4.00605 6.35755 4.18179 6.18181Z" fill="currentColor" fillRule="evenodd" clipRule="evenodd"></path>
                            </svg>
                        </Combobox.Trigger>
                    </div>
                    <Combobox.Portal>
                        <Combobox.Positioner className="select-positioner">
                            <Combobox.Popup className="select-popup">
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
            </Field.Root>

            <Field.Root className="form-group">
                <Field.Label htmlFor="postalCode">Zip/Postal Code</Field.Label>
                <Input
                    id="postalCode"
                    type="text"
                    value={settings.postalCode || ''}
                    onChange={(e) => onChange("postalCode", e.target.value)}
                    placeholder="e.g. 90210"
                />
            </Field.Root>

            <Field.Root className="form-group" style={{ display: 'none' }}>
                <Field.Label htmlFor="solarDirection">Roof Solar Panel Direction</Field.Label>
                <Select.Root
                    value={(settings.solarTilt && settings.solarTilt > 0) ? (settings.solarAzimuth?.toString() || "") : ""}
                    onValueChange={(val) => {
                        const azimuth = parseInt(val as string, 10);
                        onChange("solarAzimuth", azimuth);
                        onChange("solarTilt", 25);
                    }}
                >
                    <Select.Trigger className="select-trigger" aria-label="Solar Direction">
                        <Select.Value placeholder="Select direction...">
                            {settings.solarTilt && settings.solarTilt > 0 ? (
                                ({
                                    "0": "North",
                                    "45": "Northeast",
                                    "90": "East",
                                    "135": "Southeast",
                                    "180": "South",
                                    "225": "Southwest",
                                    "270": "West",
                                    "315": "Northwest"
                                } as Record<string, string>)[settings.solarAzimuth?.toString() || ""]
                            ) : null}
                        </Select.Value>
                        <Select.Icon className="select-icon">
                            <svg width="15" height="15" viewBox="0 0 15 15" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                <path d="M4.18179 6.18181C4.35753 6.00608 4.64245 6.00608 4.81819 6.18181L7.49999 8.86362L10.1818 6.18181C10.3575 6.00608 10.6424 6.00608 10.8182 6.18181C10.9939 6.35755 10.9939 6.64247 10.8182 6.81821L7.81819 9.81821C7.73379 9.9026 7.61934 9.95001 7.49999 9.95001C7.38064 9.95001 7.26618 9.9026 7.18179 9.81821L4.18179 6.81821C4.00605 6.64247 4.00605 6.35755 4.18179 6.18181Z" fill="currentColor" fillRule="evenodd" clipRule="evenodd"></path>
                            </svg>
                        </Select.Icon>
                    </Select.Trigger>
                    <Select.Portal>
                        <Select.Positioner className="select-positioner">
                            <Select.Popup className="select-popup">
                                <Select.List>
                                    <Select.Item className="select-item" value="0"><Select.ItemText>North</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="45"><Select.ItemText>Northeast</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="90"><Select.ItemText>East</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="135"><Select.ItemText>Southeast</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="180"><Select.ItemText>South</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="225"><Select.ItemText>Southwest</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="270"><Select.ItemText>West</Select.ItemText></Select.Item>
                                    <Select.Item className="select-item" value="315"><Select.ItemText>Northwest</Select.ItemText></Select.Item>
                                </Select.List>
                            </Select.Popup>
                        </Select.Positioner>
                    </Select.Portal>
                </Select.Root>
            </Field.Root>
        </>
    );
};

interface UtilityFormProps {
    settings: SettingsType;
    onChange: <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => void;
    utilities: UtilityProviderInfo[];
    isWizard?: boolean;
    editUtility?: boolean;
    setEditUtility?: (val: boolean) => void;
    isUtilityDirty?: boolean;
    setIsUtilityDirty?: (val: boolean) => void;
}

const UtilityForm = ({
    settings,
    onChange,
    utilities,
    isWizard = false,
    editUtility = false,
    setEditUtility,
    isUtilityDirty = false,
    setIsUtilityDirty
}: UtilityFormProps) => {
    const sortedUtilities = utilities
        .filter(u => !u.hidden || u.id === settings.utilityProvider)
        .map(u => ({ label: u.name, value: u.id }))
        .sort((a, b) => a.label.localeCompare(b.label));

    const renderFormFields = () => (
        <>
            <Field.Root className="form-group">
                <Field.Label htmlFor={isWizard ? "wizardUtilityService" : "utilityService"}>
                    {isWizard ? "Utility Provider" : "Service"}
                </Field.Label>
                <Combobox.Root
                    value={settings.utilityProvider || ''}
                    onValueChange={(value) => {
                        if (setEditUtility) setEditUtility(true);
                        if (setIsUtilityDirty) setIsUtilityDirty(true);
                        const providerID = value as string;
                        const provider = utilities.find(u => u.id === providerID);
                        const newSettings = {
                            ...settings,
                            utilityProvider: providerID,
                            utilityRate: "",
                            utilityRateOptions: {}
                        };

                        if (provider?.rates && provider.rates.length === 1) {
                            const rate = provider.rates[0];
                            newSettings.utilityRate = rate.id;
                            const newOpts: Record<string, string | number | boolean> = {};
                            (rate.options || []).forEach((opt: UtilityRateOption) => {
                                newOpts[opt.field] = opt.default;
                            });
                            newSettings.utilityRateOptions = newOpts;
                        }

                        onChange('utilityProvider', newSettings.utilityProvider);
                        onChange('utilityRate', newSettings.utilityRate);
                        onChange('utilityRateOptions', newSettings.utilityRateOptions);
                    }}
                    items={sortedUtilities}
                    itemToStringLabel={(val) => utilities.find(u => u.id === val)?.name || val}
                >
                    <div className="combobox-input-wrapper select-trigger">
                        <Combobox.Input
                            placeholder="Select a service..."
                            id={isWizard ? "wizardUtilityService" : "utilityService"}
                            className="combobox-input"
                        />
                        <Combobox.Trigger className="combobox-trigger" aria-label={isWizard ? "Utility Provider" : "Open popup"}>
                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                                <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                            </svg>
                        </Combobox.Trigger>
                    </div>
                    <Combobox.Portal>
                        <Combobox.Positioner className="select-positioner">
                            <Combobox.Popup className="select-popup">
                                <Combobox.Empty>
                                    <div className="select-item" style={{ pointerEvents: 'none' }}>No services found.</div>
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
            </Field.Root>

            {settings.utilityProvider && (
                <Field.Root className="form-group">
                    <Field.Label htmlFor={isWizard ? "wizardUtilityRate" : "utilityRate"}>{isWizard ? "Rate Plan" : "Rate/Plan"}</Field.Label>
                    <Select.Root
                        value={settings.utilityRate || ""}
                        onValueChange={(value) => {
                            if (setIsUtilityDirty) setIsUtilityDirty(true);
                            const rateID = value as string;
                            const provider = utilities.find(u => u.id === settings.utilityProvider);
                            const rate = (provider?.rates || []).find(r => r.id === rateID);
                            onChange('utilityRate', rateID);
                            if (rate) {
                                const newOpts: Record<string, string | number | boolean> = {};
                                (rate.options || []).forEach((opt: UtilityRateOption) => {
                                    newOpts[opt.field] = opt.default;
                                });
                                onChange('utilityRateOptions', newOpts);
                            } else {
                                onChange('utilityRateOptions', {});
                            }
                        }}
                    >
                        <Select.Trigger className="select-trigger" id={isWizard ? "wizardUtilityRate" : "utilityRate"} aria-label={isWizard ? "Rate Plan" : "Rate/Plan"}>
                            <Select.Value placeholder="Select rate plan...">
                                {(utilities.find(u => u.id === settings.utilityProvider)?.rates || []).find(r => r.id === settings.utilityRate)?.name || 'Select rate plan...'}
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
                                    <Select.Item className="select-item" value="">
                                        <Select.ItemText>Select a rate/plan...</Select.ItemText>
                                    </Select.Item>
                                    {(utilities.find(u => u.id === settings.utilityProvider)?.rates || []).map(r => (
                                        <Select.Item key={r.id} className="select-item" value={r.id}>
                                            <Select.ItemText>{r.name}</Select.ItemText>
                                        </Select.Item>
                                    ))}
                                </Select.Popup>
                            </Select.Positioner>
                        </Select.Portal>
                    </Select.Root>
                </Field.Root>
            )}

            {settings.utilityProvider && settings.utilityRate && (() => {
                const provider = utilities.find(u => u.id === settings.utilityProvider);
                const rate = (provider?.rates || []).find(r => r.id === settings.utilityRate);
                if (!rate || !rate.options) return null;
                const visibleOptions = rate.options.filter((opt: UtilityRateOption) => !opt.hidden);
                if (visibleOptions.length === 0) return null;

                return (
                    <div className="sub-section">
                        {visibleOptions.map((opt: UtilityRateOption) => (
                            <Field.Root key={opt.field} className={`form-group ${opt.type === 'switch' ? 'switch-group' : ''}`}>
                                {opt.type === 'select' && (
                                    <>
                                        <Field.Label htmlFor={`opt-${opt.field}`}>{opt.name}</Field.Label>
                                        <Select.Root
                                            value={settings.utilityRateOptions?.[opt.field] || opt.default}
                                            onValueChange={(value) => {
                                                const newOpts = {
                                                    ...settings.utilityRateOptions,
                                                    [opt.field]: value
                                                };
                                                onChange('utilityRateOptions', newOpts);
                                                if (setIsUtilityDirty) setIsUtilityDirty(true);
                                            }}
                                        >
                                            <Select.Trigger className="select-trigger" id={`opt-${opt.field}`}>
                                                <Select.Value>
                                                    {opt.choices?.find(c => c.value === (settings.utilityRateOptions?.[opt.field] || opt.default))?.name || (settings.utilityRateOptions?.[opt.field] || opt.default)}
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
                                                        {opt.choices?.map((choice) => (
                                                            <Select.Item key={choice.value} className="select-item" value={choice.value}>
                                                                <Select.ItemText>{choice.name}</Select.ItemText>
                                                            </Select.Item>
                                                        ))}
                                                    </Select.Popup>
                                                </Select.Positioner>
                                            </Select.Portal>
                                        </Select.Root>
                                    </>
                                )}
                                {opt.type === 'switch' && (
                                    <>
                                        <div className="switch-row">
                                            <Switch.Root
                                                id={`opt-${opt.field}`}
                                                checked={settings.utilityRateOptions?.[opt.field] ?? !!opt.default}
                                                onCheckedChange={(checked) => {
                                                    const newOpts = {
                                                        ...settings.utilityRateOptions,
                                                        [opt.field]: checked
                                                    };
                                                    onChange('utilityRateOptions', newOpts);
                                                    if (setIsUtilityDirty) setIsUtilityDirty(true);
                                                }}
                                                className="switch-root"
                                            >
                                                <Switch.Thumb className="switch-thumb" />
                                            </Switch.Root>
                                            <Field.Label htmlFor={`opt-${opt.field}`}>{opt.name}</Field.Label>
                                        </div>
                                    </>
                                )}
                                {opt.description && <Field.Description>{opt.description}</Field.Description>}
                            </Field.Root>
                        ))}
                    </div>
                );
            })()}

            {!isWizard && (
                <div className="utility-rate-unsupported-link">
                    <Dialog.Root>
                        <Dialog.Trigger className="inline-link" type="button">
                            Don't see your rate or options?
                        </Dialog.Trigger>
                        <Dialog.Portal>
                            <Dialog.Backdrop className="dialog-backdrop" />
                            <Dialog.Popup className="dialog-popup">
                                <Dialog.Title className="dialog-title">Request a Rate or Option</Dialog.Title>
                                <Dialog.Description className="dialog-description">
                                    Let us know which utility provider, state, or rate plan options you need.
                                </Dialog.Description>
                                <InterestForm
                                    utilitiesList={utilities}
                                    hideBattery={true}
                                    alwaysShowDetails={true}
                                />
                                <Dialog.Close className="btn btn-secondary dialog-close-btn" type="button">
                                    Close
                                </Dialog.Close>
                            </Dialog.Popup>
                        </Dialog.Portal>
                    </Dialog.Root>
                </div>
            )}

            {!isWizard && editUtility && setEditUtility && (
                <button type="button" className="text-button cancel-button" onClick={() => setEditUtility(false)} aria-label="Finish editing Utility Service">Done</button>
            )}
        </>
    );

    if (isWizard) {
        return renderFormFields();
    }

    return (
        <>
            {settings.utilityProvider && settings.utilityRate && !editUtility && setEditUtility ? (
                <button type="button" className="configured-summary" onClick={() => setEditUtility(true)} aria-label="Edit Utility Service">
                    <div className="summary-info">
                        <span className="summary-label">
                            {utilities.find(u => u.id === settings.utilityProvider)?.name || settings.utilityProvider}
                        </span>
                        <span className="summary-sublabel">
                            {utilities.find(u => u.id === settings.utilityProvider)?.rates?.find(r => r.id === settings.utilityRate)?.name || settings.utilityRate}
                        </span>
                    </div>
                    <div className={`summary-status ${isUtilityDirty ? 'pending' : ''}`}>
                        {isUtilityDirty ? 'Pending Save' : 'Configured'}
                    </div>
                </button>
            ) : (
                <div className={editUtility ? "edit-section" : ""}>
                    {renderFormFields()}
                </div>
            )}
        </>
    );
};

interface ESSFormProps {
    settings: SettingsType;
    onChange: <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => void;
    essProviders: ESSProviderInfo[];
    essCredentials: Record<string, string>;
    setEssCredentials: React.Dispatch<React.SetStateAction<Record<string, string>>>;
    isESSDirty: boolean;
    setIsESSDirty: (val: boolean) => void;
    isSaving: boolean;
    isStaging: boolean;
    oauthStatus: 'idle' | 'popup_open' | 'success';
    setOauthStatus: (status: 'idle' | 'popup_open' | 'success') => void;
    currentStage: number;
    setCurrentStage: (stage: number | ((prev: number) => number)) => void;
    handleOAuthLogin: (url: string, fieldName: string) => void;
    handleESSContinue: (e?: React.FormEvent | React.MouseEvent) => Promise<void>;
    isWizard?: boolean;
    editESS?: boolean;
    setEditESS?: (val: boolean) => void;
}

const ESSForm = ({
    settings,
    onChange,
    essProviders,
    essCredentials,
    setEssCredentials,
    isESSDirty,
    setIsESSDirty,
    isSaving,
    isStaging,
    oauthStatus,
    setOauthStatus,
    currentStage,
    setCurrentStage,
    handleOAuthLogin,
    handleESSContinue,
    isWizard = false,
    editESS = false,
    setEditESS
}: ESSFormProps) => {
    const provider = essProviders.find(p => p.id === settings.ess);
    const maxStage = provider
        ? (provider.credentials || []).reduce((max, cred) => {
              const stage = cred.stage ?? 0;
              return stage > max ? stage : max;
          }, 0)
        : 0;

    const renderFormFields = () => (
        <>
            <Field.Root className="form-group">
                <Field.Label htmlFor={isWizard ? "wizardESS" : "ess"}>System Type</Field.Label>
                <Select.Root
                    value={settings.ess || ""}
                    onValueChange={(value) => {
                        if (setEditESS) setEditESS(true);
                        setIsESSDirty(true);
                        onChange('ess', value as string);
                        setEssCredentials({});
                        setOauthStatus('idle');
                        setCurrentStage(0);
                    }}
                >
                    <Select.Trigger className="select-trigger" id={isWizard ? "wizardESS" : "ess"} aria-label="ESS Type">
                        <Select.Value placeholder="Select a system type...">
                            {essProviders.find(u => u.id === settings.ess)?.name || 'Select a system type...'}
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
                                <Select.Item className="select-item" value="">
                                    <Select.ItemText>Select a system type...</Select.ItemText>
                                </Select.Item>
                                {essProviders.filter(p => !p.hidden || p.id === settings.ess).map(p => (
                                    <Select.Item key={p.id} className="select-item" value={p.id}>
                                        <Select.ItemText>{p.name}</Select.ItemText>
                                    </Select.Item>
                                ))}
                            </Select.Popup>
                        </Select.Positioner>
                    </Select.Portal>
                </Select.Root>
            </Field.Root>

            {settings.ess && (
                <div style={{ padding: 0 }}>
                    {(() => {
                        const provider = essProviders.find(p => p.id === settings.ess);
                        if (!provider) return null;

                        return (
                            <>
                                {provider.oAuthURLs && Object.keys(provider.oAuthURLs).length > 0 && (
                                    <div style={{ marginBottom: '1rem' }}>
                                        {provider.oAuthKey && provider.oAuthKey.choices && (
                                            <Field.Root className="form-group" style={{ marginBottom: '1rem' }}>
                                                <Field.Label htmlFor={isWizard ? "wizard-oauth-key" : "oauth-key"}>{provider.oAuthKey.name}</Field.Label>
                                                <Select.Root
                                                    value={essCredentials[provider.oAuthKey.field] || provider.oAuthKey.default || ""}
                                                    onValueChange={(value) => {
                                                        setEssCredentials({ ...essCredentials, [provider.oAuthKey!.field]: value as string });
                                                        setIsESSDirty(true);
                                                    }}
                                                >
                                                    <Select.Trigger className="select-trigger" id={isWizard ? "wizard-oauth-key" : "oauth-key"} aria-label={provider.oAuthKey.name}>
                                                        <Select.Value>
                                                            {provider.oAuthKey.choices?.find(c => c.value === (essCredentials[provider.oAuthKey!.field] || provider.oAuthKey!.default))?.name || (essCredentials[provider.oAuthKey!.field] || provider.oAuthKey!.default)}
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
                                                                {provider.oAuthKey.choices.map(c => (
                                                                    <Select.Item key={c.value} className="select-item" value={c.value}>
                                                                        <Select.ItemText>{c.name}</Select.ItemText>
                                                                    </Select.Item>
                                                                ))}
                                                            </Select.Popup>
                                                        </Select.Positioner>
                                                    </Select.Portal>
                                                </Select.Root>
                                                {provider.oAuthKey.description && <Field.Description>{provider.oAuthKey.description}</Field.Description>}
                                            </Field.Root>
                                        )}
                                        {essCredentials.authCode ? (
                                            <div className="success-message" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: 0 }}>
                                                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ marginRight: '0.25rem' }}>
                                                    <polyline points="20 6 9 17 4 12" />
                                                </svg>
                                                Received code! Save Settings below to complete.
                                            </div>
                                        ) : (
                                            <>
                                                <Button
                                                    className="save-button"
                                                    style={{ width: '100%' }}
                                                    disabled={oauthStatus === 'popup_open'}
                                                    onClick={() => {
                                                        const keyVal = provider.oAuthKey ? (essCredentials[provider.oAuthKey.field] || provider.oAuthKey.default || Object.keys(provider.oAuthURLs!)[0]) : Object.keys(provider.oAuthURLs!)[0];
                                                        const url = provider.oAuthURLs![keyVal];
                                                        if (url) {
                                                            handleOAuthLogin(url, "authCode");
                                                        }
                                                    }}
                                                    type="button"
                                                >
                                                    {oauthStatus === 'popup_open' && <span className="loading-spinner" aria-hidden="true"></span>}
                                                    {oauthStatus === 'popup_open' ? 'Awaiting link...' : 'Login to link account'}
                                                </Button>
                                                {oauthStatus === 'popup_open' && (
                                                    <div className="warning-notice" style={{ marginTop: '0.5rem' }}>
                                                        Please complete authentication in the popup window.
                                                    </div>
                                                )}
                                            </>
                                        )}
                                    </div>
                                )}
                                {(provider.credentials || []).filter(cred => ((cred.stage ?? 0) <= currentStage) && !cred.hidden).map(cred => (
                                    <Field.Root key={cred.field} className="form-group">
                                        <Field.Label htmlFor={`ess-${cred.field}`}>{cred.name}</Field.Label>
                                        {cred.type === 'select' ? (
                                            <Select.Root
                                                value={essCredentials[cred.field] || cred.default || ""}
                                                disabled={isSaving || isStaging}
                                                onValueChange={(value) => {
                                                    setEssCredentials({ ...essCredentials, [cred.field]: value as string });
                                                    setIsESSDirty(true);
                                                }}
                                            >
                                                <Select.Trigger className="select-trigger" id={`ess-${cred.field}`} aria-label={cred.name}>
                                                    <Select.Value placeholder={`Select ${cred.name}`}>
                                                        {cred.choices?.find(c => c.value === (essCredentials[cred.field] || cred.default))?.name || (essCredentials[cred.field] || cred.default)}
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
                                                            {cred.choices?.map((choice) => (
                                                                <Select.Item key={choice.value} className="select-item" value={choice.value}>
                                                                    <Select.ItemText>{choice.name}</Select.ItemText>
                                                                </Select.Item>
                                                            ))}
                                                        </Select.Popup>
                                                    </Select.Positioner>
                                                </Select.Portal>
                                            </Select.Root>
                                        ) : (
                                            <Input
                                                id={`ess-${cred.field}`}
                                                value={essCredentials[cred.field] || cred.default || ""}
                                                type={cred.type === 'password' ? 'password' : 'text'}
                                                disabled={isSaving || isStaging}
                                                onChange={(e) => {
                                                    setEssCredentials({ ...essCredentials, [cred.field]: e.target.value });
                                                    setIsESSDirty(true);
                                                }}
                                                placeholder={`Enter ${cred.name}`}
                                            />
                                        )}
                                        {cred.description && <Field.Description>{cred.description}</Field.Description>}
                                    </Field.Root>
                                ))}
                            </>
                        );
                    })()}
                </div>
            )}
        </>
    );

    if (isWizard) {
        return renderFormFields();
    }

    return (
        <>
            {settings.ess && (settings.hasCredentials?.[settings.ess] || isESSDirty) && !editESS ? (
                <button type="button" className="configured-summary" onClick={() => setEditESS && setEditESS(true)} aria-label="Edit Energy Storage System">
                    <div className="summary-info">
                        <span className="summary-label">{essProviders.find(p => p.id === settings.ess)?.name || settings.ess || 'Unknown System'}</span>
                    </div>
                    <div className={`summary-status ${isESSDirty ? 'pending' : ''}`}>
                        {isESSDirty ? 'Pending Save' : 'Connected'}
                    </div>
                </button>
            ) : (
                <div className={editESS ? "edit-section" : ""}>
                    {renderFormFields()}

                    {editESS && (
                        <div className="ess-actions" style={{ display: 'flex', gap: '1rem', marginTop: '1.5rem', alignItems: 'center' }}>
                            {maxStage > currentStage && (
                                <button
                                    type="button"
                                    className="save-button"
                                    style={{ width: 'auto', padding: '0.5rem 1.5rem', marginTop: 0 }}
                                    onClick={handleESSContinue}
                                    disabled={isStaging}
                                >
                                    {isStaging && <span className="loading-spinner" aria-hidden="true"></span>}
                                    {isStaging ? 'Submitting...' : 'Continue'}
                                </button>
                            )}
                            <button
                                type="button"
                                className="text-button cancel-button"
                                style={{ marginTop: 0 }}
                                onClick={() => {
                                    if (setEditESS) setEditESS(false);
                                    setCurrentStage(0);
                                }}
                                aria-label="Finish editing Energy Storage System"
                            >
                                {maxStage > currentStage ? 'Cancel' : 'Done'}
                            </button>
                        </div>
                    )}
                </div>
            )}
        </>
    );
};

const Settings = ({ siteID, settings: parentSettings, onSettingsSaved }: { siteID?: string, settings: SettingsType | null, onSettingsSaved?: () => Promise<void> }) => {
    const [settings, setSettings] = useState<SettingsType | null>(null);
    const [isUtilityDirty, setIsUtilityDirty] = useState(false);
    const [isESSDirty, setIsESSDirty] = useState(false);
    const [utilities, setUtilities] = useState<UtilityProviderInfo[]>([]);
    const [essProviders, setEssProviders] = useState<ESSProviderInfo[]>([]);
    const [loadingLists, setLoadingLists] = useState(true);
    const [isSaving, setIsSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [successMessage, setSuccessMessage] = useState<string | null>(null);
    const [currentStage, setCurrentStage] = useState(0);
    const [isStaging, setIsStaging] = useState(false);

    const loading = loadingLists || !settings;

    const [, navigate] = useLocation();
    const [forceFullSettings, setForceFullSettings] = useState(false);
    const [wizardStep, setWizardStep] = useState(1);
    const [isInWizard, setIsInWizard] = useState<boolean | null>(null);

    useEffect(() => {
        if (settings && isInWizard === null) {
            const isUtility = !!settings.utilityProvider && settings.utilityProvider !== "";
            const isESS = !!settings.ess && settings.ess !== "" && !!settings.hasCredentials?.[settings.ess];
            setIsInWizard(!isUtility && !isESS);
        }
    }, [settings, isInWizard]);

    const [essCredentials, setEssCredentials] = useState<Record<string, string>>({});
    const [oauthStatus, setOauthStatus] = useState<'idle' | 'popup_open' | 'success'>('idle');
    const savingRef = useRef(false);
    const oauthTimerRef = useRef<any>(null);
    const oauthListenerRef = useRef<((event: MessageEvent) => void) | null>(null);

    useEffect(() => {
        return () => {
            if (oauthTimerRef.current) {
                clearInterval(oauthTimerRef.current);
            }
            if (oauthListenerRef.current) {
                window.removeEventListener('message', oauthListenerRef.current);
            }
        };
    }, []);

    // UI State for consolidated views
    const [editUtility, setEditUtility] = useState(false);
    const [editESS, setEditESS] = useState(false);
    const [showAdvanced, setShowAdvanced] = useState(false);

    useEffect(() => {
        if (!siteID) return;
        let active = true;

        const loadLists = async () => {
            try {
                setLoadingLists(true);
                setCurrentStage(0);
                const [utilitiesData, essProvidersData] = await Promise.all([
                    fetchUtilities(siteID),
                    fetchESSList(siteID)
                ]);
                if (active) {
                    setUtilities(utilitiesData);
                    setEssProviders(essProvidersData);
                    setError(null);
                }
            } catch (err) {
                if (active) {
                    setError(err instanceof Error ? err.message : 'Failed to load options');
                }
            } finally {
                if (active) {
                    setLoadingLists(false);
                }
            }
        };

        loadLists();

        return () => {
            active = false;
        };
    }, [siteID]);

    useEffect(() => {
        if (parentSettings) {
            setSettings({
                ...parentSettings,
                gridChargeBatteries: parentSettings.gridChargeBatteries ?? true,
                gridExportSolar: parentSettings.gridExportSolar ?? false,
                gridExportBatteries: parentSettings.gridExportBatteries ?? false
            });
            if (parentSettings.ess && !parentSettings.hasCredentials?.[parentSettings.ess]) {
                setEditESS(true);
                setIsESSDirty(true);
            }
        }
    }, [parentSettings]);

    const handleESSContinue = async (e?: React.FormEvent | React.MouseEvent) => {
        if (e) {
            e.preventDefault();
            e.stopPropagation();
        }
        if (!settings || !settings.ess) return;
        setIsStaging(true);
        setError(null);
        try {
            const provider = essProviders.find(p => p.id === settings.ess);
            if (!provider) throw new Error('Selected ESS provider not found');

            const payload: Record<string, string> = {};
            const stageFields = (provider.credentials || []).filter(cred => (cred.stage ?? 0) <= currentStage);
            for (const cred of stageFields) {
                payload[cred.field] = essCredentials[cred.field] || cred.default || "";
            }

            await submitESSStage(settings.ess, payload, siteID);
            setCurrentStage(prev => prev + 1);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to advance stage');
        } finally {
            setIsStaging(false);
        }
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!settings || savingRef.current) return;

        if (editESS && maxStage > currentStage) {
            handleESSContinue(e);
            return;
        }

        try {
            savingRef.current = true;
            setIsSaving(true);
            setError(null);
            setSuccessMessage(null);

            // Merge default values for utility rate options
            const utilityProvider = utilities.find(u => u.id === settings.utilityProvider);
            const utilityRate = (utilityProvider?.rates || []).find(r => r.id === settings.utilityRate);
            const finalSettings = { ...settings };
            if (utilityRate) {
                const finalOpts = { ...finalSettings.utilityRateOptions };
                (utilityRate.options || []).forEach(opt => {
                    if (finalOpts[opt.field] === undefined || finalOpts[opt.field] === null || finalOpts[opt.field] === "") {
                        if (opt.default !== undefined) {
                            finalOpts[opt.field] = opt.default;
                        }
                    }
                });
                finalSettings.utilityRateOptions = finalOpts;
            }

            let credentialsPayload: CredentialsPayload | undefined = undefined;
            const essProvider = essProviders.find(p => p.id === settings.ess);
            if (essProvider && (isESSDirty || Object.keys(essCredentials).length > 0)) {
                credentialsPayload = { [essProvider.id]: {} };
                const processCred = (cred: ESSCredentialField) => {
                    let val = essCredentials[cred.field];
                    if (val === undefined || val === null || val === "") {
                        val = cred.default;
                    }

                    if (cred.required && (val === undefined || val === null || val === "")) {
                        // TODO: we should probably have the backend define better errors
                        if (cred.field === 'authCode') {
                            setEditESS(true);
                            throw new Error('Login to connect your energy system.');
                        }
                        throw new Error(`The ${cred.name} field is required.`);
                    }
                    if (val !== undefined && val !== null && val !== "") {
                        credentialsPayload![essProvider.id][cred.field] = val;
                    }
                };

                for (const cred of (essProvider.credentials || [])) {
                    processCred(cred);
                }
                if (essProvider.oAuthKey) {
                    processCred(essProvider.oAuthKey);
                }
            }

            await updateSettings(finalSettings, siteID, credentialsPayload);
            setSuccessMessage('Settings saved successfully');

            if (onSettingsSaved) {
                await onSettingsSaved();
            }

            const updatedSettings = credentialsPayload && finalSettings.ess ? {
                ...finalSettings,
                hasCredentials: {
                    ...finalSettings.hasCredentials,
                    [finalSettings.ess]: true
                }
            } : finalSettings;

            if (credentialsPayload && settings.ess) {
                setSettings(updatedSettings);
                setEditESS(false);
                setEssCredentials({});
                setOauthStatus('idle');
            }
            setIsUtilityDirty(false);
            setIsESSDirty(false);

            setTimeout(() => setSuccessMessage(null), 3000);
        } catch (err) {
            const errMsg = err instanceof Error ? err.message : 'Failed to save settings';
            if (errMsg.toLowerCase().includes("authorization code is invalid") || errMsg.toLowerCase().includes("code is invalid")) {
                setError("The authorization code expired. Please click 'Login to link account' again and save immediately.");
                setEditESS(true);
            } else {
                setError(errMsg);
            }
            setOauthStatus('idle');

            const provider = essProviders.find(p => p.id.toLowerCase() === settings.ess?.toLowerCase());
            const oneTimeFields = new Set<string>(['authCode']);
            if (provider) {
                const fields = (provider.credentials || [])
                    .filter(c => c.oneTime)
                    .map(c => c.field);
                fields.forEach(f => oneTimeFields.add(f));
                if (provider.oAuthKey && provider.oAuthKey.oneTime) {
                    oneTimeFields.add(provider.oAuthKey.field);
                }
            }

            setEssCredentials(prev => {
                const next = { ...prev };
                oneTimeFields.forEach(field => {
                    delete next[field];
                });
                return next;
            });
        } finally {
            savingRef.current = false;
            setIsSaving(false);
        }
    };

    const handleChange = <K extends keyof SettingsType>(field: K, value: SettingsType[K]) => {
        setSettings(prev => prev ? ({ ...prev, [field]: value }) : null);
    };

    const scrollToSection = (testId: string) => {
        const element = document.querySelector(`[data-testid="${testId}"]`);
        if (element) {
            element.scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
    };

    const handleWizardSave = async (e: React.FormEvent, step: number) => {
        e.preventDefault();
        if (!settings || savingRef.current) return;

        if (step === 3 && editESS && maxStage > currentStage) {
            handleESSContinue(e);
            return;
        }

        if (step < 3) {
            if (step === 2) {
                const utilityProvider = utilities.find(u => u.id === settings.utilityProvider);
                const utilityRate = (utilityProvider?.rates || []).find(r => r.id === settings.utilityRate);
                if (utilityRate) {
                    const finalOpts = { ...settings.utilityRateOptions };
                    (utilityRate.options || []).forEach(opt => {
                        if (finalOpts[opt.field] === undefined || finalOpts[opt.field] === null || finalOpts[opt.field] === "") {
                            if (opt.default !== undefined) {
                                finalOpts[opt.field] = opt.default;
                            }
                        }
                    });
                    setSettings(prev => prev ? ({ ...prev, utilityRateOptions: finalOpts }) : null);
                }
            }
            setWizardStep(prev => prev + 1);
            return;
        }

        let success = false;
        try {
            savingRef.current = true;
            setIsSaving(true);
            setError(null);
            setSuccessMessage(null);

            let credentialsPayload: CredentialsPayload | undefined = undefined;
            const finalSettings = { ...settings };

            const essProvider = essProviders.find(p => p.id === settings.ess);
            if (essProvider && (isESSDirty || Object.keys(essCredentials).length > 0)) {
                credentialsPayload = { [essProvider.id]: {} };
                const processCred = (cred: ESSCredentialField) => {
                    let val = essCredentials[cred.field];
                    if (val === undefined || val === null || val === "") {
                        val = cred.default;
                    }

                    if (cred.required && (val === undefined || val === null || val === "")) {
                        if (cred.field === 'authCode') {
                            setEditESS(true);
                            throw new Error('Login to connect your energy system.');
                        }
                        throw new Error(`The ${cred.name} field is required.`);
                    }
                    if (val !== undefined && val !== null && val !== "") {
                        credentialsPayload![essProvider.id][cred.field] = val;
                    }
                };

                for (const cred of (essProvider.credentials || [])) {
                    processCred(cred);
                }
                if (essProvider.oAuthKey) {
                    processCred(essProvider.oAuthKey);
                }
            }

            await updateSettings(finalSettings, siteID, credentialsPayload);
            setSuccessMessage('Setup complete!');

            if (onSettingsSaved) {
                await onSettingsSaved();
            }

            if (credentialsPayload && finalSettings.ess) {
                setEditESS(false);
                setEssCredentials({});
                setOauthStatus('idle');
            }
            setIsUtilityDirty(false);
            setIsESSDirty(false);
            setTimeout(() => {
                navigate('/dashboard');
            }, 1000);
            success = true;
        } catch (err) {
            const errMsg = err instanceof Error ? err.message : 'Failed to save settings';
            setError(errMsg);
        } finally {
            if (!success) {
                savingRef.current = false;
                setIsSaving(false);
            }
        }
    };

    const handleOAuthLogin = (url: string, fieldName: string) => {
        const width = 500;
        const height = 600;
        const left = window.screenX + (window.outerWidth - width) / 2;
        const top = window.screenY + (window.outerHeight - height) / 2;

        const oauthUrl = new URL(url);
        // add the siteID as the state parameter
        if (siteID) {
            oauthUrl.searchParams.set('state', siteID);
        }

        const popup = window.open(
            oauthUrl.toString(),
            'OAuthLogin',
            `width=${width},height=${height},left=${left},top=${top},status=yes,scrollbars=yes`
        );

        if (!popup) {
            setError('Please allow popups for this site to log in.');
            return;
        }

        // Clear any existing listener/timer before starting a new one
        if (oauthTimerRef.current) {
            clearInterval(oauthTimerRef.current);
            oauthTimerRef.current = null;
        }
        if (oauthListenerRef.current) {
            window.removeEventListener('message', oauthListenerRef.current);
            oauthListenerRef.current = null;
        }

        setOauthStatus('popup_open');

        const listener = (event: MessageEvent) => {
            if (event.origin !== window.location.origin) return;

            if (event.data && event.data.type === 'OAUTH_CODE') {
                if (siteID && event.data.state !== siteID) {
                    setError('Authentication state mismatch. Please try again.');
                    window.removeEventListener('message', listener);
                    oauthListenerRef.current = null;
                    return;
                }

                setEssCredentials(prev => ({
                    ...prev,
                    [fieldName]: event.data.code
                }));
                setOauthStatus('success');
                setIsESSDirty(true);
                window.removeEventListener('message', listener);
                oauthListenerRef.current = null;
                if (oauthTimerRef.current) {
                    clearInterval(oauthTimerRef.current);
                    oauthTimerRef.current = null;
                }
            }
        };

        window.addEventListener('message', listener);
        oauthListenerRef.current = listener;

        const timer = setInterval(() => {
            if (popup.closed) {
                clearInterval(timer);
                oauthTimerRef.current = null;
                window.removeEventListener('message', listener);
                oauthListenerRef.current = null;
                setOauthStatus(current => {
                    if (current === 'popup_open') {
                        return 'idle';
                    }
                    return current;
                });
            }
        }, 500);
        oauthTimerRef.current = timer;
    };

    if (loading) return (
        <div className="loading-screen">
            <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
            Loading settings...
        </div>
    );
    if (!settings) return <div>Error loading settings</div>;

    const provider = essProviders.find(p => p.id === settings.ess);
    const maxStage = provider
        ? (provider.credentials || []).reduce((max, cred) => {
              const stage = cred.stage ?? 0;
              return stage > max ? stage : max;
          }, 0)
        : 0;

    const isLocationConfigured = settings ? (!!settings.countryCode && !!settings.postalCode) : false;
    const isUtilityConfigured = settings ? (!!settings.utilityProvider && settings.utilityProvider !== "") : false;
    const isESSConfigured = settings ? (!!settings.ess && settings.ess !== "" && !!settings.hasCredentials?.[settings.ess]) : false;

    const showWizard = !!(settings && isInWizard && !forceFullSettings);
    const showChecklist = !!(settings && !showWizard && (!isUtilityConfigured || !isESSConfigured) && !forceFullSettings);

    const locationHighlightClass = showChecklist && !isLocationConfigured ? "highlighted-section" : "";
    const utilityHighlightClass = showChecklist && !isUtilityConfigured ? "highlighted-section" : "";
    const essHighlightClass = showChecklist && !isESSConfigured ? "highlighted-section" : "";

    if (showWizard) {
        return (
            <div className="content-container settings-container wizard-container">
                <div className="wizard-card">
                    <div className="wizard-step-indicators">
                        <div className={`wizard-step-bubble ${wizardStep >= 1 ? 'active' : ''} ${wizardStep > 1 ? 'completed' : ''}`}>
                            {wizardStep > 1 ? '✓' : '1'}
                        </div>
                        <div className="wizard-step-line" />
                        <div className={`wizard-step-bubble ${wizardStep >= 2 ? 'active' : ''} ${wizardStep > 2 ? 'completed' : ''}`}>
                            {wizardStep > 2 ? '✓' : '2'}
                        </div>
                        <div className="wizard-step-line" />
                        <div className={`wizard-step-bubble ${wizardStep >= 3 ? 'active' : ''} ${wizardStep > 3 ? 'completed' : ''}`}>
                            {wizardStep > 3 ? '✓' : '3'}
                        </div>
                    </div>

                    <div className="wizard-header">
                        <h2>
                            {wizardStep === 1 && "Step 1: Set Location"}
                            {wizardStep === 2 && "Step 2: Choose Utility"}
                            {wizardStep === 3 && "Step 3: Connect Battery"}
                        </h2>
                        <p className="wizard-subtitle">
                            {wizardStep === 1 && "Location helps us fetch the correct weather forecast for solar output predictions."}
                            {wizardStep === 2 && "Configure your electricity utility rate plan to optimize battery charging and grid exports."}
                            {wizardStep === 3 && "Connect your Energy Storage System (ESS) to enable automated charging and discharging."}
                        </p>
                    </div>

                    <form onSubmit={(e) => handleWizardSave(e, wizardStep)}>
                        {error && <div className="error-message">{error}</div>}
                        {successMessage && <div className="success-message">{successMessage}</div>}

                        {wizardStep === 1 && (
                            <div className="wizard-step-fields">
                                <LocationForm settings={settings} onChange={handleChange} />
                            </div>
                        )}

                        {wizardStep === 2 && (
                            <div className="wizard-step-fields">
                                <UtilityForm
                                    settings={settings}
                                    onChange={handleChange}
                                    utilities={utilities}
                                    isWizard={true}
                                />

                                <div className="grid-strategy-section" style={{ marginTop: '0.5rem', borderTop: '1px solid var(--outline-variant)', paddingTop: '1rem' }}>
                                    <h4 style={{ fontSize: '0.8rem', fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '0.75rem', marginTop: 0 }}>Grid Restrictions</h4>
                                    <div className="grid-strategy-grid" style={{ background: 'transparent', border: 'none', padding: 0, marginBottom: 0 }}>
                                        <Field.Root className="form-group switch-group compact">
                                            <div className="switch-row">
                                                <Switch.Root
                                                    id="wizard-gridChargeBatteries"
                                                    checked={settings.gridChargeBatteries ?? true}
                                                    onCheckedChange={(checked) => handleChange('gridChargeBatteries', checked)}
                                                    className="switch-root"
                                                >
                                                    <Switch.Thumb className="switch-thumb" />
                                                </Switch.Root>
                                                <Field.Label htmlFor="wizard-gridChargeBatteries">Grid Can Charge Battery</Field.Label>
                                            </div>
                                        </Field.Root>

                                        <Field.Root className="form-group switch-group compact">
                                            <div className="switch-row">
                                                <Switch.Root
                                                    id="wizard-gridExportSolar"
                                                    checked={settings.gridExportSolar ?? false}
                                                    onCheckedChange={(checked) => handleChange('gridExportSolar', checked)}
                                                    className="switch-root"
                                                >
                                                    <Switch.Thumb className="switch-thumb" />
                                                </Switch.Root>
                                                <Field.Label htmlFor="wizard-gridExportSolar">Export Solar to Grid</Field.Label>
                                            </div>
                                        </Field.Root>

                                        <Field.Root className="form-group switch-group compact">
                                            <div className="switch-row">
                                                <Switch.Root
                                                    id="wizard-gridExportBatteries"
                                                    checked={settings.gridExportBatteries ?? false}
                                                    onCheckedChange={(checked) => handleChange('gridExportBatteries', checked)}
                                                    className="switch-root"
                                                >
                                                    <Switch.Thumb className="switch-thumb" />
                                                </Switch.Root>
                                                <Field.Label htmlFor="wizard-gridExportBatteries">Export Battery to Grid</Field.Label>
                                            </div>
                                        </Field.Root>

                                        {!settings.gridChargeBatteries && !settings.gridExportSolar && !settings.gridExportBatteries && (
                                            <div className="warning-notice" style={{ gridColumn: '1 / -1', marginTop: 0 }} data-testid="wizard-grid-restrictions-warning">
                                                Warning: All grid interactions are disabled. The system will only charge from solar and will not charge from the grid or export any energy.
                                            </div>
                                        )}
                                    </div>
                                </div>
                            </div>
                        )}

                        {wizardStep === 3 && (
                            <div className="wizard-step-fields">
                                <ESSForm
                                    settings={settings}
                                    onChange={handleChange}
                                    essProviders={essProviders}
                                    essCredentials={essCredentials}
                                    setEssCredentials={setEssCredentials}
                                    isESSDirty={isESSDirty}
                                    setIsESSDirty={setIsESSDirty}
                                    isSaving={isSaving}
                                    isStaging={isStaging}
                                    oauthStatus={oauthStatus}
                                    setOauthStatus={setOauthStatus}
                                    currentStage={currentStage}
                                    setCurrentStage={setCurrentStage}
                                    handleOAuthLogin={handleOAuthLogin}
                                    handleESSContinue={handleESSContinue}
                                    isWizard={true}
                                />
                            </div>
                        )}

                        <div className="wizard-actions">
                            {wizardStep > 1 && (
                                <button
                                    type="button"
                                    className="btn btn-secondary"
                                    onClick={() => setWizardStep(prev => prev - 1)}
                                    disabled={isSaving || isStaging}
                                >
                                    Back
                                </button>
                            )}
                            <button
                                type="submit"
                                className="btn btn-primary"
                                disabled={isSaving || isStaging || (wizardStep === 2 && !settings.utilityProvider) || (wizardStep === 3 && !settings.ess)}
                            >
                                {isSaving || isStaging ? (
                                    <>
                                        <span className="loading-spinner" aria-hidden="true" style={{ marginRight: '0.5rem' }}></span>
                                        Saving...
                                    </>
                                ) : (
                                    wizardStep === 3 ? 'Complete Setup' : 'Save & Continue'
                                )}
                            </button>
                        </div>
                    </form>

                    <div className="wizard-footer">
                        <button
                            type="button"
                            className="text-button wizard-skip-link"
                            onClick={() => {
                                setForceFullSettings(true);
                                setIsInWizard(false);
                            }}
                        >
                            Skip setup wizard and view all settings
                        </button>
                    </div>
                </div>
            </div>
        );
    }

    return (
        <div className="content-container settings-container">
            {showChecklist && (
                <div className="checklist-banner font-sans" data-testid="checklist-banner">
                    <h4>Getting Started Checklist</h4>
                    <p>Configure the following settings to enable RateRudder's automation:</p>
                    <div className="checklist-items">
                        <div
                            className={`checklist-item ${isLocationConfigured ? 'checked' : 'pending'}`}
                            onClick={() => scrollToSection('location-section')}
                        >
                            <span className="checklist-icon">{isLocationConfigured ? '✓' : '○'}</span>
                            <span className="checklist-text">Set Location</span>
                        </div>
                        <div
                            className={`checklist-item ${isUtilityConfigured ? 'checked' : 'pending'}`}
                            onClick={() => scrollToSection('utility-section')}
                        >
                            <span className="checklist-icon">{isUtilityConfigured ? '✓' : '○'}</span>
                            <span className="checklist-text">Choose Utility</span>
                        </div>
                        <div
                            className={`checklist-item ${isESSConfigured ? 'checked' : 'pending'}`}
                            onClick={() => scrollToSection('ess-section')}
                        >
                            <span className="checklist-icon">{isESSConfigured ? '✓' : '○'}</span>
                            <span className="checklist-text">Connect Battery (ESS)</span>
                        </div>
                    </div>
                </div>
            )}
            <h2>Settings</h2>
            <form onSubmit={handleSubmit}>
                {/* Quick Settings at the top */}
                <div className="section-header">
                    <h3>Quick Settings</h3>
                </div>
                <div className="form-grid compact-grid">
                    <Field.Root className="form-group switch-group compact">
                        <div className="switch-row">
                            <Switch.Root
                                checked={settings.pause}
                                onCheckedChange={(checked) => handleChange('pause', checked)}
                                className="switch-root"
                            >
                                <Switch.Thumb className="switch-thumb" />
                            </Switch.Root>
                            <Field.Label>Pause Automation</Field.Label>
                        </div>
                        <Field.Description>If enabled, stop changing states but continue monitoring.</Field.Description>
                    </Field.Root>

                    <Field.Root className="form-group compact">
                        <Field.Label htmlFor="minBatterySOC">Minimum Battery %</Field.Label>
                        <Input
                            id="minBatterySOC"
                            type="number"
                            step="1"
                            min="0"
                            max="100"
                            value={settings.minBatterySOC}
                            onChange={(e) => handleChange('minBatterySOC', parseFloat(e.target.value))}
                        />
                        <Field.Description>Maintain battery charge at or above this level at all costs.</Field.Description>
                    </Field.Root>
                </div>

                {/* Location directly under Quick Settings */}
                <div className={`settings-section ${locationHighlightClass}`} data-testid="location-section">
                    <div className="section-header">
                        <h3>Location</h3>
                    </div>
                    <div className="grid-strategy-grid">
                        <LocationForm settings={settings} onChange={handleChange} />
                    </div>

                <div className="weather-attribution">
                    Weather data provided by <a href="https://open-meteo.com" target="_blank" rel="noopener noreferrer">Open-Meteo</a> to improve solar prediction
                </div>
                </div>

                {/* Utility Service Section */}
                <div className={`settings-section ${utilityHighlightClass}`} data-testid="utility-section">
                    <div className="section-header">
                        <h3>Utility Service</h3>
                        {settings.utilityProvider && settings.utilityRate && !editUtility && (
                        <button type="button" className="text-button" onClick={() => setEditUtility(true)} aria-label="Change Utility Service">Change</button>
                    )}
                </div>

                <UtilityForm
                    settings={settings}
                    onChange={handleChange}
                    utilities={utilities}
                    isWizard={false}
                    editUtility={editUtility}
                    setEditUtility={setEditUtility}
                    isUtilityDirty={isUtilityDirty}
                    setIsUtilityDirty={setIsUtilityDirty}
                />

                <div className="grid-strategy-grid">
                    <Field.Root className="form-group switch-group compact">
                        <div className="switch-row">
                            <Switch.Root
                                id="gridChargeBatteries"
                                checked={settings.gridChargeBatteries}
                                onCheckedChange={(checked) => handleChange('gridChargeBatteries', checked)}
                                className="switch-root"
                            >
                                <Switch.Thumb className="switch-thumb" />
                            </Switch.Root>
                            <Field.Label htmlFor="gridChargeBatteries">Grid Can Charge Battery</Field.Label>
                        </div>
                    </Field.Root>

                    <Field.Root className="form-group switch-group compact">
                        <div className="switch-row">
                            <Switch.Root
                                id="gridExportSolar"
                                checked={settings.gridExportSolar}
                                onCheckedChange={(checked) => handleChange('gridExportSolar', checked)}
                                className="switch-root"
                            >
                                <Switch.Thumb className="switch-thumb" />
                            </Switch.Root>
                            <Field.Label htmlFor="gridExportSolar">Export Solar to Grid</Field.Label>
                        </div>
                    </Field.Root>

                    <Field.Root className="form-group switch-group compact">
                        <div className="switch-row">
                            <Switch.Root
                                id="gridExportBatteries"
                                checked={settings.gridExportBatteries}
                                onCheckedChange={(checked) => handleChange('gridExportBatteries', checked)}
                                className="switch-root"
                            >
                                <Switch.Thumb className="switch-thumb" />
                            </Switch.Root>
                            <Field.Label htmlFor="gridExportBatteries">Export Battery to Grid</Field.Label>
                        </div>
                    </Field.Root>

                    {!settings.gridChargeBatteries && !settings.gridExportSolar && !settings.gridExportBatteries && (
                        <div className="warning-notice" style={{ gridColumn: '1 / -1', marginTop: 0 }} data-testid="grid-restrictions-warning">
                            Warning: All grid interactions are disabled. The system will only charge from solar and will not charge from the grid or export any energy.
                        </div>
                    )}
                </div>

                </div>

                {/* ESS Configuration Section */}
                <div className={`settings-section ${essHighlightClass}`} data-testid="ess-section">
                    <div className="section-header">
                        <h3 id="ess-credentials">Energy Storage System</h3>
                        {settings.ess && settings.hasCredentials?.[settings.ess] && !editESS && (
                        <button type="button" className="text-button" onClick={() => setEditESS(true)} aria-label="Update Energy Storage System">Update</button>
                    )}
                </div>

                <ESSForm
                    settings={settings}
                    onChange={handleChange}
                    essProviders={essProviders}
                    essCredentials={essCredentials}
                    setEssCredentials={setEssCredentials}
                    isESSDirty={isESSDirty}
                    setIsESSDirty={setIsESSDirty}
                    isSaving={isSaving}
                    isStaging={isStaging}
                    oauthStatus={oauthStatus}
                    setOauthStatus={setOauthStatus}
                    currentStage={currentStage}
                    setCurrentStage={setCurrentStage}
                    handleOAuthLogin={handleOAuthLogin}
                    handleESSContinue={handleESSContinue}
                    isWizard={false}
                    editESS={editESS}
                    setEditESS={setEditESS}
                />
                </div>

                {!showAdvanced ? (
                    <div className="advanced-trigger-section">
                        <hr className="settings-separator" />
                        <button
                            type="button"
                            className="btn btn-secondary show-advanced-btn"
                            onClick={() => setShowAdvanced(true)}
                        >
                            Show Advanced Settings
                        </button>
                    </div>
                ) : (
                    <>
                        <div className="section-header">
                            <h3>Advanced Automation Overrides</h3>
                        </div>

                        <div className="grid-strategy-grid">
                            <Field.Root className="form-group">
                                <Field.Label htmlFor="alwaysChargeUnder">Always Charge Below ($/kWh)</Field.Label>
                                <Input
                                    id="alwaysChargeUnder"
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={settings.alwaysChargeUnderDollarsPerKWH}
                                    onChange={(e) => handleChange('alwaysChargeUnderDollarsPerKWH', parseFloat(e.target.value))}
                                />
                                <Field.Description>Charge battery whenever the price is less than this threshold.</Field.Description>
                                {settings.alwaysChargeUnderDollarsPerKWH > 0.05 && (
                                    <div className="warning-text" style={{ color: 'orange', marginTop: '4px', fontSize: '0.9em' }}>
                                        Are you sure you want to force charging the batteries from the grid when it's below this price?
                                    </div>
                                )}
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="minArbitrage">Minimum Arbitrage Profit ($/kWh)</Field.Label>
                                <Input
                                    id="minArbitrage"
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={settings.minArbitrageDifferenceDollarsPerKWH}
                                    onChange={(e) => handleChange('minArbitrageDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                                />
                                <Field.Description>Required profit margin to trigger immediate charging to later use/export at a higher prices.</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="minDeficit">Charge for Deficit ($/kWh)</Field.Label>
                                <Input
                                    id="minDeficit"
                                    type="number"
                                    step="0.01"
                                    min="0"
                                    value={settings.minDeficitPriceDifferenceDollarsPerKWH}
                                    onChange={(e) => handleChange('minDeficitPriceDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                                />
                                <Field.Description>Price difference required to justify charging now to avoid a future battery depletion.</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="minStartChargeMinutes">Minimum Start Charge Duration (minutes)</Field.Label>
                                <Input
                                    id="minStartChargeMinutes"
                                    type="number"
                                    step="1"
                                    min="1"
                                    value={settings.minStartChargeMinutes}
                                    onChange={(e) => handleChange('minStartChargeMinutes', parseInt(e.target.value, 10))}
                                />
                                <Field.Description>Minimum duration in minutes of charging time needed to start charging.</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="peakSurvivalBufferMinutes">Peak Survival Buffer (minutes)</Field.Label>
                                <Input
                                    id="peakSurvivalBufferMinutes"
                                    type="number"
                                    step="1"
                                    min="0"
                                    value={settings.peakSurvivalBufferMinutes}
                                    onChange={(e) => handleChange('peakSurvivalBufferMinutes', parseInt(e.target.value, 10))}
                                />
                                <Field.Description>Buffer in minutes to attempt to have the battery outlast a peak price period.</Field.Description>
                            </Field.Root>

                            {settings.release === 'staging' && (
                                <>
                                    <Field.Root className="form-group switch-group compact">
                                        <div className="switch-row">
                                            <Switch.Root
                                                id="acUsagePrediction"
                                                checked={settings.acUsageIncreasePercentPerDegree > 0}
                                                onCheckedChange={(checked) => {
                                                    if (checked) {
                                                        handleChange('acUsageIncreasePercentPerDegree', 9);
                                                        handleChange('acUsageMaxIncreasePercent', 50);
                                                    } else {
                                                        handleChange('acUsageIncreasePercentPerDegree', -1);
                                                        handleChange('acUsageMaxIncreasePercent', -1);
                                                    }
                                                }}
                                                className="switch-root"
                                            >
                                                <Switch.Thumb className="switch-thumb" />
                                            </Switch.Root>
                                            <Field.Label htmlFor="acUsagePrediction">Enable A/C Weather Prediction</Field.Label>
                                        </div>
                                        <Field.Description>Automatically predict increased load during hotter weather to ensure adequate battery reserves.</Field.Description>
                                    </Field.Root>

                                    {settings.acUsageIncreasePercentPerDegree > 0 && (
                                        <Field.Root className="form-group compact">
                                            <Field.Label htmlFor="acBaseTemperatureF">A/C Base Temperature (°F)</Field.Label>
                                            <Input
                                                id="acBaseTemperatureF"
                                                type="number"
                                                step="1"
                                                value={Math.round((settings.acBaseTemperatureC || 24) * 9 / 5 + 32)}
                                                onChange={(e) => {
                                                    const f = parseFloat(e.target.value);
                                                    if (!isNaN(f)) {
                                                        const c = (f - 32) * 5 / 9;
                                                        handleChange('acBaseTemperatureC', c);
                                                    }
                                                }}
                                            />
                                            <Field.Description>Temperature above which A/C is typically used to cool the house.</Field.Description>
                                        </Field.Root>
                                    )}
                                </>
                            )}

                            <Field.Root className="form-group switch-group compact">
                                <div className="switch-row">
                                    <Switch.Root
                                        id="dryRun"
                                        checked={settings.dryRun}
                                        onCheckedChange={(checked) => handleChange('dryRun', checked)}
                                        className="switch-root"
                                    >
                                        <Switch.Thumb className="switch-thumb" />
                                    </Switch.Root>
                                    <Field.Label htmlFor="dryRun">Dry Run Mode</Field.Label>
                                </div>
                                <Field.Description>Simulate actions without executing them (useful for testing).</Field.Description>
                            </Field.Root>
                        </div>

                        <div className="section-header">
                            <h3>Advanced Solar Settings</h3>
                        </div>
                        <div className="grid-strategy-grid">
                            <Field.Root className="form-group">
                                <Field.Label htmlFor="solarTrendRatioMax">Solar Trend Ratio Max</Field.Label>
                                <Input
                                    id="solarTrendRatioMax"
                                    type="number"
                                    step="0.1"
                                    min="1"
                                    value={settings.solarTrendRatioMax}
                                    onChange={(e) => handleChange('solarTrendRatioMax', parseFloat(e.target.value))}
                                />
                                <Field.Description>Maximum ratio for solar trend adjustment. Higher values allow more aggressive upward solar predictions.</Field.Description>
                            </Field.Root>
                            <Field.Root className="form-group">
                                <Field.Label htmlFor="solarBellCurveMultiplier">Solar Bell Curve Multiplier</Field.Label>
                                <Input
                                    id="solarBellCurveMultiplier"
                                    type="number"
                                    step="0.1"
                                    min="0"
                                    max="1"
                                    value={settings.solarBellCurveMultiplier}
                                    onChange={(e) => handleChange('solarBellCurveMultiplier', parseFloat(e.target.value))}
                                />
                                <Field.Description>Multiplier for bell curve solar smoothing. 0 disables smoothing entirely</Field.Description>
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="solarFullyChargeHeadroomBatterySOC">Solar Fully Charge Headroom (%)</Field.Label>
                                <Input
                                    id="solarFullyChargeHeadroomBatterySOC"
                                    type="number"
                                    step="1"
                                    value={settings.solarFullyChargeHeadroomBatterySOC}
                                    onChange={(e) => handleChange('solarFullyChargeHeadroomBatterySOC', parseFloat(e.target.value))}
                                />
                                <Field.Description>
                                    Battery percentage to leave as headroom during solar charging when export is disabled. Negative values will remove the headroom and ignore solar curtailment.
                                </Field.Description>
                            </Field.Root>

                            {settings.utilityRateOptions?.netMeteringCredits && (
                                <Field.Root className="form-group">
                                    <Field.Label htmlFor="solarNetMeteringCreditsValue">Solar Net Metering Credits Value</Field.Label>
                                    <Select.Root
                                        value={settings.solarNetMeteringCreditsValue || ""}
                                        onValueChange={(value) => handleChange('solarNetMeteringCreditsValue', value || '')}
                                    >
                                        <Select.Trigger className="select-trigger" id="solarNetMeteringCreditsValue">
                                            <Select.Value>
                                                {
                                                    settings.solarNetMeteringCreditsValue === 'highest' ? 'Highest Price' :
                                                    settings.solarNetMeteringCreditsValue === 'none' ? 'None' :
                                                    settings.solarNetMeteringCreditsValue === 'lowest' ? 'Lowest Price' :
                                                    'Lowest / Default'
                                                }
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
                                                    <Select.Item className="select-item" value="">
                                                        <Select.ItemText>Lowest / Default</Select.ItemText>
                                                    </Select.Item>
                                                    <Select.Item className="select-item" value="lowest">
                                                        <Select.ItemText>Lowest Price</Select.ItemText>
                                                    </Select.Item>
                                                    <Select.Item className="select-item" value="highest">
                                                        <Select.ItemText>Highest Price</Select.ItemText>
                                                    </Select.Item>
                                                    <Select.Item className="select-item" value="none">
                                                        <Select.ItemText>None</Select.ItemText>
                                                    </Select.Item>
                                                </Select.Popup>
                                            </Select.Positioner>
                                        </Select.Portal>
                                    </Select.Root>
                                    <Field.Description>
                                        How to value exported solar credits. "Lowest" price of the day, "Highest" price of the day, or value them as nothing.
                                    </Field.Description>
                                </Field.Root>
                            )}
                        </div>

                        <div className="section-header">
                            <h3>Advanced Power History Settings</h3>
                        </div>
                        <div className="grid-strategy-grid">
                            <Field.Root className="form-group">
                                <Field.Label htmlFor="ignoreHourUsageOverMultiple">Ignore Usage Outlier Multiple</Field.Label>
                                <Input
                                    id="ignoreHourUsageOverMultiple"
                                    type="number"
                                    step="0.1"
                                    min="1"
                                    value={settings.ignoreHourUsageOverMultiple}
                                    onChange={(e) => handleChange('ignoreHourUsageOverMultiple', parseFloat(e.target.value))}
                                />
                                <Field.Description>If a single hour's usage is this many times greater than the average of other data points for that hour, ignore it. Must be &ge; 1.</Field.Description>
                            </Field.Root>
                        </div>
                    </>
                )}

                <div className="submit-section">
                    {error && <div className="error-message">{error}</div>}
                    {successMessage && <div className="success-message">{successMessage}</div>}
                    {!(editESS && maxStage > currentStage) && (
                        <button type="submit" className="save-button" disabled={isSaving}>
                            {isSaving && <span className="loading-spinner" aria-hidden="true"></span>}
                            {isSaving ? 'Saving...' : 'Save Settings'}
                        </button>
                    )}
                </div>
            </form>
        </div>
    );
};
export default Settings;
