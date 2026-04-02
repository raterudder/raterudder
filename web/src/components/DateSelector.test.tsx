import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import DateSelector from './DateSelector';

describe('DateSelector', () => {
    const defaultProps = {
        currentDate: new Date('2023-10-27T12:00:00'),
        onDateChange: vi.fn(),
        isToday: false,
        loading: false
    };

    it('renders the formatted date', () => {
        render(<DateSelector {...defaultProps} />);
        // The exact format depends on the locale, but it should contain the day, month, and year
        expect(screen.getByText(/Friday/i)).toBeInTheDocument();
        expect(screen.getByText(/October/i)).toBeInTheDocument();
        expect(screen.getByText(/2023/i)).toBeInTheDocument();
    });

    it('calls onDateChange when Prev button is clicked', () => {
        render(<DateSelector {...defaultProps} />);
        const prevButton = screen.getByLabelText(/Previous day/i);
        fireEvent.click(prevButton);
        expect(defaultProps.onDateChange).toHaveBeenCalledWith(-1);
    });

    it('calls onDateChange when Next button is clicked', () => {
        render(<DateSelector {...defaultProps} />);
        const nextButton = screen.getByLabelText(/Next day/i);
        fireEvent.click(nextButton);
        expect(defaultProps.onDateChange).toHaveBeenCalledWith(1);
    });

    it('disables buttons when loading', () => {
        render(<DateSelector {...defaultProps} loading={true} />);
        expect(screen.getByLabelText(/Previous day/i)).toBeDisabled();
        expect(screen.getByLabelText(/Next day/i)).toBeDisabled();
    });

    it('disables Next button when isToday is true', () => {
        render(<DateSelector {...defaultProps} isToday={true} />);
        expect(screen.getByLabelText(/Next day/i)).toBeDisabled();
        expect(screen.getByLabelText(/Previous day/i)).not.toBeDisabled();
    });

    it('responds to keyboard arrows', () => {
        const onDateChange = vi.fn();
        render(<DateSelector {...defaultProps} onDateChange={onDateChange} />);
        
        fireEvent.keyDown(window, { key: 'ArrowLeft' });
        expect(onDateChange).toHaveBeenCalledWith(-1);

        fireEvent.keyDown(window, { key: 'ArrowRight' });
        expect(onDateChange).toHaveBeenCalledWith(1);
    });

    it('does not respond to ArrowRight when isToday is true', () => {
        const onDateChange = vi.fn();
        render(<DateSelector {...defaultProps} onDateChange={onDateChange} isToday={true} />);
        
        fireEvent.keyDown(window, { key: 'ArrowRight' });
        expect(onDateChange).not.toHaveBeenCalledWith(1);
    });
});
