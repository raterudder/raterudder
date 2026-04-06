import React, { useEffect } from 'react';
import './DateSelector.css';

interface DateSelectorProps {
    currentDate: Date;
    onDateChange: (days: number) => void;
    loading?: boolean;
    isToday: boolean;
}

const DateSelector: React.FC<DateSelectorProps> = ({
    currentDate,
    onDateChange,
    loading = false,
    isToday
}) => {
    const formattedDate = currentDate.toLocaleDateString(undefined, {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric'
    });

    useEffect(() => {
        const handleKeyDown = (e: KeyboardEvent) => {
            if (document.activeElement?.tagName === 'INPUT' || document.activeElement?.tagName === 'TEXTAREA') return;

            if (e.key === 'ArrowLeft' && !loading) {
                onDateChange(-1);
            } else if (e.key === 'ArrowRight' && !loading && !isToday) {
                onDateChange(1);
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        return () => window.removeEventListener('keydown', handleKeyDown);
    }, [onDateChange, loading, isToday]);

    return (
        <div className="date-controls">
            <button
                onClick={() => onDateChange(-1)}
                disabled={loading}
                aria-label="Previous day"
                title="Previous day (Left Arrow)"
            >
                {loading && <span className="loading-spinner" aria-hidden="true"></span>}
                &lt; Prev
            </button>
            <h2>{formattedDate}</h2>
            <button
                onClick={() => onDateChange(1)}
                disabled={loading || isToday}
                aria-label="Next day"
                title={isToday ? "Cannot view future dates" : "Next day (Right Arrow)"}
            >
                {loading && <span className="loading-spinner" aria-hidden="true"></span>}
                Next &gt;
            </button>
        </div>
    );
};

export default DateSelector;
