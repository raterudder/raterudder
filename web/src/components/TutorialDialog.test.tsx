import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { TutorialDialog } from './TutorialDialog';
import { type Settings as SettingsType } from '../api';

const defaultSettings = {
    pause: false,
    ess: 'tesla',
    hasCredentials: { tesla: true },
    dryRun: false,
    countryCode: 'US',
    postalCode: '90210',
    utilityProvider: 'comed',
    utilityRate: 'besh',
    utilityRateOptions: {},
    gridChargeBatteries: true,
    gridExportSolar: false,
    gridExportBatteries: false
} as unknown as SettingsType;

describe('TutorialDialog Component', () => {
    it('does not render when open is false', () => {
        const onClose = vi.fn();
        render(<TutorialDialog open={false} onClose={onClose} settings={defaultSettings} />);
        expect(screen.queryByText(/Welcome to RateRudder!/i)).not.toBeInTheDocument();
    });

    it('renders step 1 when open is true', () => {
        const onClose = vi.fn();
        render(<TutorialDialog open={true} onClose={onClose} settings={defaultSettings} />);
        expect(screen.getByText(/Welcome to RateRudder!/i)).toBeInTheDocument();
        expect(screen.getByText(/switch it into/i)).toBeInTheDocument();
        expect(screen.getByText(/Self-Consumption/i)).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /Next/i })).toBeInTheDocument();
    });

    it('navigates to step 2 when Next is clicked, and back when Back is clicked', () => {
        const onClose = vi.fn();
        render(<TutorialDialog open={true} onClose={onClose} settings={defaultSettings} />);
        
        // Step 1
        expect(screen.getByText(/Welcome to RateRudder!/i)).toBeInTheDocument();
        const nextBtn = screen.getByRole('button', { name: /Next/i });
        fireEvent.click(nextBtn);

        // Step 2
        expect(screen.getByText(/Manual Charging/i)).toBeInTheDocument();
        expect(screen.queryByText(/Welcome to RateRudder!/i)).not.toBeInTheDocument();
        
        const backBtn = screen.getByRole('button', { name: /Back/i });
        fireEvent.click(backBtn);

        // Back to Step 1
        expect(screen.getByText(/Welcome to RateRudder!/i)).toBeInTheDocument();
    });

    it('calls onClose when Got It is clicked on step 2', () => {
        const onClose = vi.fn();
        render(<TutorialDialog open={true} onClose={onClose} settings={defaultSettings} />);
        
        const nextBtn = screen.getByRole('button', { name: /Next/i });
        fireEvent.click(nextBtn);

        const gotItBtn = screen.getByRole('button', { name: /Got It/i });
        fireEvent.click(gotItBtn);

        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('calls onClose immediately if settings has pause set to true', () => {
        const onClose = vi.fn();
        const pausedSettings = { ...defaultSettings, pause: true };
        const { rerender } = render(<TutorialDialog open={true} onClose={onClose} settings={defaultSettings} />);
        
        expect(onClose).not.toHaveBeenCalled();

        // Rerender with paused settings
        rerender(<TutorialDialog open={true} onClose={onClose} settings={pausedSettings} />);
        
        expect(onClose).toHaveBeenCalledTimes(1);
    });
});
