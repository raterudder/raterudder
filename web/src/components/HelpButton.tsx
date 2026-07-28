import React, { useState } from 'react';
import { Dialog } from '@base-ui/react/dialog';
import './HelpButton.css';

export interface HelpButtonProps {
    title: string;
    description: React.ReactNode;
    ariaLabel?: string;
    className?: string;
}

export const HelpButton: React.FC<HelpButtonProps> = ({
    title,
    description,
    ariaLabel,
    className = '',
}) => {
    const [open, setOpen] = useState(false);

    return (
        <>
            <button
                className={`help-btn ${className}`}
                type="button"
                aria-label={ariaLabel || `More info`}
                onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    setOpen(true);
                }}
            >
                ?
            </button>
            {open && (
                <Dialog.Root open={open} onOpenChange={setOpen}>
                    <Dialog.Portal>
                        <Dialog.Backdrop className="dialog-backdrop" />
                        <Dialog.Popup className="dialog-popup help-dialog-popup">
                            <div className="help-dialog-header">
                                <Dialog.Title className="dialog-title help-dialog-title">
                                    {title}
                                </Dialog.Title>
                            </div>
                            <div className="dialog-description help-dialog-content">
                                {description}
                            </div>
                            <Dialog.Close
                                className="btn btn-primary dialog-close-btn"
                                type="button"
                            >
                                Got It
                            </Dialog.Close>
                        </Dialog.Popup>
                    </Dialog.Portal>
                </Dialog.Root>
            )}
        </>
    );
};
