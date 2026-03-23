import { useState } from 'react';
import { Link } from 'wouter';
import { Field } from '@base-ui/react/field';
import { createSite } from '../api';

export interface NewSitePageProps {
    onJoinSuccess: () => void;
}

const NewSitePage = ({ onJoinSuccess }: NewSitePageProps) => {
    const [name, setName] = useState('');
    const [error, setError] = useState<string | null>(null);
    const [isSubmitting, setIsSubmitting] = useState(false);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);

        if (!name.trim()) {
            setError('Site Name is required');
            return;
        }

        setIsSubmitting(true);
        try {
            await createSite(name.trim());
            onJoinSuccess();
        } catch (err: any) {
            setError(err.message || 'Failed to create site');
            setIsSubmitting(false);
        }
    };

    return (
        <div className="auth-page">
            <div className="auth-card">
                <h1>Create a New Site</h1>
                <p>
                    A site represents a set of solar panels and batteries.
                    We'll monitor and optimize your energy independently for each site.
                </p>

                <form onSubmit={handleSubmit} className="join-form">
                    <Field.Root className="join-field">
                        <Field.Label htmlFor="create-name">Site Name</Field.Label>
                        <Field.Control
                            id="create-name"
                            className="input"
                            value={name}
                            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
                            disabled={isSubmitting}
                            required
                        />
                    </Field.Root>

                    {error && <div className="join-error">{error}</div>}

                    <p className="join-consent">
                        By creating, you agree to our{' '}
                        <a href="/terms" target="_blank" rel="noopener noreferrer">Terms of Service</a> and{' '}
                        <a href="/privacy" target="_blank" rel="noopener noreferrer">Privacy Policy</a>.
                    </p>

                    <button
                        type="submit"
                        disabled={isSubmitting || !name.trim()}
                        className="join-submit"
                    >
                        {isSubmitting && <span className="loading-spinner" aria-hidden="true"></span>}
                        {isSubmitting ? 'Creating...' : 'Create Site'}
                    </button>
                </form>
            </div>

            <p className="auth-alternate-link">
                <Link href="/join-site">Already have a site? Join it.</Link>
            </p>
        </div>
    );
};

export default NewSitePage;

