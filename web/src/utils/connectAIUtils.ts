import type { UserApp } from '../api';

export const hasExistingGeminiApp = (apps: UserApp[]): boolean =>
    apps.some(app => /^gemini(\s+\d+)?$/i.test(app.name.trim()));

export const getNextGeminiAppName = (apps: UserApp[]): string => {
    const existingNames = new Set(apps.map(app => app.name.trim().toLowerCase()));
    if (!existingNames.has('gemini')) {
        return 'Gemini';
    }
    let suffix = 2;
    while (existingNames.has(`gemini ${suffix}`)) {
        suffix += 1;
    }
    return `Gemini ${suffix}`;
};

export const buildSummaryUrl = (origin: string, token: string): string =>
    `${origin}/api/agent/summary?token=${encodeURIComponent(token)}`;

export const buildMcpPrompt = (origin: string): string =>
    `Connect to my RateRudder home energy account using the remote MCP server at ${origin}/mcp (read-only OAuth). Once connected, summarize my current battery charge, solar forecast, electricity rates, and recent savings.`;

export const buildGeminiSummaryPrompt = (summaryUrl: string): string =>
    `Create a reusable skill for checking my RateRudder home energy data by fetching ${summaryUrl}. Use it now to summarize my current battery charge, solar forecast, electricity rates, and recent savings.`;

export const buildOtherSummaryPrompt = (summaryUrl: string): string =>
    `Fetch my live RateRudder home energy data from ${summaryUrl} and summarize my current battery charge, solar forecast, electricity rates, and recent savings.`;
