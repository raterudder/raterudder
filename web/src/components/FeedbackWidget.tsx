import React, { useState } from 'react';
import { useLocation } from 'wouter';
import { submitFeedback } from '../api';
import { Popover } from '@base-ui/react/popover';
import './FeedbackWidget.css';

interface FeedbackWidgetProps {
    siteID: string;
}

const FeedbackWidget: React.FC<FeedbackWidgetProps> = ({ siteID }) => {
    const [isOpen, setIsOpen] = useState(false);
    const [sentiment, setSentiment] = useState<string | null>(null);
    const [comment, setComment] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState(false);

    // Fallback if wouter isn't managing all location state
    const [location] = useLocation();

    const handleOpenChange = (open: boolean) => {
        if (open) {
            // Reset state when opening
            setSentiment(null);
            setComment('');
            setError(null);
            setSuccess(false);
        }
        setIsOpen(open);
    };

    const handleSubmit = async () => {
        setLoading(true);
        setError(null);

        const extra = {
            pathname: window.location.pathname || location,
            userAgent: navigator.userAgent
        };

        try {
            await submitFeedback(siteID, sentiment || 'neutral', comment, extra);
            setSuccess(true);
            setTimeout(() => {
                setIsOpen(false);
                setSuccess(false);
            }, 3000);
        } catch (err: any) {
            console.error("Failed to submit feedback:", err);
            setError(err.message || 'Failed to submit feedback. Please try again later.');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="feedback-widget-container">
            <Popover.Root open={isOpen} onOpenChange={(open) => handleOpenChange(open)}>
                <Popover.Trigger aria-label="Feedback" className="feedback-fab">
                    <span aria-hidden="true">💬</span>
                </Popover.Trigger>
                <Popover.Portal>
                    <Popover.Positioner sideOffset={8}>
                        <Popover.Popup className="feedback-popup">
                            <Popover.Arrow className="feedback-arrow">
                                <ArrowSvg />
                            </Popover.Arrow>
                            {success ? (
                                <div className="feedback-success">
                                    <Popover.Title className="feedback-title">Thank you!</Popover.Title>
                                    <Popover.Description className="feedback-desc">Your feedback has been submitted.</Popover.Description>
                                </div>
                            ) : (
                                <>
                                    <Popover.Title className="feedback-title">How are you feeling about RateRudder?</Popover.Title>

                                    <div className="feedback-sentiment">
                                        <button
                                            className={`sentiment-btn ${sentiment === 'sad' ? 'selected' : ''}`}
                                            onClick={() => setSentiment('sad')}
                                            title="Sad"
                                            aria-label="Sad"
                                            aria-pressed={sentiment === 'sad'}
                                            type="button"
                                        >
                                            <span aria-hidden="true">😞</span>
                                        </button>
                                        <button
                                            className={`sentiment-btn ${sentiment === 'neutral' ? 'selected' : ''}`}
                                            onClick={() => setSentiment('neutral')}
                                            title="Neutral"
                                            aria-label="Neutral"
                                            aria-pressed={sentiment === 'neutral'}
                                            type="button"
                                        >
                                            <span aria-hidden="true">😐</span>
                                        </button>
                                        <button
                                            className={`sentiment-btn ${sentiment === 'happy' ? 'selected' : ''}`}
                                            onClick={() => setSentiment('happy')}
                                            title="Happy"
                                            aria-label="Happy"
                                            aria-pressed={sentiment === 'happy'}
                                            type="button"
                                        >
                                            <span aria-hidden="true">😀</span>
                                        </button>
                                    </div>

                                    <textarea
                                        className="feedback-textarea"
                                        placeholder="Tell us more about your experience (optional)..."
                                        aria-label="Tell us more about your experience (optional)"
                                        value={comment}
                                        onChange={(e) => setComment(e.target.value)}
                                    />

                                    {error && <div className="feedback-error" role="alert">{error}</div>}

                                    <div className="feedback-actions">
                                        <Popover.Close
                                            className="feedback-cancel-btn"
                                            disabled={loading}
                                        >
                                            Cancel
                                        </Popover.Close>
                                        <button
                                            className="feedback-submit-btn"
                                            onClick={handleSubmit}
                                            type="button"
                                            disabled={loading || (comment.trim() === '' && sentiment === null)}
                                            title={(comment.trim() === '' && sentiment === null) ? "Please select a sentiment or leave a comment" : undefined}
                                        >
                                            {loading && <span className="loading-spinner" aria-hidden="true"></span>}
                                            {loading ? 'Sending...' : 'Submit'}
                                        </button>
                                    </div>
                                </>
                            )}
                        </Popover.Popup>
                    </Popover.Positioner>
                </Popover.Portal>
            </Popover.Root>
        </div>
    );
};

function ArrowSvg(props: React.ComponentProps<'svg'>) {
  return (
    <svg width="20" height="10" viewBox="0 0 20 10" fill="none" {...props}>
      <path
        d="M9.66437 2.60207L4.80758 6.97318C4.07308 7.63423 3.11989 8 2.13172 8H0V10H20V8H18.5349C17.5468 8 16.5936 7.63423 15.8591 6.97318L11.0023 2.60207C10.622 2.2598 10.0447 2.25979 9.66437 2.60207Z"
        className="feedback-arrow-fill"
      />
      <path
        d="M8.99542 1.85876C9.75604 1.17425 10.9106 1.17422 11.6713 1.85878L16.5281 6.22989C17.0789 6.72568 17.7938 7.00001 18.5349 7.00001L15.89 7L11.0023 2.60207C10.622 2.2598 10.0447 2.2598 9.66436 2.60207L4.77734 7L2.13171 7.00001C2.87284 7.00001 3.58774 6.72568 4.13861 6.22989L8.99542 1.85876Z"
        className="feedback-arrow-outer-stroke"
      />
      <path
        d="M10.3333 3.34539L5.47654 7.71648C4.55842 8.54279 3.36693 9 2.13172 9H0V8H2.13172C3.11989 8 4.07308 7.63423 4.80758 6.97318L9.66437 2.60207C10.0447 2.25979 10.622 2.2598 11.0023 2.60207L15.8591 6.97318C16.5936 7.63423 17.5468 8 18.5349 8H20V9H18.5349C17.2998 9 16.1083 8.54278 15.1901 7.71648L10.3333 3.34539Z"
        className="feedback-arrow-inner-stroke"
      />
    </svg>
  );
}

export default FeedbackWidget;
