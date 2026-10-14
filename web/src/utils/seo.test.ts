import { renderHook } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import realIndexHtml from '../../index.html?raw';
import { PUBLIC_PAGES_SEO, renderSubpageHtml, usePageSEO } from './seo';

describe('seo utilities', () => {
    it('renders subpage HTML from the real index.html template with updated metadata and stripped homepage JSON-LD', () => {
        const seo = PUBLIC_PAGES_SEO.utilities;
        const out = renderSubpageHtml(realIndexHtml, seo);

        const expectedTitle = 'Supported Utilities &amp; Rate Plans | 50+ Real-Time &amp; TOU Tariffs — RateRudder';
        const expectedDesc = seo.description;
        const expectedUrl = 'https://raterudder.com/utilities';

        expect(out).toContain(`<title>${expectedTitle}</title>`);
        expect(out).toContain(`<meta name="title" content="${expectedTitle}" />`);
        expect(out).toContain(`<meta name="description" content="${expectedDesc}" />`);
        expect(out).toContain(`<link rel="canonical" href="${expectedUrl}" />`);
        expect(out).toContain(`<meta property="og:url" content="${expectedUrl}" />`);
        expect(out).toContain(`<meta property="og:title" content="${expectedTitle}" />`);
        expect(out).toContain(`<meta property="og:description" content="${expectedDesc}" />`);
        expect(out).toContain(`<meta name="twitter:url" content="${expectedUrl}" />`);
        expect(out).toContain(`<meta name="twitter:title" content="${expectedTitle}" />`);
        expect(out).toContain(`<meta name="twitter:description" content="${expectedDesc}" />`);

        // Ensure homepage-specific values and JSON-LD were replaced/removed
        expect(out).not.toContain('href="https://raterudder.com/"');
        expect(out).not.toContain('content="https://raterudder.com/"');
        expect(out).not.toContain(PUBLIC_PAGES_SEO.index.title);
        expect(out).not.toContain(PUBLIC_PAGES_SEO.index.description);
        expect(out).not.toContain('application/ld+json');
    });

    it('updates document title via usePageSEO', () => {
        renderHook(() => usePageSEO(PUBLIC_PAGES_SEO['how-it-works']));
        expect(document.title).toBe(PUBLIC_PAGES_SEO['how-it-works'].title);
    });
});
