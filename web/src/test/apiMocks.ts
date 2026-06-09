import type { ESSProviderInfo, UtilityProviderInfo } from "../api";

export const defaultAuthStatus = {
    loggedIn: true,
    authRequired: true,
    clientIDs: { google: 'test-client-id' },
    email: 'user@example.com',
    sites: [{ id: 'site1', name: 'Site 1' }]
};

export const defaultSavings = {
    batterySavings: 0,
    solarSavings: 0,
    cost: 0,
    credit: 0,
    avoidedCost: 0,
    chargingCost: 0,
    solarGenerated: 0,
    gridImported: 0,
    gridExported: 0,
    homeUsed: 0,
    batteryUsed: 0
};

export const defaultSettings = {
    dryRun: false,
    pause: false,
    release: 'production',
    minBatterySOC: 10,
    gridExportSolar: false,
    gridExportBatteries: false,
    gridChargeBatteries: true,
    solarTrendRatioMax: 3.0,
    solarBellCurveMultiplier: 1.0,
    ignoreHourUsageOverMultiple: 2,
    alwaysChargeUnderDollarsPerKWH: 0.05,
    minArbitrageDifferenceDollarsPerKWH: 0.03,
    minDeficitPriceDifferenceDollarsPerKWH: 0.02,
    utilityProvider: '',
    utilityRate: '',
    utilityRateOptions: {},
    ess: '',
    hasCredentials: {
        franklin: false
    },
    minStartChargeMinutes: 5,
    peakSurvivalBufferMinutes: 30
};

export const defaultUtilities = [
    {
        id: 'comed',
        name: 'Commonwealth Edison (ComEd)',
        rates: [
            {
                id: 'comed_besh',
                name: 'Hourly Pricing Program (BESH)',
                options: [
                    {
                        field: 'rateClass',
                        name: 'Rate Class',
                        type: 'select',
                        choices: [
                            { value: 'singleFamilyWithoutElectricHeat', name: 'Residential Single Family Without Electric Space Heat' },
                            { value: 'multiFamilyWithoutElectricHeat', name: 'Residential Multi Family Without Electric Space Heat' },
                            { value: 'singleFamilyElectricHeat', name: 'Residential Single Family With Electric Space Heat' },
                            { value: 'multiFamilyElectricHeat', name: 'Residential Multi Family With Electric Space Heat' },
                        ],
                        default: 'singleFamilyWithoutElectricHeat',
                    },
                    {
                        field: 'variableDeliveryRate',
                        name: 'Delivery Time-of-Day (DTOD)',
                        type: 'switch',
                        description: "Enable if you are enrolled in ComEd's Delivery Time-of-Day pricing. 30%-47% cheaper than fixed delivery rates in off-peak hours but 2x more expensive in on-peak hours (1pm-7pm).",
                        default: false,
                    },
                ],
            },
        ],
    },
    {
        id: 'hidden_utility',
        name: 'Secret Utility',
        hidden: true,
        rates: [
            {
                id: 'hidden_rate',
                name: 'Hidden Rate',
                options: [],
            },
        ],
    },
] as UtilityProviderInfo[];

export const defaultESSProviders = [
    {
        id: 'franklin',
        name: 'FranklinWH',
        credentials: [
            { field: 'username', name: 'Email', type: 'string', required: true },
            { field: 'password', name: 'Password', type: 'password', required: true },
            { field: 'gatewayID', name: 'Target Gateway ID (Optional)', type: 'string', required: false },
        ]
    },
    {
        id: 'hidden_ess',
        name: 'Secret ESS',
        hidden: true,
        credentials: [],
    },
    {
        id: 'missing_ess',
        name: 'Missing ESS',
        credentials: [
            { field: 'username', name: 'Username', type: 'string', required: true },
        ],
    },
    {
        id: 'multi_region_ess',
        name: 'Multi Region ESS',
        oAuthURLs: {
            "NA": "https://na.example.com",
            "EU": "https://eu.example.com"
        },
        oAuthKey: {
            field: 'region',
            name: 'Region',
            type: 'select',
            required: true,
            choices: [
                { value: 'NA', name: 'North America' },
                { value: 'EU', name: 'Europe' },
            ],
            default: "NA"
        },
        credentials: [
            { field: 'authCode', name: 'Authorization Code', type: 'password', required: true, oneTime: true },
        ]
    },
    {
        id: 'tesla',
        name: 'Tesla',
        oAuthURLs: {
            "default": "https://auth.tesla.com"
        },
        credentials: [
            { field: 'authCode', name: 'Authorization Code', type: 'password', required: true, hidden: true, oneTime: true },
            { field: 'energySiteID', name: 'Energy Site ID (Optional)', type: 'string', required: false }
        ]
    }
] as ESSProviderInfo[];

export const setupDefaultApiMocks = (api: any) => {
    if (typeof api.fetchActions?.mockResolvedValue === 'function') api.fetchActions.mockResolvedValue([]);
    if (typeof api.fetchSavings?.mockResolvedValue === 'function') api.fetchSavings.mockResolvedValue(defaultSavings);
    if (typeof api.fetchActionsAndSavings?.mockResolvedValue === 'function') {
        api.fetchActionsAndSavings.mockResolvedValue({
            actions: [],
            savings: defaultSavings
        });
    }
    if (typeof api.fetchAuthStatus?.mockResolvedValue === 'function') api.fetchAuthStatus.mockResolvedValue(defaultAuthStatus);
    if (typeof api.fetchSettings?.mockResolvedValue === 'function') api.fetchSettings.mockResolvedValue(defaultSettings);
    if (typeof api.fetchUtilities?.mockResolvedValue === 'function') api.fetchUtilities.mockResolvedValue(defaultUtilities);
    if (typeof api.fetchESSList?.mockResolvedValue === 'function') api.fetchESSList.mockResolvedValue(defaultESSProviders);
    if (typeof api.updateSettings?.mockResolvedValue === 'function') api.updateSettings.mockResolvedValue(undefined);
    if (typeof api.submitESSStage?.mockResolvedValue === 'function') api.submitESSStage.mockResolvedValue(undefined);
    if (typeof api.login?.mockResolvedValue === 'function') api.login.mockResolvedValue(undefined);
    if (typeof api.logout?.mockResolvedValue === 'function') api.logout.mockResolvedValue(undefined);
    if (typeof api.fetchModeling?.mockResolvedValue === 'function') api.fetchModeling.mockResolvedValue([]);
};
