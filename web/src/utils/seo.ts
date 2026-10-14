import { useEffect } from 'react';

export interface PageSEO {
    title: string;
    description: string;
    path: string;
}

export const PUBLIC_PAGES_SEO = {
    index: {
        title: 'RateRudder | Smart Battery & Solar Arbitrage for Tesla Powerwall & FranklinWH',
        description: 'Automate your home battery to charge during cheap grid rates and export during peak utility credits. Real-time dynamic arbitrage for Tesla Powerwall, FranklinWH, and Enphase.',
        path: '/',
    },
    'how-it-works': {
        title: 'How SmartPlan Works | 24-Hour Dynamic Battery Optimization — RateRudder',
        description: 'Discover how RateRudder SmartPlan evaluates thousands of 24-hour battery trajectories at sub-hour resolution using real-time grid rates, solar forecasts, and practical guardrails.',
        path: '/how-it-works',
    },
    'getting-started': {
        title: 'Getting Started | 2-Minute Setup Guide — RateRudder',
        description: 'Connect your Tesla Powerwall, FranklinWH, or Enphase battery to RateRudder in under 2 minutes with zero hardware changes and secure OAuth.',
        path: '/getting-started',
    },
    utilities: {
        title: 'Supported Utilities & Rate Plans | 50+ Real-Time & TOU Tariffs — RateRudder',
        description: 'Browse 50+ electric utilities and 120+ residential rate plans supported by RateRudder, including ComEd PJM, Ameren MISO, Texas ERCOT, California NEM 3.0, and Eversource VPP.',
        path: '/utilities',
    },
    changelog: {
        title: 'Product Changelog | Recent Improvements & Releases — RateRudder',
        description: 'Explore recent RateRudder improvements including SmartPlan 24-hour trajectory optimization, Texas ERCOT and California NEM 3.0 tariffs, and automatic vacation detection.',
        path: '/changelog',
    },
    privacy: {
        title: 'Privacy Policy — RateRudder',
        description: 'Learn how RateRudder collects, uses, and protects your account and home battery telemetry information.',
        path: '/privacy',
    },
    terms: {
        title: 'Terms of Service — RateRudder',
        description: 'Review the Terms of Service for using RateRudder automated home battery optimization.',
        path: '/terms',
    },
} as const satisfies Record<string, PageSEO>;

function escapeHtmlAttr(value: string): string {
    return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;');
}

export function renderSubpageHtml(indexHtml: string, seo: PageSEO): string {
    const canonicalUrl = `https://raterudder.com${seo.path}`;
    const safeTitle = escapeHtmlAttr(seo.title);
    const safeDesc = escapeHtmlAttr(seo.description);

    return indexHtml
        .replace(/<title>[\s\S]*?<\/title>/, `<title>${safeTitle}</title>`)
        .replace(/(<meta\s+name="title"\s+content=")[^"]*(")/, `$1${safeTitle}$2`)
        .replace(/(<meta\s+name="description"\s+content=")[^"]*(")/, `$1${safeDesc}$2`)
        .replace(/(<link\s+rel="canonical"\s+href=")[^"]*(")/, `$1${canonicalUrl}$2`)
        .replace(/(<meta\s+property="og:url"\s+content=")[^"]*(")/, `$1${canonicalUrl}$2`)
        .replace(/(<meta\s+property="og:title"\s+content=")[^"]*(")/, `$1${safeTitle}$2`)
        .replace(/(<meta\s+property="og:description"\s+content=")[^"]*(")/, `$1${safeDesc}$2`)
        .replace(/(<meta\s+name="twitter:url"\s+content=")[^"]*(")/, `$1${canonicalUrl}$2`)
        .replace(/(<meta\s+name="twitter:title"\s+content=")[^"]*(")/, `$1${safeTitle}$2`)
        .replace(/(<meta\s+name="twitter:description"\s+content=")[^"]*(")/, `$1${safeDesc}$2`)
        .replace(/\s*<!-- Schema\.org JSON-LD Structured Data -->\s*<script type="application\/ld\+json">[\s\S]*?<\/script>/, '');
}

interface MinimalDoc {
    title: string;
    querySelector(selector: string): { setAttribute(name: string, value: string): void } | null;
}

function getDoc(): MinimalDoc | undefined {
    return (globalThis as { document?: MinimalDoc }).document;
}

function setMetaContent(selector: string, content: string) {
    const el = getDoc()?.querySelector(selector);
    if (el) {
        el.setAttribute('content', content);
    }
}

function setCanonicalHref(href: string) {
    const el = getDoc()?.querySelector('link[rel="canonical"]');
    if (el) {
        el.setAttribute('href', href);
    }
}

export function usePageSEO({ title, description, path }: PageSEO) {
    useEffect(() => {
        const doc = getDoc();
        if (!doc) return;
        const canonicalUrl = `https://raterudder.com${path}`;
        doc.title = title;
        setMetaContent('meta[name="title"]', title);
        setMetaContent('meta[name="description"]', description);
        setMetaContent('meta[property="og:title"]', title);
        setMetaContent('meta[property="og:description"]', description);
        setMetaContent('meta[property="og:url"]', canonicalUrl);
        setMetaContent('meta[name="twitter:title"]', title);
        setMetaContent('meta[name="twitter:description"]', description);
        setMetaContent('meta[name="twitter:url"]', canonicalUrl);
        setCanonicalHref(canonicalUrl);
    }, [title, description, path]);
}
