import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { HelpButton } from './HelpButton';

describe('HelpButton', () => {
    it('renders the help trigger button', () => {
        render(<HelpButton title="Test Feature" description="This is a test description." />);
        const button = screen.getByRole('button', { name: /more info/i });
        expect(button).toBeInTheDocument();
        expect(button).toHaveTextContent('?');
    });

    it('opens dialog with title and description when clicked', async () => {
        render(
            <HelpButton
                title="Minimum Reserve SOC"
                description="Keeps battery charge above threshold."
            />
        );

        const trigger = screen.getByRole('button', { name: /more info/i });
        fireEvent.click(trigger);

        expect(await screen.findByRole('dialog')).toBeInTheDocument();
        expect(screen.getByText('Minimum Reserve SOC')).toBeInTheDocument();
        expect(screen.getByText('Keeps battery charge above threshold.')).toBeInTheDocument();
    });

    it('closes dialog when Got It button is clicked', async () => {
        render(
            <HelpButton
                title="Minimum Reserve SOC"
                description="Keeps battery charge above threshold."
            />
        );

        const trigger = screen.getByRole('button', { name: /more info/i });
        fireEvent.click(trigger);

        const closeBtn = await screen.findByRole('button', { name: /got it/i });
        fireEvent.click(closeBtn);

        expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
});
