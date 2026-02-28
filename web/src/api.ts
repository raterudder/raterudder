export interface SystemAlarm {
    name: string;
    description: string;
    time: string;
    code: string;
}

export interface SystemStorm {
    description: string;
    tsStart: string;
    tsEnd: string;
}

export interface SystemStatus {
    alarms?: SystemAlarm[];
    storms?: SystemStorm[];
    // Add other fields from backend if useful, but alarms is what we need now
    [key: string]: any;
}

export const ActionReason = {
    AlwaysChargeBelowThreshold: 'alwaysChargeBelowThreshold',
    MissingBattery: 'missingBattery',
    DeficitCharge: 'deficitCharge',
    ArbitrageCharge: 'arbitrageCharge',
    DischargeBeforeCapacity: 'dischargeBeforeCapacity',
    DeficitSaveForPeak: 'deficitSaveForPeak',
    ArbitrageSave: 'dischargeAtPeak',
    SufficientBattery: 'sufficientBattery',
    EmergencyMode: 'emergencyMode',
    HasAlarms: 'hasAlarms',
    WaitingToCharge: 'waitingToCharge',
    ChargeSurvivePeak: 'chargeSurvivePeak',
    PreventSolarCurtailment: 'preventSolarCurtailment',
    // deprecated
    DeficitSave: 'deficitSave',
} as const;

export type ActionReason = typeof ActionReason[keyof typeof ActionReason];

export interface PriceInfo {
    tsStart: string;
    tsEnd: string;
    dollarsPerKWH: number;
    gridUseDollarsPerKWH: number; // delivery adder; true grid charge cost = dollarsPerKWH + gridUseDollarsPerKWH
}

export interface Action {
    timestamp: string;
    batteryMode: number;
    solarMode: number;
    targetBatteryMode?: number;
    targetSolarMode?: number;
    reason?: ActionReason;
    description: string;
    currentPrice?: PriceInfo;
    futurePrice?: PriceInfo;
    deficitAt?: string;
    capacityAt?: string;
    systemStatus?: SystemStatus;
    dryRun?: boolean;
    fault?: boolean;
    paused?: boolean;
}

export const BatteryMode = {
    NoChange: 0,
    Standby: 1,
    ChargeAny: 2,
    ChargeSolar: 3,
    Load: -1,
} as const;

export type BatteryMode = typeof BatteryMode[keyof typeof BatteryMode];

export const SolarMode = {
    NoChange: 0,
    NoExport: 1,
    Any: 2,
} as const;

export type SolarMode = typeof SolarMode[keyof typeof SolarMode];

async function extractError(response: Response, fallback: string): Promise<string> {
    if (response.status === 401 && !response.url.includes('/api/auth/status')) {
        try {
            const statusRes = await fetch('/api/auth/status');
            if (statusRes.ok) {
                const status = await statusRes.json();
                if (!status.loggedIn) {
                    window.location.href = '/login';
                }
            }
        } catch { /* ignore */ }
    }

    try {
        const body = await response.json();
        if (body && typeof body.error === 'string') {
            return body.error;
        }
    } catch { /* ignore parse failures */ }
    return fallback;
}

export const fetchActions = async (start: Date, end: Date, siteID?: string): Promise<Action[]|null> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const query = new URLSearchParams({
        start: startStr,
        end: endStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/history/actions?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch actions'));
    }
    return response.json();
};

export interface SavingsStats {
    timestamp: string;
    cost: number;
    credit: number;
    batterySavings: number;
    solarSavings: number;
    avoidedCost: number;
    chargingCost: number;
    solarGenerated: number;
    gridImported: number;
    gridExported: number;
    homeUsed: number;
    batteryUsed: number;
}

export const fetchSavings = async (start: Date, end: Date, siteID?: string): Promise<SavingsStats|null> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const query = new URLSearchParams({
        start: startStr,
        end: endStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/history/savings?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch savings'));
    }
    return response.json();
};

export interface UtilityRateOptions {
    [key: string]: any;
}

export interface UtilityOptionChoice {
    value: string;
    name: string;
}

export type UtilityOptionType = 'select' | 'switch';

export interface UtilityRateOption {
    field: string;
    name: string;
    type: UtilityOptionType;
    description?: string;
    choices?: UtilityOptionChoice[];
    default?: any;
}

export interface UtilityProviderInfo {
  id: string;
  name: string;
  rates: UtilityRateInfo[];
  hidden?: boolean;
}

export interface UtilityRateInfo {
  id: string;
  name: string;
  options: UtilityRateOption[];
}

export interface ESSCredential {
  field: string;
  name: string;
  type: string;
  required: boolean;
  description?: string;
}

export interface ESSProviderInfo {
  id: string;
  name: string;
  credentials: ESSCredential[];
  hidden?: boolean;
}

export interface Settings {
    dryRun: boolean;
    pause: boolean;
    release: string;
    alwaysChargeUnderDollarsPerKWH: number;
    minArbitrageDifferenceDollarsPerKWH: number;
    minDeficitPriceDifferenceDollarsPerKWH: number;
    minBatterySOC: number;
    ignoreHourUsageOverMultiple: number;
    gridChargeBatteries: boolean;
    gridExportSolar: boolean;
    gridExportBatteries: boolean;
    solarTrendRatioMax: number;
    solarBellCurveMultiplier: number;
    solarFullyChargeHeadroomBatterySOC: number;
    solarNetMeteringCreditsValue: string;
    utilityProvider: string;
    utilityRate: string;
    utilityRateOptions: UtilityRateOptions;
    ess: string;
    hasCredentials: {
        [key: string]: boolean;
    };
    essAuthStatus?: {
        consecutiveFailures: number;
        lastAttempt: string;
    };
}

export interface FranklinCredentials {
    username: string;
    md5Password: string;
    gatewayID: string;
}

export interface SettingsUpdate {
    settings: Settings;
    credentials?: Record<string, any>;
    siteID?: string;
}

export const fetchSettings = async (siteID?: string): Promise<Settings> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/settings?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch settings'));
    }
    return response.json();
};

export const updateSettings = async (settings: Settings, siteID?: string, credentials?: Record<string, any>): Promise<void> => {
    const payload: any = {
        ...settings,
        siteID: siteID,
    };

    if (siteID) {
        payload.siteID = siteID;
    }

    if (credentials) {
        payload.credentials = credentials;
    }

    const response = await fetch('/api/settings', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to update settings'));
    }
};

export interface Site {
    id: string;
}

export interface UserSite extends Site {
    name: string;
}

export interface AuthStatus {
    loggedIn: boolean;
    email: string;
    authRequired: boolean;
    clientIDs: Record<string, string>;
    sites?: UserSite[];
}

export const fetchAuthStatus = async (): Promise<AuthStatus> => {
    const response = await fetch('/api/auth/status');
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch auth status'));
    }
    return response.json();
};

export const login = async (token: string, client?: string): Promise<void> => {
    const response = await fetch('/api/auth/login', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ token, client }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Login failed'));
    }
};

export const logout = async (): Promise<void> => {
    const response = await fetch('/api/auth/logout', {
        method: 'POST',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Logout failed'));
    }
};

export interface AdminSite extends Site {
    lastAction?: Action;
}

export const listSites = async (): Promise<AdminSite[]> => {
    const response = await fetch('/api/list/sites', {
        method: 'GET',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to list sites'));
    }
    return response.json();
};

export interface ModelingHour {
    ts: string;
    hour: number;
    netLoadSolarKWH: number;
    gridChargeDollarsPerKWH: number;
    solarOppDollarsPerKWH: number;
    avgHomeLoadKWH: number;
    predictedSolarKWH: number;
    batteryKWH: number;
    batteryKWHIfStandby: number;
    batteryCapacityKWH: number;
    batteryReserveKWH: number;
    todaySolarTrend: number;
}

export const fetchModeling = async (siteID?: string): Promise<ModelingHour[]> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/forecast?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch modeling data'));
    }
    return response.json();
};

export const joinSite = async (joinSiteID: string, inviteCode: string, name: string): Promise<void> => {
    const response = await fetch('/api/join', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ joinSiteID, inviteCode, name }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to join site'));
    }
};

export const createSite = async (name: string): Promise<void> => {
    const response = await fetch('/api/join', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ create: true, name }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to create site'));
    }
};

export const fetchUtilities = async (siteID?: string): Promise<UtilityProviderInfo[]> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/list/utilities?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch utilities'));
    }
    return response.json();
};

export const fetchESSList = async (siteID?: string): Promise<ESSProviderInfo[]> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/list/ess?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch ESS systems'));
    }
    return response.json();
};
