import React, { useState, useEffect } from 'react';
import { Dialog } from '@base-ui/react/dialog';
import { type Settings as SettingsType } from '../api';
import './TutorialDialog.css';

interface TutorialDialogProps {
    open: boolean;
    onClose: () => void;
    settings: SettingsType | null;
}

export const TutorialDialog: React.FC<TutorialDialogProps> = ({ open, onClose, settings }) => {
    const [step, setStep] = useState(1);

    // Auto-close if pause is enabled while the tutorial is open
    useEffect(() => {
        if (open && settings?.pause) {
            onClose();
        }
    }, [open, settings?.pause, onClose]);

    const [prevOpen, setPrevOpen] = useState(open);
    if (open !== prevOpen) {
        setPrevOpen(open);
        if (open) {
            setStep(1);
        }
    }

    if (!open) return null;

    return (
        <Dialog.Root open={open} onOpenChange={(isOpen) => { if (!isOpen) onClose(); }}>
            <Dialog.Portal>
                <Dialog.Backdrop className="dialog-backdrop" />
                <Dialog.Popup className="dialog-popup tutorial-dialog-popup">
                    {step === 1 ? (
                        <>
                            <Dialog.Title className="dialog-title tutorial-title">
                                Welcome to RateRudder! 🚀
                            </Dialog.Title>
                            <div className="tutorial-content">
                                <div className="tutorial-illustration">
                                    <span className="icon-pulse" role="img" aria-label="lightning">⚡</span>
                                </div>
                                <p className="dialog-description tutorial-desc">
                                    RateRudder will take over your system and switch it into <strong>Self-Consumption</strong> mode.
                                </p>
                                <p className="dialog-description tutorial-desc">
                                    It will respect <strong>VPP</strong> and <strong>Storm events</strong> automatically.
                                </p>
                            </div>
                            <div className="tutorial-footer">
                                <div className="tutorial-steps">
                                    <span className="step-dot active"></span>
                                    <span className="step-dot"></span>
                                </div>
                                <button className="btn btn-primary" type="button" onClick={() => setStep(2)}>
                                    Next
                                </button>
                            </div>
                        </>
                    ) : (
                        <>
                            <Dialog.Title className="dialog-title tutorial-title">
                                Manual Charging 💡
                            </Dialog.Title>
                            <div className="tutorial-content">
                                <div className="tutorial-illustration">
                                    <span className="icon-pulse" role="img" aria-label="plug">🔌</span>
                                </div>
                                <p className="dialog-description tutorial-desc">
                                    If you need to manually charge, switch your system to <strong>Emergency/Backup</strong> mode.
                                </p>
                                <p className="dialog-description tutorial-desc">
                                    RateRudder will automatically pause until you put it back into <strong>Self-Consumption</strong> mode.
                                </p>
                            </div>
                            <div className="tutorial-footer">
                                <div className="tutorial-steps">
                                    <span className="step-dot"></span>
                                    <span className="step-dot active"></span>
                                </div>
                                <div className="tutorial-buttons">
                                    <button className="btn btn-secondary" type="button" onClick={() => setStep(1)}>
                                        Back
                                    </button>
                                    <button className="btn btn-primary" type="button" onClick={onClose}>
                                        Got It
                                    </button>
                                </div>
                            </div>
                        </>
                    )}
                </Dialog.Popup>
            </Dialog.Portal>
        </Dialog.Root>
    );
};
