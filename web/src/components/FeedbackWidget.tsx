import React, { useState } from 'react';
import { useLocation } from 'wouter';
import { submitFeedback } from '../api';
import './FeedbackWidget.css';

interface FeedbackWidgetProps {
    siteID: string;
}

const FeedbackWidget: React.FC<FeedbackWidgetProps> = ({ siteID }) => {
    const [isOpen, setIsOpen] = useState(false);
    const [sentiment, setSentiment] = useState('neutral');
    const [comment, setComment] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState(false);

    // Fallback if wouter isn't managing all location state
    const [location] = useLocation();

    const toggleOpen = () => {
        if (!isOpen) {
            // Reset state when opening
            setSentiment('neutral');
            setComment('');
            setError(null);
            setSuccess(false);
        }
        setIsOpen(!isOpen);
    };

    const handleSubmit = async () => {
        setLoading(true);
        setError(null);

        const extra = {
            pathname: window.location.pathname || location,
            userAgent: navigator.userAgent
        };

        try {
            await submitFeedback(siteID, sentiment, comment, extra);
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
            {isOpen && (
                <div className="feedback-popup">
                    {success ? (
                        <div className="feedback-success">
                            <h3>Thank you!</h3>
                            <p>Your feedback has been submitted.</p>
                        </div>
                    ) : (
                        <>
                            <h3>How are you feeling about RateRudder?</h3>

                            <div className="feedback-sentiment">
                                <button
                                    className={`sentiment-btn ${sentiment === 'sad' ? 'selected' : ''}`}
                                    onClick={() => setSentiment('sad')}
                                    title="Sad"
                                    type="button"
                                >
                                    😞
                                </button>
                                <button
                                    className={`sentiment-btn ${sentiment === 'neutral' ? 'selected' : ''}`}
                                    onClick={() => setSentiment('neutral')}
                                    title="Neutral"
                                    type="button"
                                >
                                    😐
                                </button>
                                <button
                                    className={`sentiment-btn ${sentiment === 'happy' ? 'selected' : ''}`}
                                    onClick={() => setSentiment('happy')}
                                    title="Happy"
                                    type="button"
                                >
                                    😀
                                </button>
                            </div>

                            <textarea
                                className="feedback-textarea"
                                placeholder="Tell us more about your experience (optional)..."
                                value={comment}
                                onChange={(e) => setComment(e.target.value)}
                            />

                            {error && <div className="feedback-error">{error}</div>}

                            <div className="feedback-actions">
                                <button
                                    className="feedback-cancel-btn"
                                    onClick={toggleOpen}
                                    type="button"
                                    disabled={loading}
                                >
                                    Cancel
                                </button>
                                <button
                                    className="feedback-submit-btn"
                                    onClick={handleSubmit}
                                    type="button"
                                    disabled={loading || (comment.trim() === '' && sentiment === 'neutral')}
                                >
                                    {loading ? 'Sending...' : 'Submit'}
                                </button>
                            </div>
                        </>
                    )}
                </div>
            )}
            <button className="feedback-fab" onClick={toggleOpen} aria-label="Feedback">
                💬
            </button>
        </div>
    );
};

export default FeedbackWidget;
