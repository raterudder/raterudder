import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import SavingsHero from './SavingsHero';
import { type SavingsStats } from '../api';

describe('SavingsHero', () => {
    const defaultSavings: SavingsStats = {
        timestamp: new Date().toISOString(),
        batterySavings: 10,
        solarSavings: 20,
        cost: 5,
        credit: 2,
        avoidedCost: 12,
        chargingCost: 2,
        solarGenerated: 30,
        gridImported: 15,
        gridExported: 5,
        homeUsed: 40,
        batteryUsed: 10,
    };

    it('renders net savings correctly', () => {
        render(<SavingsHero savings={defaultSavings} />);
        // 10 + 20 + 2 = 32
        expect(screen.getByText('$ 32.00')).toBeInTheDocument();
    });

    it('renders negative savings with a minus sign', () => {
        const negativeSavings = { ...defaultSavings, batterySavings: -50 };
        render(<SavingsHero savings={negativeSavings} />);
        // -50 + 20 + 2 = -28
        expect(screen.getByText('- $ 28.00')).toBeInTheDocument();
    });

    it('renders individual metrics', () => {
        render(<SavingsHero savings={defaultSavings} />);
        expect(screen.getByText('Solar Gen')).toBeInTheDocument();
        expect(screen.getByText('30.0')).toBeInTheDocument();
        expect(screen.getByText('Home Usage')).toBeInTheDocument();
        expect(screen.getByText('40.0')).toBeInTheDocument();
    });

    it('returns null if savings is null', () => {
        const { container } = render(<SavingsHero savings={null} />);
        expect(container.firstChild).toBeNull();
    });

    it('renders planMode multi-period savings and swaps period on click', () => {
        const periods: SavingsStats[] = [
            {
                ...defaultSavings,
                totalSavingsDollars: 3.4,
                solarAndBatterySavingsDollars: 2.55,
                rateRudderSavingsDollars: 0.85,
                estimated: true,
                homeUsed: 32.4,
                solarGenerated: 25.0,
                batteryUsed: 11.6,
                gridImported: 14.2,
                gridExported: 6.8,
                credit: 0.45,
                cost: 0.75,
            },
            {
                ...defaultSavings,
                totalSavingsDollars: 22.8,
                solarAndBatterySavingsDollars: 17.4,
                rateRudderSavingsDollars: 5.4,
                estimated: false,
                homeUsed: 218.0,
                solarGenerated: 165.2,
                batteryUsed: 78.4,
                gridImported: 112.5,
                gridExported: 44.0,
                credit: 3.1,
                cost: 6.9,
            },
            {
                ...defaultSavings,
                totalSavingsDollars: 94.5,
                solarAndBatterySavingsDollars: 73.3,
                rateRudderSavingsDollars: 21.2,
                estimated: false,
                homeUsed: 910.5,
                solarGenerated: 712.0,
                batteryUsed: 324.0,
                gridImported: 418.0,
                gridExported: 182.0,
                credit: 12.4,
                cost: 24.6,
            },
        ];

        render(
            <SavingsHero
                savings={periods[0]}
                savingsPeriods={periods}
                planMode={true}
                isToday={true}
            />
        );

        expect(screen.getByText('Today Savings')).toBeInTheDocument();
        expect(screen.getByTestId('hero-primary-savings')).toHaveTextContent('$ 3.40');
        expect(screen.getByTestId('hero-without-rr-savings')).toHaveTextContent('($ 2.55)');
        expect(screen.getByTestId('hero-subcaption')).toHaveTextContent('Estimated. (Without RateRudder).');
        expect(screen.getByText('32.4')).toBeInTheDocument();

        // Click Last 7 Days button to swap period
        const weekBtn = screen.getByRole('button', { name: /Last 7 Days/i });
        fireEvent.click(weekBtn);

        expect(screen.getByText('Last 7 Days Savings')).toBeInTheDocument();
        expect(screen.getByTestId('hero-primary-savings')).toHaveTextContent('$ 22.80');
        expect(screen.getByTestId('hero-without-rr-savings')).toHaveTextContent('($ 17.40)');
        expect(screen.getByTestId('hero-subcaption')).toHaveTextContent('(Without RateRudder).');
        // Right-hand stat panel should update to 7-day usage
        expect(screen.getByText('218.0')).toBeInTheDocument();
        // Today should now be in the breakdown buttons
        expect(screen.getByRole('button', { name: /Today/i })).toBeInTheDocument();
    });

    it('hides Without RateRudder parenthetical when rateRudderSavingsDollars < 0.05', () => {
        const periods: SavingsStats[] = [
            {
                ...defaultSavings,
                totalSavingsDollars: 2.55,
                solarAndBatterySavingsDollars: 2.53,
                rateRudderSavingsDollars: 0.02,
                estimated: false,
            },
        ];

        render(
            <SavingsHero
                savings={periods[0]}
                savingsPeriods={periods}
                planMode={true}
                isToday={false}
                currentDate={new Date(2026, 2, 15)}
            />
        );

        expect(screen.getByText('Mar 15 Savings')).toBeInTheDocument();
        expect(screen.getByTestId('hero-primary-savings')).toHaveTextContent('$ 2.55');
        expect(screen.queryByTestId('hero-without-rr-savings')).not.toBeInTheDocument();
        expect(screen.queryByTestId('hero-subcaption')).not.toBeInTheDocument();
    });

    it('formats negative planMode savings and resets selectedPeriodIdx when currentDate changes', () => {
        const periods: SavingsStats[] = [
            {
                ...defaultSavings,
                totalSavingsDollars: -1.25,
                solarAndBatterySavingsDollars: -2.0,
                rateRudderSavingsDollars: 0.75,
                estimated: false,
            },
            {
                ...defaultSavings,
                totalSavingsDollars: 10.0,
                solarAndBatterySavingsDollars: 8.0,
                rateRudderSavingsDollars: 2.0,
                estimated: false,
            },
        ];

        const { rerender } = render(
            <SavingsHero
                savings={periods[0]}
                savingsPeriods={periods}
                planMode={true}
                isToday={false}
                currentDate={new Date(2026, 2, 15)}
            />
        );

        expect(screen.getByTestId('hero-primary-savings')).toHaveTextContent('- $ 1.25');
        expect(screen.getByTestId('hero-without-rr-savings')).toHaveTextContent('(- $ 2.00)');

        // Select Last 7 Days
        fireEvent.click(screen.getByRole('button', { name: /Last 7 Days/i }));
        expect(screen.getByText('Last 7 Days Savings')).toBeInTheDocument();

        // Change currentDate -> should reset back to period 0 (Mar 14 Savings)
        rerender(
            <SavingsHero
                savings={periods[0]}
                savingsPeriods={periods}
                planMode={true}
                isToday={false}
                currentDate={new Date(2026, 2, 14)}
            />
        );
        expect(screen.getByText('Mar 14 Savings')).toBeInTheDocument();
    });
});
