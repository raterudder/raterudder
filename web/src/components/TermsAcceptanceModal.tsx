import React, { useState } from 'react';
import { Dialog } from '@base-ui/react/dialog';
import { acceptTerms } from '../api';
import './TermsAcceptanceModal.css';

interface TermsAcceptanceModalProps {
    open: boolean;
    onAccepted: () => void;
    onLogout: () => void;
}

export const TermsAcceptanceModal: React.FC<TermsAcceptanceModalProps> = ({
    open,
    onAccepted,
    onLogout,
}) => {
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);

    if (!open) {
        return null;
    }

    const handleAccept = async () => {
        setSubmitting(true);
        setError(null);
        try {
            await acceptTerms();
            onAccepted();
        } catch (err: any) {
            setError(err?.message || 'Failed to save acceptance. Please try again.');
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <Dialog.Root
            open={open}
            disablePointerDismissal
            onOpenChange={(isOpen, eventDetails) => {
                if (!isOpen) {
                    eventDetails.cancel();
                }
            }}
        >
            <Dialog.Portal>
                <Dialog.Backdrop className="dialog-backdrop" />
                <Dialog.Popup className="dialog-popup terms-dialog-popup" data-testid="terms-acceptance-modal">
                    <div className="terms-dialog-header">
                        <Dialog.Title className="dialog-title">
                            Updated Terms &amp; Privacy Policy
                        </Dialog.Title>
                        <Dialog.Description className="dialog-description">
                            We updated our Terms of Service and Privacy Policy in plain English to reflect how RateRudder works today:
                        </Dialog.Description>
                    </div>

                    <ul className="terms-dialog-summary">
                        <li>
                            <strong>Data &amp; Optional Features:</strong> Explains encrypted battery credentials, optional location lookups for weather forecasts, optional feature tokens/integrations, and ongoing energy monitoring while automation is paused.
                        </li>
                        <li>
                            <strong>Open-Source Benchmarks:</strong> Clarifies how aggregated stats and de-identified site energy histories help test and improve RateRudder&apos;s scheduler.
                        </li>
                        <li>
                            <strong>Beta &amp; Hardware Expectations:</strong> Clarifies that RateRudder is an independent free beta, isn&apos;t a guaranteed backup-power system for critical/medical loads, requires matching settings to your utility&apos;s rules, and caps liability.
                        </li>
                    </ul>

                    <p className="terms-dialog-links">
                        Please review the full{' '}
                        <a href="/terms" target="_blank" rel="noopener noreferrer">
                            Terms of Service
                        </a>{' '}
                        and{' '}
                        <a href="/privacy" target="_blank" rel="noopener noreferrer">
                            Privacy Policy
                        </a>
                        .
                    </p>

                    {error && <div className="terms-dialog-error" role="alert">{error}</div>}

                    <div className="terms-dialog-actions">
                        <button
                            type="button"
                            className="btn btn-secondary"
                            onClick={onLogout}
                            disabled={submitting}
                        >
                            Log Out
                        </button>
                        <button
                            type="button"
                            className="btn btn-primary"
                            onClick={handleAccept}
                            disabled={submitting}
                        >
                            {submitting && <span className="loading-spinner" aria-hidden="true"></span>}
                            {submitting ? 'Saving…' : 'Agree & Continue'}
                        </button>
                    </div>

                    <p className="terms-dialog-note">
                        Don&apos;t agree and want to pause automation or delete your site and account? Email{' '}
                        <a href="mailto:privacy@raterudder.com">privacy@raterudder.com</a> and we&apos;ll take care of it for you.
                    </p>
                </Dialog.Popup>
            </Dialog.Portal>
        </Dialog.Root>
    );
};
