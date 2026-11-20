import { extractError, fetchSettings, fetchAuthStatus, login, logout } from './apiCore';
import {
    ActionReason,
    AppType,
    BatteryMode,
    HIGH_HOME_LOAD_MIN_KW_HIGH,
    HIGH_HOME_LOAD_MIN_KW_LOW,
    HIGH_HOME_LOAD_MIN_KW_MEDIUM,
    SolarMode,
    type Action,
    type ActionsAndSavingsResponse,
    type AdminFeatureFlag,
    type AdminSite,
    type AlertSensitivity,
    type AnomalyAlertSensitivity,
    type AuthStatus,
    type CreateAppResponse,
    type CredentialsPayload,
    type ESSAuthStatus,
    type ESSCredentialField,
    type ESSCredentialFieldChoice,
    type ESSCredentialFieldType,
    type ESSProviderInfo,
    type EnergyHistoryRes,
    type EnergyStats,
    type EveningSummaryFlavor,
    type FeatureCriteria,
    type Feedback,
    type ForecastResponse,
    type HistoryEnergyResponse,
    type HourlySavingsStatsDebugging,
    type InterestSubmission,
    type MinBatterySOCPeriod,
    type MorningSummaryFlavor,
    type NotificationSubscriptionsResponse,
    type OAuthClientInfo,
    type Plan,
    type PlanPeriod,
    type PriceHistoryRes,
    type PriceInfo,
    type PushSubscription,
    type PushSubscriptionKeys,
    type SavingsStats,
    type Settings,
    type SettingsUpdateRequest,
    type ShortPlanPeriod,
    type SimulationParams,
    type Site,
    type SiteLocation,
    type SitePermissions,
    type SystemAlarm,
    type SystemStatus,
    type SystemStorm,
    type TimePeriod,
    type UserApp,
    type UserNotificationSettings,
    type UserSite,
    type UtilityFeesPeriod,
    type UtilityHourPeriod,
    type UtilityOptionChoice,
    type UtilityOptionType,
    type UtilityProviderInfo,
    type UtilityRateInfo,
    type UtilityRateOption,
    type UtilityRateOptions,
    type VPPEvent,
    type WeatherRes,
} from './apiTypes.gen';

export { fetchSettings, fetchAuthStatus, login, logout };

export {
    ActionReason,
    AppType,
    BatteryMode,
    HIGH_HOME_LOAD_MIN_KW_HIGH,
    HIGH_HOME_LOAD_MIN_KW_LOW,
    HIGH_HOME_LOAD_MIN_KW_MEDIUM,
    SolarMode,
    type Action,
    type ActionsAndSavingsResponse,
    type AdminFeatureFlag,
    type AdminSite,
    type AlertSensitivity,
    type AnomalyAlertSensitivity,
    type AuthStatus,
    type CreateAppResponse,
    type CredentialsPayload,
    type ESSAuthStatus,
    type ESSCredentialField,
    type ESSCredentialFieldChoice,
    type ESSCredentialFieldType,
    type ESSProviderInfo,
    type EnergyHistoryRes,
    type EnergyStats,
    type EveningSummaryFlavor,
    type FeatureCriteria,
    type Feedback,
    type ForecastResponse,
    type HistoryEnergyResponse,
    type HourlySavingsStatsDebugging,
    type InterestSubmission,
    type MinBatterySOCPeriod,
    type MorningSummaryFlavor,
    type NotificationSubscriptionsResponse,
    type OAuthClientInfo,
    type Plan,
    type PlanPeriod,
    type PriceHistoryRes,
    type PriceInfo,
    type PushSubscription,
    type PushSubscriptionKeys,
    type SavingsStats,
    type Settings,
    type ShortPlanPeriod,
    type SimulationParams,
    type Site,
    type SiteLocation,
    type SitePermissions,
    type SystemAlarm,
    type SystemStatus,
    type SystemStorm,
    type TimePeriod,
    type UserApp,
    type UserNotificationSettings,
    type UserSite,
    type UtilityFeesPeriod,
    type UtilityHourPeriod,
    type UtilityOptionChoice,
    type UtilityOptionType,
    type UtilityProviderInfo,
    type UtilityRateInfo,
    type UtilityRateOption,
    type UtilityRateOptions,
    type VPPEvent,
    type WeatherRes,
};

export const fetchUtilityPeriods = async (
    siteID?: string,
    utilityProvider?: string,
    utilityRate?: string,
    utilityRateOptions?: UtilityRateOptions
): Promise<TimePeriod[] | null> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    if (utilityProvider) {
        query.append('utilityProvider', utilityProvider);
    }
    if (utilityRate) {
        query.append('utilityRate', utilityRate);
    }
    if (utilityRateOptions && Object.keys(utilityRateOptions).length > 0) {
        query.append('utilityRateOptions', JSON.stringify(utilityRateOptions));
    }
    const response = await fetch(`/api/utility/periods${query.toString() ? `?${query.toString()}` : ''}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch utility periods'));
    }
    return response.json();
};

export const updateSettings = async (settings: Settings, siteID?: string, credentials?: CredentialsPayload): Promise<void> => {
    const { hasCredentials, features, ...baseSettings } = settings;
    void hasCredentials;
    void features;

    const payload: SettingsUpdateRequest = {
        ...baseSettings,
        ...(siteID ? { siteID } : {}),
        ...(credentials ? { credentials } : {}),
    };

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

export const acceptTerms = async (): Promise<void> => {
    const response = await fetch('/api/auth/acceptTerms', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to accept terms'));
    }
};

export const deleteSite = async (siteID: string): Promise<void> => {
    const response = await fetch('/api/delete/site', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to delete site'));
    }
};

export const deleteUser = async (): Promise<void> => {
    const response = await fetch('/api/delete/user', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to delete user'));
    }
};

export const listFeatures = async (): Promise<AdminFeatureFlag[]> => {
    const response = await fetch('/api/list/features', {
        method: 'GET',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to list feature flags'));
    }
    return response.json();
};

export const updateFeatures = async (features: Record<string, FeatureCriteria[]>): Promise<void> => {
    const response = await fetch('/api/features', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ features }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to update feature flags'));
    }
};

export const listSites = async (): Promise<AdminSite[]> => {
    const response = await fetch('/api/list/sites', {
        method: 'GET',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to list sites'));
    }
    return response.json();
};

export const setSiteAlias = async (siteID: string, alias: string): Promise<void> => {
    const response = await fetch('/api/site/alias', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID, alias }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to update site alias'));
    }
};

export const listUserSites = async (): Promise<UserSite[]> => {
    const response = await fetch('/api/list/userSites', {
        method: 'GET',
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to list user sites'));
    }
    return response.json();
};

export const fetchModeling = async (siteID?: string, overrideHomeLoadPredictionStrategy?: string, regeneratePlan?: boolean): Promise<ForecastResponse> => {
    const query = new URLSearchParams();
    if (siteID) {
        query.append('siteID', siteID);
    }
    if (overrideHomeLoadPredictionStrategy) {
        query.append('overrideHomeLoadPredictionStrategy', overrideHomeLoadPredictionStrategy);
    }
    if (regeneratePlan) {
        query.append('regeneratePlan', 'true');
    }
    const response = await fetch(`/api/forecast?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch modeling data'));
    }
    return response.json();
};

export const joinSite = async (joinSiteID: string, inviteCode: string, name: string): Promise<string> => {
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
    const data = await response.json();
    return data.siteID;
};

export const createSite = async (name: string): Promise<string> => {
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
    const data = await response.json();
    return data.siteID;
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

export async function submitFeedback(siteID: string, sentiment: string, comment: string, extra: Record<string, string>): Promise<void> {
    const response = await fetch('/api/feedback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ siteID, sentiment, comment, extra })
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to submit feedback'));
    }
}

export async function listFeedback(limit?: number, lastFeedbackID?: string): Promise<Feedback[]> {
    let url = '/api/list/feedback?';
    if (limit !== undefined) {
        url += `limit=${limit}&`;
    }
    if (lastFeedbackID) {
        url += `lastFeedbackID=${lastFeedbackID}`;
    }
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch feedback'));
    }
    return response.json();
}

export async function submitInterest(submission: Omit<InterestSubmission, 'email' | 'timestamp'>): Promise<void> {
    const response = await fetch('/api/interest', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(submission)
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to submit interest'));
    }
}

export async function listInterest(limit?: number): Promise<InterestSubmission[]> {
    let url = '/api/list/interest?';
    if (limit !== undefined) {
        url += `limit=${limit}`;
    }
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch interest submissions'));
    }
    return response.json();
}

export const fetchHistoryEnergy = async (date: Date, siteID?: string): Promise<HistoryEnergyResponse> => {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const dateStr = `${year}-${month}-${day}`;

    const query = new URLSearchParams({
        date: dateStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    const response = await fetch(`/api/history/energy?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch energy history'));
    }
    return response.json();
};

export const submitESSStage = async (
    ess: string,
    credentials: Record<string, string | number | boolean | undefined>,
    siteID?: string
): Promise<void> => {
    const payload: {
        ess: string;
        credentials: CredentialsPayload;
        siteID?: string;
    } = {
        ess,
        credentials: {
            [ess]: credentials,
        },
        ...(siteID ? { siteID } : {}),
    };
    const response = await fetch('/api/ess/stage', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to trigger next stage'));
    }
};

interface SavingsPeriod {
    start: Date;
    end: Date;
}

export const fetchActionsAndSavings = async (
    start: Date,
    end: Date,
    siteID?: string,
    options?: {
        savingsPeriods?: SavingsPeriod[];
    }
): Promise<ActionsAndSavingsResponse> => {
    const startStr = start.toISOString();
    const endStr = end.toISOString();
    const query = new URLSearchParams({
        start: startStr,
        end: endStr,
    });
    if (siteID) {
        query.append('siteID', siteID);
    }
    if (options?.savingsPeriods && options.savingsPeriods.length > 0) {
        const periodsParam = options.savingsPeriods
            .map(p => `${p.start.toISOString()},${p.end.toISOString()}`)
            .join(';');
        query.append('savingsPeriods', periodsParam);
    }
    const response = await fetch(`/api/history/actionsAndSavings?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch actions and savings'));
    }
    return response.json();
};

export const fetchNotificationSubscriptions = async (): Promise<NotificationSubscriptionsResponse> => {
    const response = await fetch('/api/notifications/subscriptions');
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch notification subscriptions'));
    }
    return response.json();
};

export const fetchVAPIDPublicKey = async (): Promise<ArrayBuffer> => {
    const response = await fetch('/api/notifications/vapidPublicKey');
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch VAPID public key'));
    }
    return await response.arrayBuffer();
};

export const subscribePushNotification = async (
    subscription: Omit<PushSubscription, 'id' | 'tsCreated'> & Partial<Pick<PushSubscription, 'id' | 'tsCreated'>>,
    sendTest: boolean = false
): Promise<void> => {
    const response = await fetch('/api/notifications/subscribe', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ subscription, sendTest }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to register push subscription'));
    }
};

export const unsubscribePushNotification = async (endpoint: string): Promise<void> => {
    const response = await fetch('/api/notifications/unsubscribe', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ endpoint }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to remove push subscription'));
    }
};

export const updateNotificationSettings = async (siteID: string, settings: UserNotificationSettings): Promise<void> => {
    const response = await fetch('/api/settings/notifications', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID, settings }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to update notification settings'));
    }
};

export const fetchApps = async (siteID: string): Promise<UserApp[]> => {
    const query = new URLSearchParams({ siteID });
    const response = await fetch(`/api/apps?${query.toString()}`);
    if (response.status === 501) {
        return [];
    }
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to fetch connected apps'));
    }
    const data: { apps?: UserApp[] } = await response.json();
    return data.apps || [];
};

export const createApp = async (siteID: string, name: string): Promise<CreateAppResponse> => {
    const response = await fetch('/api/apps/new', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID, name }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to create API key'));
    }
    return response.json();
};

export const revokeApp = async (siteID: string, id: string): Promise<void> => {
    const response = await fetch('/api/apps/revoke', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ siteID, id }),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to revoke app'));
    }
};

export const fetchOAuthClientInfo = async (params: {
    clientID: string;
    redirectURI: string;
    responseType: string;
    codeChallenge: string;
    codeChallengeMethod: string;
    resource?: string;
}): Promise<OAuthClientInfo> => {
    const query = new URLSearchParams({
        client_id: params.clientID,
        redirect_uri: params.redirectURI,
        response_type: params.responseType,
        code_challenge: params.codeChallenge,
        code_challenge_method: params.codeChallengeMethod,
    });
    if (params.resource) {
        query.set('resource', params.resource);
    }
    const response = await fetch(`/api/oauth/client?${query.toString()}`);
    if (!response.ok) {
        throw new Error(await extractError(response, 'Invalid OAuth authorization request'));
    }
    return response.json();
};

export const approveOAuthClient = async (payload: {
    siteID: string;
    clientID: string;
    redirectURI: string;
    responseType: string;
    codeChallenge: string;
    codeChallengeMethod: string;
    state?: string;
    resource?: string;
}): Promise<{ redirectURL: string }> => {
    const response = await fetch('/api/oauth/approve', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
    if (!response.ok) {
        throw new Error(await extractError(response, 'Failed to approve connected app'));
    }
    return response.json();
};
