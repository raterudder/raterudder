import React, { useState } from 'react';
import { Link } from 'wouter';
import { joinSite } from '../api';
import { Field } from '@base-ui/react/field';
import { Input } from '@base-ui/react/input';
import './SitePages.css';

interface JoinPageProps {
    onJoinSuccess: () => void;
}

const JoinPage: React.FC<JoinPageProps> = ({ onJoinSuccess }) => {
    const [siteID, setSiteID] = useState('');
    const [inviteCode, setInviteCode] = useState('');
    const [name, setName] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError('');

        if (!siteID.trim() || !inviteCode.trim() || !name.trim()) {
            setError('All fields are required.');
            return;
        }

        setLoading(true);
        try {
            await joinSite(siteID.trim(), inviteCode.trim(), name.trim());
            onJoinSuccess();
        } catch (err: any) {
            setError(err.message || 'Failed to join site');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="auth-page">
            <div className="auth-card">
                <h1>Join a Site</h1>
                <p>Enter the Site ID and Invite Code provided by the site owner.</p>

                <form onSubmit={handleSubmit} className="join-form">
                    <Field.Root className="join-field">
                        <Field.Label htmlFor="join-site-id">Site ID</Field.Label>
                        <Input
                            id="join-site-id"
                            type="text"
                            value={siteID}
                            onChange={(e) => setSiteID(e.target.value)}
                            placeholder="e.g. my-home"
                            autoComplete="off"
                            disabled={loading}
                        />
                    </Field.Root>

                    <Field.Root className="join-field">
                        <Field.Label htmlFor="join-name">Site Name</Field.Label>
                        <Input
                            id="join-name"
                            type="text"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            placeholder="e.g. My Home"
                            autoComplete="off"
                            disabled={loading}
                            required
                        />
                    </Field.Root>

                    <Field.Root className="join-field">
                        <Field.Label htmlFor="join-invite-code">Invite Code</Field.Label>
                        <Input
                            id="join-invite-code"
                            type="text"
                            value={inviteCode}
                            onChange={(e) => setInviteCode(e.target.value)}
                            placeholder="Enter invite code"
                            autoComplete="off"
                            disabled={loading}
                        />
                    </Field.Root>

                    {error && <div className="join-error">{error}</div>}

                    <p className="join-consent">
                        By joining, you agree to our{' '}
                        <Link to="/terms">Terms of Service</Link> and{' '}
                        <Link to="/privacy">Privacy Policy</Link>.
                    </p>

                    <button
                        type="submit"
                        className="join-submit"
                        disabled={loading || !siteID.trim() || !inviteCode.trim() || !name.trim()}
                    >
                        {loading && <span className="loading-spinner" aria-hidden="true"></span>}
                        {loading ? 'Joining…' : 'Join Site'}
                    </button>
                </form>
            </div>

            <p className="auth-alternate-link">
                <Link href="/new-site">Don't have a site yet? Create one.</Link>
            </p>
        </div>
    );
};

export default JoinPage;
