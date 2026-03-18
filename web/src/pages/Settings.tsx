import { useEffect, useState } from 'react';
import { fetchSettings, updateSettings, fetchUtilities, fetchESSList, type Settings as SettingsType, type UtilityProviderInfo, type UtilityRateOption, type ESSProviderInfo, type ESSCredentialField } from '../api';
import { Field } from '@base-ui/react/field';
import { Input } from '@base-ui/react/input';
import { Button } from '@base-ui/react/button';
import { Switch } from '@base-ui/react/switch';
import { Collapsible } from '@base-ui/react/collapsible';
import { Select } from '@base-ui/react/select';
import { Combobox } from '@base-ui/react/combobox';
import './Settings.css';


const Settings = ({ siteID }: { siteID?: string }) => {
    const [settings, setSettings] = useState<SettingsType | null>(null);
    const [isUtilityDirty, setIsUtilityDirty] = useState(false);
    const [isESSDirty, setIsESSDirty] = useState(false);
    const [utilities, setUtilities] = useState<UtilityProviderInfo[]>([]);
    const [essProviders, setEssProviders] = useState<ESSProviderInfo[]>([]);
    const [loading, setLoading] = useState(true);
    const [isSaving, setIsSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [successMessage, setSuccessMessage] = useState<string | null>(null);

    const [essCredentials, setEssCredentials] = useState<Record<string, string>>({});

    // UI State for consolidated views
    const [editUtility, setEditUtility] = useState(false);
    const [editESS, setEditESS] = useState(false);

    useEffect(() => {
        loadData();
    }, [siteID]);

    const loadData = async () => {
        try {
            setLoading(true);
            const [settingsData, utilitiesData, essProvidersData] = await Promise.all([
                fetchSettings(siteID),
                fetchUtilities(siteID),
                fetchESSList(siteID)
            ]);
            setSettings(settingsData);
            setUtilities(utilitiesData);
            setEssProviders(essProvidersData);

            // if the ESS was configured but the credentials are not set then put
            // the ESS section into edit mode
            // we are explicitly checking for false in case the ess doesn't support
            // credentials we don't set edit every time when its undefined
            if (settingsData.ess && settingsData.hasCredentials?.[settingsData.ess] === false) {
                setEditESS(true);
                setIsESSDirty(true);
            }

            setError(null);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load settings');
        } finally {
            setLoading(false);
        }
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!settings) return;

        try {
            setIsSaving(true);
            setError(null);
            setSuccessMessage(null);

            // Merge default values for utility rate options
            const utilityProvider = utilities.find(u => u.id === settings.utilityProvider);
            const utilityRate = utilityProvider?.rates.find(r => r.id === settings.utilityRate);
            const finalSettings = { ...settings };
            if (utilityRate) {
                const finalOpts = { ...finalSettings.utilityRateOptions };
                utilityRate.options.forEach(opt => {
                    if (finalOpts[opt.field] === undefined || finalOpts[opt.field] === null || finalOpts[opt.field] === "") {
                        if (opt.default !== undefined) {
                            finalOpts[opt.field] = opt.default;
                        }
                    }
                });
                finalSettings.utilityRateOptions = finalOpts;
            }

            let credentialsPayload: any = undefined;
            const essProvider = essProviders.find(p => p.id === settings.ess);
            if (essProvider && (isESSDirty || Object.keys(essCredentials).length > 0)) {
                credentialsPayload = { [essProvider.id]: {} };
                const processCred = (cred: ESSCredentialField) => {
                    let val = essCredentials[cred.field];
                    if (val === undefined || val === null || val === "") {
                        val = cred.default;
                    }

                    if (cred.required && (val === undefined || val === null || val === "")) {
                        throw new Error(`The ${cred.name} field is required.`);
                    }
                    if (val !== undefined && val !== null && val !== "") {
                        credentialsPayload[essProvider.id][cred.field] = val;
                    }
                };

                for (const cred of essProvider.credentials) {
                    processCred(cred);
                }
                if (essProvider.oAuthKey) {
                    processCred(essProvider.oAuthKey);
                }
            }

            finalSettings.release = "staging";

            await updateSettings(finalSettings, siteID, credentialsPayload);
            setSuccessMessage('Settings saved successfully');

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
            }
            setIsUtilityDirty(false);
            setIsESSDirty(false);

            setTimeout(() => setSuccessMessage(null), 3000);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to save settings');
        } finally {
            setIsSaving(false);
        }
    };

    const handleChange = (field: keyof SettingsType, value: any) => {
        setSettings(prev => prev ? ({ ...prev, [field]: value }) : null);
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

        const listener = (event: MessageEvent) => {
            if (event.origin !== window.location.origin) return;

            if (event.data && event.data.type === 'OAUTH_CODE') {
                if (siteID && event.data.state !== siteID && event.data.state) {
                    setError('Authentication state mismatch. Please try again.');
                    window.removeEventListener('message', listener);
                    return;
                }

                setEssCredentials(prev => ({
                    ...prev,
                    [fieldName]: event.data.code
                }));
                setIsESSDirty(true);
                window.removeEventListener('message', listener);
            }
        };

        window.addEventListener('message', listener);
    };

    if (loading) return <div>Loading settings...</div>;
    if (!settings) return <div>Error loading settings</div>;

    return (
        <div className="content-container settings-container">
            <h2>Settings</h2>
            <form onSubmit={handleSubmit}>
                {/* Pause Updates at the top */}
                <div className="section-header pause-section">
                    <h3>Automation Status</h3>
                </div>
                <Field.Root className="form-group switch-group">
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

                {/* Utility Service Section */}
                <div className="section-header">
                    <h3>Utility Service</h3>
                    {settings.utilityProvider && settings.utilityRate && !editUtility && (
                        <button type="button" className="text-button" onClick={() => setEditUtility(true)}>Change</button>
                    )}
                </div>

                {settings.utilityProvider && settings.utilityRate && !editUtility ? (
                    <button type="button" className="configured-summary" onClick={() => setEditUtility(true)}>
                        <div className="summary-info">
                            <span className="summary-label">
                                {utilities.find(u => u.id === settings.utilityProvider)?.name || settings.utilityProvider}
                            </span>
                            <span className="summary-sublabel">
                                {utilities.find(u => u.id === settings.utilityProvider)?.rates.find(r => r.id === settings.utilityRate)?.name || settings.utilityRate}
                            </span>
                        </div>
                        <div className={`summary-status ${isUtilityDirty ? 'pending' : ''}`}>
                            {isUtilityDirty ? 'Pending Save' : 'Configured'}
                        </div>
                    </button>
                ) : (
                    <div className={editUtility ? "edit-section" : ""}>
                        <Field.Root className="form-group">
                            <Field.Label>Service</Field.Label>
                            <Select.Root
                                value={settings.utilityProvider}
                                onValueChange={(value) => {
                                    setEditUtility(true);
                                    setIsUtilityDirty(true);
                                    const providerID = value as string;
                                    const provider = utilities.find(u => u.id === providerID);
                                    const newSettings = {
                                        ...settings,
                                        utilityProvider: providerID,
                                        utilityRate: "",
                                        utilityRateOptions: {}
                                    };

                                    // If provider has only one rate, auto-select it
                                    if (provider?.rates.length === 1) {
                                        const rate = provider.rates[0];
                                        newSettings.utilityRate = rate.id;
                                        const newOpts: any = {};
                                        rate.options.forEach((opt: UtilityRateOption) => {
                                            newOpts[opt.field] = opt.default;
                                        });
                                        newSettings.utilityRateOptions = newOpts;
                                    }

                                    setSettings(newSettings);
                                }}
                            >
                                <Select.Trigger className="select-trigger" id="utilityService" aria-label="Service">
                                    <Select.Value>
                                        {utilities.find(u => u.id === settings.utilityProvider)?.name || 'Select a service...'}
                                    </Select.Value>
                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
                                            <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                                        </svg>
                                    </Select.Icon>
                                </Select.Trigger>
                                <Select.Portal>
                                    <Select.Positioner className="select-positioner">
                                        <Select.Popup className="select-popup">
                                            <Select.Item className="select-item" value="">
                                                <Select.ItemText>Select a service...</Select.ItemText>
                                            </Select.Item>
                                            {utilities.filter(u => !u.hidden || u.id === settings.utilityProvider).map(u => (
                                                <Select.Item key={u.id} className="select-item" value={u.id}>
                                                    <Select.ItemText>{u.name}</Select.ItemText>
                                                </Select.Item>
                                            ))}
                                        </Select.Popup>
                                    </Select.Positioner>
                                </Select.Portal>
                            </Select.Root>
                        </Field.Root>

                        {(() => {
                            const provider = utilities.find(u => u.id === settings.utilityProvider);
                            if (!provider) return null;

                            return (
                                <Field.Root className="form-group">
                                    <Field.Label>Rate/Plan</Field.Label>
                                    <Select.Root
                                        value={settings.utilityRate}
                                        onValueChange={(value) => {
                                            setIsUtilityDirty(true);
                                            const rateID = value as string;
                                            const rate = provider.rates.find(r => r.id === rateID);
                                            const newSettings = { ...settings, utilityRate: rateID };
                                            if (rate) {
                                                const newOpts: any = {};
                                                rate.options.forEach((opt: UtilityRateOption) => {
                                                    newOpts[opt.field] = opt.default;
                                                });
                                                newSettings.utilityRateOptions = newOpts;
                                            }
                                            setSettings(newSettings);
                                        }}
                                    >
                                        <Select.Trigger className="select-trigger" id="utilityRate" aria-label="Rate/Plan">
                                            <Select.Value>
                                                {provider.rates.find(r => r.id === settings.utilityRate)?.name || 'Select a rate/plan...'}
                                            </Select.Value>
                                            <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
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
                                                    {provider.rates.map(r => (
                                                        <Select.Item key={r.id} className="select-item" value={r.id}>
                                                            <Select.ItemText>{r.name}</Select.ItemText>
                                                        </Select.Item>
                                                    ))}
                                                </Select.Popup>
                                            </Select.Positioner>
                                        </Select.Portal>
                                    </Select.Root>
                                </Field.Root>
                            );
                        })()}

                        {(() => {
                            const provider = utilities.find(u => u.id === settings.utilityProvider);
                            const rate = provider?.rates.find(r => r.id === settings.utilityRate);
                            if (!rate || rate.options.length === 0) return null;

                            return (
                                <div className="sub-section">
                                    {rate.options.map((opt: UtilityRateOption) => (
                                        <Field.Root key={opt.field} className={`form-group ${opt.type === 'switch' ? 'switch-group' : ''}`}>
                                            {opt.type === 'select' && (
                                                <>
                                                    <Field.Label>{opt.name}</Field.Label>
                                                    <Select.Root
                                                        value={settings.utilityRateOptions?.[opt.field] || opt.default}
                                                            onValueChange={(value) => {
                                                                const newOpts = {
                                                                    ...settings.utilityRateOptions,
                                                                    [opt.field]: value
                                                                };
                                                                handleChange('utilityRateOptions', newOpts);
                                                                setIsUtilityDirty(true);
                                                            }}
                                                    >
                                                        <Select.Trigger className="select-trigger" id={`opt-${opt.field}`}>
                                                            <Select.Value>
                                                                {opt.choices?.find(c => c.value === (settings.utilityRateOptions?.[opt.field] || opt.default))?.name || (settings.utilityRateOptions?.[opt.field] || opt.default)}
                                                            </Select.Value>
                                                            <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
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
                                                                handleChange('utilityRateOptions', newOpts);
                                                                setIsUtilityDirty(true);
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
                        {editUtility && (
                            <button type="button" className="text-button cancel-button" onClick={() => setEditUtility(false)}>Done</button>
                        )}
                    </div>
                )}

                {/* ESS Configuration Section */}
                <div className="section-header">
                    <h3 id="ess-credentials">Energy Storage System</h3>
                    {settings.ess && settings.hasCredentials?.[settings.ess] && !editESS && (
                        <button type="button" className="text-button" onClick={() => setEditESS(true)}>Update</button>
                    )}
                </div>

                {settings.ess && (settings.hasCredentials?.[settings.ess] || isESSDirty) && !editESS ? (
                    <button type="button" className="configured-summary" onClick={() => setEditESS(true)}>
                        <div className="summary-info">
                            <span className="summary-label">{essProviders.find(p => p.id === settings.ess)?.name || settings.ess || 'Unknown System'}</span>
                        </div>
                        <div className={`summary-status ${isESSDirty ? 'pending' : ''}`}>
                            {isESSDirty ? 'Pending Save' : 'Connected'}
                        </div>
                    </button>
                ) : (
                    <div className={editESS ? "edit-section" : ""}>
                        <Field.Root className="form-group">
                            <Field.Label>System Type</Field.Label>
                            <Select.Root
                                value={settings.ess || ""}
                                onValueChange={(value) => {
                                    setEditESS(true);
                                    setIsESSDirty(true);
                                    setSettings({ ...settings, ess: value as string });
                                    setEssCredentials({}); // clear credentials when changing provider
                                }}
                            >
                                <Select.Trigger className="select-trigger" aria-label="ESS Type">
                                    <Select.Value>
                                        {essProviders.find(u => u.id === settings.ess)?.name || 'Select a system type...'}
                                    </Select.Value>
                                    <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
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

                        {(() => {
                            const provider = essProviders.find(p => p.id === settings.ess);
                            if (!provider) return null;

                            return (
                                <>
                                    {provider.oAuthURLs && Object.keys(provider.oAuthURLs).length > 0 && (
                                        <div style={{ marginBottom: '1rem' }}>
                                            {provider.oAuthKey && provider.oAuthKey.choices && (
                                                <Field.Root className="form-group" style={{ marginBottom: '1rem' }}>
                                                    <Field.Label>{provider.oAuthKey.name}</Field.Label>
                                                    <Select.Root
                                                        value={essCredentials[provider.oAuthKey.field] || provider.oAuthKey.default || ""}
                                                        onValueChange={(value) => {
                                                            setEssCredentials({ ...essCredentials, [provider.oAuthKey!.field]: value as string });
                                                            setIsESSDirty(true);
                                                        }}
                                                    >
                                                        <Select.Trigger className="select-trigger" aria-label={provider.oAuthKey.name}>
                                                            <Select.Value>
                                                                {provider.oAuthKey.choices?.find(c => c.value === (essCredentials[provider.oAuthKey!.field] || provider.oAuthKey!.default))?.name || (essCredentials[provider.oAuthKey!.field] || provider.oAuthKey!.default)}
                                                            </Select.Value>
                                                            <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
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
                                            <Button
                                                className="save-button"
                                                style={{ width: '100%' }}
                                                onClick={() => {
                                                    const keyVal = provider.oAuthKey ? (essCredentials[provider.oAuthKey.field] || provider.oAuthKey.default || Object.keys(provider.oAuthURLs!)[0]) : Object.keys(provider.oAuthURLs!)[0];
                                                    const url = provider.oAuthURLs![keyVal];
                                                    if (url) {
                                                        handleOAuthLogin(url, "authCode");
                                                    }
                                                }}
                                                type="button"
                                            >
                                                Login to link account
                                            </Button>
                                        </div>
                                    )}
                                    {provider.credentials.map(cred => (
                                        <Field.Root key={cred.field} className="form-group">
                                            <Field.Label>{cred.name}</Field.Label>
                                            {cred.type === 'select' ? (
                                                <Select.Root
                                                    value={essCredentials[cred.field] || cred.default || ""}
                                                    onValueChange={(value) => {
                                                        setEssCredentials({ ...essCredentials, [cred.field]: value as string });
                                                        setIsESSDirty(true);
                                                    }}
                                                >
                                                    <Select.Trigger className="select-trigger" aria-label={cred.name}>
                                                        <Select.Value>
                                                            {cred.choices?.find(c => c.value === (essCredentials[cred.field] || cred.default))?.name || (essCredentials[cred.field] || cred.default)}
                                                        </Select.Value>
                                                        <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                                                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
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
                                                    value={essCredentials[cred.field] || cred.default || ""}
                                                    type={cred.type === 'password' ? 'password' : 'text'}
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

                        {editESS && (
                            <button type="button" className="text-button cancel-button" onClick={() => setEditESS(false)}>Done</button>
                        )}
                    </div>
                )}

                <div className="section-header">
                    <h3>Automation Thresholds</h3>
                </div>
                <div className="form-grid compact-grid">
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

                    <Field.Root className="form-group compact">
                        <Field.Label htmlFor="minArbitrage">Min Arbitrage Profit ($/kWh)</Field.Label>
                        <Input
                            id="minArbitrage"
                            type="number"
                            step="0.01"
                            value={settings.minArbitrageDifferenceDollarsPerKWH}
                            onChange={(e) => handleChange('minArbitrageDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                        />
                        <Field.Description>Required profit margin to trigger immediate charging to later use/export at a higher prices.</Field.Description>
                    </Field.Root>

                    <Field.Root className="form-group compact">
                        <Field.Label htmlFor="minDeficit">Charge for Deficit ($/kWh)</Field.Label>
                        <Input
                            id="minDeficit"
                            type="number"
                            step="0.01"
                            value={settings.minDeficitPriceDifferenceDollarsPerKWH}
                            onChange={(e) => handleChange('minDeficitPriceDifferenceDollarsPerKWH', parseFloat(e.target.value))}
                        />
                        <Field.Description>Price difference required to justify charging now to avoid a future battery depletion.</Field.Description>
                    </Field.Root>
                </div>

                <div className="section-header">
                    <h3>Grid Restrictions</h3>
                </div>

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
                </div>

                {settings.release === 'staging' && (
                    <>
                        <div className="section-header">
                            <h3>Location</h3>
                        </div>
                        <div className="grid-strategy-grid">
                            <Field.Root className="form-group">
                                <Field.Label htmlFor="countryCode">Country</Field.Label>
                                <Combobox.Root
                                    value={settings.countryCode || ''}
                                    onValueChange={(val) => handleChange("countryCode", val as string)}
                                    items={[
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
                                    ]}
                                >
                                    <div className="combobox-input-wrapper select-trigger">
                                        <Combobox.Input placeholder="Select a country..." id="countryCode" className="combobox-input" />
                                        <Combobox.Trigger className="combobox-trigger">
                                            <svg width="15" height="15" viewBox="0 0 15 15" fill="none" xmlns="http://www.w3.org/2000/svg">
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
                                    onChange={(e) => handleChange("postalCode", e.target.value)}
                                />
                            </Field.Root>

                            <Field.Root className="form-group">
                                <Field.Label htmlFor="solarDirection">Roof Solar Panel Direction</Field.Label>
                                <Select.Root
                                    /* since an azimuth of 0 is south, we need to instead check the solarTilt */
                                    value={(settings.solarTilt && settings.solarTilt > 0) ? (settings.solarAzimuth?.toString() || "") : ""}
                                    onValueChange={(val) => {
                                        const azimuth = parseInt(val as string, 10);
                                        handleChange("solarAzimuth", azimuth);
                                        handleChange("solarTilt", 25);
                                    }}
                                >
                                        <Select.Trigger className="select-trigger" aria-label="Solar Direction">
                                            <Select.Value placeholder="Select direction...">
                                                {settings.solarTilt && settings.solarTilt > 0 ? (
                                                    ({ "180": "North", "270": "East", "0": "South", "90": "West" } as Record<string, string>)[settings.solarAzimuth?.toString() || ""]
                                                ) : null}
                                            </Select.Value>
                                            <Select.Icon className="select-icon">
                                                <svg width="15" height="15" viewBox="0 0 15 15" fill="none" xmlns="http://www.w3.org/2000/svg">
                                                    <path d="M4.18179 6.18181C4.35753 6.00608 4.64245 6.00608 4.81819 6.18181L7.49999 8.86362L10.1818 6.18181C10.3575 6.00608 10.6424 6.00608 10.8182 6.18181C10.9939 6.35755 10.9939 6.64247 10.8182 6.81821L7.81819 9.81821C7.73379 9.9026 7.61934 9.95001 7.49999 9.95001C7.38064 9.95001 7.26618 9.9026 7.18179 9.81821L4.18179 6.81821C4.00605 6.64247 4.00605 6.35755 4.18179 6.18181Z" fill="currentColor" fillRule="evenodd" clipRule="evenodd"></path>
                                                </svg>
                                            </Select.Icon>
                                        </Select.Trigger>
                                    <Select.Portal>
                                        <Select.Positioner className="select-positioner">
                                            <Select.Popup className="select-popup">
                                                <Select.List>
                                                    <Select.Item className="select-item" value="180">
                                                        <Select.ItemText>North</Select.ItemText>
                                                    </Select.Item>
                                                    <Select.Item className="select-item" value="270">
                                                        <Select.ItemText>East</Select.ItemText>
                                                    </Select.Item>
                                                    <Select.Item className="select-item" value="0">
                                                        <Select.ItemText>South</Select.ItemText>
                                                    </Select.Item>
                                                    <Select.Item className="select-item" value="90">
                                                        <Select.ItemText>West</Select.ItemText>
                                                    </Select.Item>
                                                </Select.List>
                                            </Select.Popup>
                                        </Select.Positioner>
                                    </Select.Portal>
                                </Select.Root>
                            </Field.Root>
                        </div>

                        <div className="weather-attribution">
                            Weather data provided by <a href="https://open-meteo.com" target="_blank" rel="noopener noreferrer">Open-Meteo</a> to improve solar prediction
                        </div>
                    </>
                )}



                <Collapsible.Root className="advanced-section">
                    <Collapsible.Trigger className="advanced-trigger">Advanced Tuning Settings</Collapsible.Trigger>
                    <Collapsible.Panel className="advanced-panel">
                        <Field.Root className="form-group switch-group" style={{ marginTop: '1rem' }}>
                            <div className="switch-row">
                                <Switch.Root
                                    checked={settings.dryRun}
                                    onCheckedChange={(checked) => handleChange('dryRun', checked)}
                                    className="switch-root"
                                >
                                    <Switch.Thumb className="switch-thumb" />
                                </Switch.Root>
                                <Field.Label>Dry Run Mode</Field.Label>
                            </div>
                            <Field.Description>Simulate actions without executing them (useful for testing).</Field.Description>
                        </Field.Root>



                        <div className="section-header">
                            <h3>Automation Overrides</h3>
                        </div>

                        <Field.Root className="form-group">
                            <Field.Label htmlFor="alwaysChargeUnder">Always Charge Below ($/kWh)</Field.Label>
                            <Input
                                id="alwaysChargeUnder"
                                type="number"
                                step="0.01"
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

                        <div className="section-header">
                            <h3>Solar Settings</h3>
                        </div>
                        <Field.Root className="form-group">
                            <Field.Label>Solar Trend Ratio Max</Field.Label>
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
                            <Field.Label>Solar Bell Curve Multiplier</Field.Label>
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
                            <Field.Label>Solar Fully Charge Headroom (%)</Field.Label>
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
                                <Field.Label>Solar Net Metering Credits Value</Field.Label>
                                <Select.Root
                                    value={settings.solarNetMeteringCreditsValue || ""}
                                    onValueChange={(value) => handleChange('solarNetMeteringCreditsValue', value)}
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
                                            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
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

                        <div className="section-header">
                            <h3>Power History Settings</h3>
                        </div>
                        <Field.Root className="form-group">
                            <Field.Label>Ignore Usage Outlier Multiple</Field.Label>
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
                    </Collapsible.Panel>
                </Collapsible.Root>

                <div className="submit-section">
                    {error && <div className="error-message">{error}</div>}
                    {successMessage && <div className="success-message">{successMessage}</div>}
                    <button type="submit" className="save-button" disabled={isSaving}>
                        {isSaving && (
                            <svg className="save-spinner" width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <circle className="spinner-track" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.25" />
                                <path className="spinner-head" d="M12 2C6.47715 2 2 6.47715 2 12C2 17.5228 6.47715 22 12 22" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
                            </svg>
                        )}
                        {isSaving ? 'Saving...' : 'Save Settings'}
                    </button>
                </div>
            </form>
        </div>
    );
};
export default Settings;
