import { useEffect, useState } from 'react';
import { fetchSettings, updateSettings, fetchUtilities, fetchESSList, type Settings as SettingsType, type UtilityProviderInfo, type UtilityRateOption, type ESSProviderInfo } from '../api';
import { Field } from '@base-ui/react/field';
import { Input } from '@base-ui/react/input';
import { Switch } from '@base-ui/react/switch';
import { Collapsible } from '@base-ui/react/collapsible';
import { Select } from '@base-ui/react/select';
import './Settings.css';


const Settings = ({ siteID }: { siteID?: string }) => {
    const [settings, setSettings] = useState<SettingsType | null>(null);
    const [isUtilityDirty, setIsUtilityDirty] = useState(false);
    const [isESSDirty, setIsESSDirty] = useState(false);
    const [utilities, setUtilities] = useState<UtilityProviderInfo[]>([]);
    const [essProviders, setEssProviders] = useState<ESSProviderInfo[]>([]);
    const [loading, setLoading] = useState(true);
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
            setIsUtilityDirty(false);
            setIsESSDirty(false);
            setUtilities(utilitiesData);
            setEssProviders(essProvidersData);
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
            setError(null);
            setSuccessMessage(null);

            let credentialsPayload: any = undefined;
            if (Object.keys(essCredentials).length > 0) {
                const provider = essProviders.find(p => p.id === settings.ess);
                if (provider) {
                    credentialsPayload = { [provider.id]: {} };
                    for (const cred of provider.credentials) {
                        const val = essCredentials[cred.field] || "";
                        if (cred.required && !val) {
                            throw new Error(`The ${cred.name} field is required.`);
                        }
                        if (val) {
                            credentialsPayload[provider.id][cred.field] = val;
                        }
                    }
                }
            }

            await updateSettings(settings, siteID, credentialsPayload);
            setSuccessMessage('Settings saved successfully');

            const updatedSettings = credentialsPayload && settings.ess ? {
                ...settings,
                hasCredentials: {
                    ...settings.hasCredentials,
                    [settings.ess]: true
                }
            } : settings;

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
        }
    };

    const handleChange = (field: keyof SettingsType, value: any) => {
        if (!settings) return;
        setSettings({ ...settings, [field]: value });
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
                    <div className="configured-summary" onClick={() => setEditUtility(true)}>
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
                    </div>
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
                    <div className="configured-summary" onClick={() => setEditESS(true)}>
                        <div className="summary-info">
                            <span className="summary-label">{essProviders.find(p => p.id === settings.ess)?.name || settings.ess || 'Unknown System'}</span>
                        </div>
                        <div className={`summary-status ${isESSDirty ? 'pending' : ''}`}>
                            {isESSDirty ? 'Pending Save' : 'Connected'}
                        </div>
                    </div>
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

                            return provider.credentials.map(cred => (
                                <Field.Root key={cred.field} className="form-group">
                                    <Field.Label>{cred.name}</Field.Label>
                                    <Input
                                        type={cred.type === 'password' ? 'password' : 'text'}
                                        value={essCredentials[cred.field] || ""}
                                        onChange={(e) => {
                                            setEssCredentials({ ...essCredentials, [cred.field]: e.target.value });
                                            setIsESSDirty(true);
                                        }}
                                        placeholder={`Enter ${cred.name}`}
                                    />
                                    {cred.description && <Field.Description>{cred.description}</Field.Description>}
                                </Field.Root>
                            ));
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
                    <button type="submit" className="save-button">
                        Save Settings
                    </button>
                </div>
            </form>
        </div>
    );
};
export default Settings;
