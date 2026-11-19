import type { AuthStatus, Settings } from './api';

export async function extractError(response: Response, fallback: string): Promise<string> {
    if (response.status === 401 && !response.url.includes('/api/auth/status') && !response.url.includes('/api/list/utilities')) {
        try {
            const statusRes = await fetch('/api/auth/status');
            if (statusRes.ok) {
                const status = await statusRes.json();
                if (!status.loggedIn) {
                    const from = encodeURIComponent(window.location.pathname + window.location.search);
                    window.location.href = `/login?from=${from}`;
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
