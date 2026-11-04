import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { FeaturesProvider, useFeatureFlag, useFeatures } from './features';

const FlagConsumer: React.FC<{ flagName: string }> = ({ flagName }) => {
    const enabled = useFeatureFlag(flagName);
    const all = useFeatures();
    return (
        <div>
            <span data-testid="flag-value">{enabled ? 'enabled' : 'disabled'}</span>
            <span data-testid="all-count">{Object.keys(all).length}</span>
        </div>
    );
};

describe('FeaturesContext and hooks', () => {
    it('defaults to false when no provider is present', () => {
        render(<FlagConsumer flagName="myFeature" />);
        expect(screen.getByTestId('flag-value').textContent).toBe('disabled');
        expect(screen.getByTestId('all-count').textContent).toBe('0');
    });

    it('returns configured boolean values from FeaturesProvider', () => {
        const { rerender } = render(
            <FeaturesProvider features={{ myFeature: true, otherFeature: false }}>
                <FlagConsumer flagName="myFeature" />
            </FeaturesProvider>
        );
        expect(screen.getByTestId('flag-value').textContent).toBe('enabled');
        expect(screen.getByTestId('all-count').textContent).toBe('2');

        rerender(
            <FeaturesProvider features={{ myFeature: true, otherFeature: false }}>
                <FlagConsumer flagName="otherFeature" />
            </FeaturesProvider>
        );
        expect(screen.getByTestId('flag-value').textContent).toBe('disabled');

        rerender(
            <FeaturesProvider features={{ myFeature: true, otherFeature: false }}>
                <FlagConsumer flagName="unknownFeature" />
            </FeaturesProvider>
        );
        expect(screen.getByTestId('flag-value').textContent).toBe('disabled');
    });
});
