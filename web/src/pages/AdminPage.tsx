import React, { useEffect, useState } from 'react';
import { listSites, listFeedback, listInterest, setSiteAlias, listUserSites } from '../api';
import type { AdminSite, Feedback, InterestSubmission, UserSite } from '../api';
import { Separator } from '@base-ui/react/separator';
import './AdminPage.css';

const AdminPage: React.FC = () => {
    const [sites, setSites] = useState<AdminSite[]>([]);
    const [feedbacks, setFeedbacks] = useState<Feedback[]>([]);
    const [interest, setInterest] = useState<InterestSubmission[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    // Search and Alias State
    const [userSites, setUserSites] = useState<UserSite[] | null>(null);
    const [searchQuery, setSearchQuery] = useState('');
    const [activeSearchQuery, setActiveSearchQuery] = useState('');
    const [loadingSearch, setLoadingSearch] = useState(false);
    const [editingSiteId, setEditingSiteId] = useState<string | null>(null);
    const [editingAliasValue, setEditingAliasValue] = useState('');

    useEffect(() => {
        Promise.all([
            listSites(),
            listFeedback(50),
            listInterest(50)
        ])
            .then(([sitesData, feedbackData, interestData]) => {
                setSites(sitesData || []);
                setFeedbacks(feedbackData || []);
                setInterest(interestData || []);
                setError(null);
            })
            .catch((err) => {
                console.error("Failed to load admin data:", err);
                setError(err.message || 'Failed to load admin data. Ensure you have admin access.');
            })
            .finally(() => {
                setLoading(false);
            });
    }, []);

    const handleSearch = async (e?: React.FormEvent) => {
        if (e) e.preventDefault();
        const query = searchQuery.trim();
        if (!query) {
            setActiveSearchQuery('');
            return;
        }

        if (userSites === null) {
            setLoadingSearch(true);
            try {
                const data = await listUserSites();
                setUserSites(data || []);
            } catch (err) {
                console.error("Failed to load user sites for search:", err);
            } finally {
                setLoadingSearch(false);
            }
        }
        setActiveSearchQuery(query);
    };

    const handleClearSearch = () => {
        setSearchQuery('');
        setActiveSearchQuery('');
    };

    const handleSaveAlias = async (siteID: string) => {
        try {
            await setSiteAlias(siteID, editingAliasValue);
            setSites((prevSites) =>
                prevSites.map((s) => (s.id === siteID ? { ...s, alias: editingAliasValue } : s))
            );
            setEditingSiteId(null);
        } catch (err: any) {
            alert(err.message || 'Failed to update alias');
        }
    };

    const filteredSites = sites.filter((site) => {
        if (!activeSearchQuery) return true;
        const query = activeSearchQuery.toLowerCase();

        // Match site ID
        if (site.id.toLowerCase().includes(query)) return true;

        // Match alias
        if (site.alias && site.alias.toLowerCase().includes(query)) return true;

        // Match names from userSites list
        if (userSites) {
            return userSites.some((us) => us.id === site.id && us.name.toLowerCase().includes(query));
        }
        return false;
    });

    if (loading) {
        return (
            <div className="loading-screen">
                <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
                <span>Loading Admin Data...</span>
            </div>
        );
    }

    if (error) {
        return (
            <div className="content-container admin-page">
                <div className="admin-error">{error}</div>
            </div>
        );
    }

    return (
        <div className="content-container admin-page">
            <div className="admin-header">
                <h1>Site List</h1>
            </div>

            <Separator className="admin-separator" />

            <form onSubmit={handleSearch} className="admin-search-container">
                <input
                    type="text"
                    placeholder="Search site names, IDs or aliases..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="admin-search-input"
                />
                <button type="submit" className="btn admin-search-btn" disabled={loadingSearch}>
                    {loadingSearch ? 'Searching...' : 'Search'}
                </button>
                {activeSearchQuery && (
                    <button type="button" onClick={handleClearSearch} className="btn admin-clear-btn">
                        Clear
                    </button>
                )}
            </form>

            <div className="admin-list">
                {filteredSites.length === 0 ? (
                    <div className="admin-empty">No sites match the search criteria.</div>
                ) : (
                    filteredSites.map((site) => (
                        <div key={site.id} className="card admin-site-card admin-card-col">
                            <div className="admin-site-row">
                                <div className="admin-site-info">
                                    <h3 className="admin-site-id">{site.id}</h3>
                                    {site.lastAction && (
                                        <div className="admin-site-action">
                                            Last Action: {site.lastAction.description} @ {new Date(site.lastAction.timestamp).toLocaleString()}<br/>
                                            Battery SOC: {site.lastAction.systemStatus?.batterySOC?.toFixed(1) || '0'}%
                                        </div>
                                    )}
                                </div>
                                <a href={`/dashboard?viewSite=${site.id}`} className="btn admin-primary-btn">
                                    View Dashboard
                                </a>
                            </div>

                            <div className="admin-site-footer">
                                {editingSiteId === site.id ? (
                                    <div className="admin-alias-edit-form">
                                        <input
                                            type="text"
                                            value={editingAliasValue}
                                            onChange={(e) => setEditingAliasValue(e.target.value)}
                                            className="admin-alias-input"
                                            placeholder="Add site alias..."
                                            autoFocus
                                        />
                                        <button onClick={() => handleSaveAlias(site.id)} className="btn admin-alias-save-btn">Save</button>
                                        <button onClick={() => setEditingSiteId(null)} className="btn admin-alias-cancel-btn">Cancel</button>
                                    </div>
                                ) : (
                                    <div className="admin-alias-display">
                                        <span className="admin-alias-text">
                                            {site.alias ? `Alias: ${site.alias}` : 'No alias set'}
                                        </span>
                                        <button
                                            onClick={() => {
                                                setEditingSiteId(site.id);
                                                setEditingAliasValue(site.alias || '');
                                            }}
                                            className="admin-alias-edit-btn"
                                            title="Edit Alias"
                                        >
                                            ✏️
                                        </button>
                                    </div>
                                )}
                            </div>
                        </div>
                    ))
                )}
            </div>

            <div className="admin-header secondary">
                <h1>Feedback</h1>
            </div>

            <Separator className="admin-separator" />

            <div className="admin-list">
                {feedbacks.length > 0 && (
                    feedbacks.map((fb) => (
                        <div key={fb.id} className="card admin-site-card admin-card-col">
                            <div className="admin-card-meta">
                                <span>{new Date(fb.timestamp).toLocaleString()}</span>
                                <span>Site: {fb.siteID} | User: {fb.userID}</span>
                            </div>
                            <div className="feedback-row">
                                <span className="feedback-sentiment">
                                    {fb.sentiment === 'happy' ? '😀' : fb.sentiment === 'sad' ? '😞' : '😐'}
                                </span>
                                <div className="feedback-comment">{fb.comment || <em>No comment</em>}</div>
                            </div>
                            {fb.extra && Object.keys(fb.extra).length > 0 && (
                                <div className="feedback-extra">
                                    {Object.entries(fb.extra).map(([k, v]) => `${k}: ${v}`).join(' | ')}
                                </div>
                            )}
                        </div>
                    ))
                )}
            </div>
            <div className="admin-header secondary">
                <h1>Interest Submissions</h1>
            </div>

            <Separator className="admin-separator" />

            <div className="admin-list">
                {interest.length > 0 && (
                    interest.map((sub) => (
                        <div key={sub.email} className="card admin-site-card admin-card-col">
                            <div className="admin-card-meta">
                                <span>{new Date(sub.timestamp).toLocaleString()}</span>
                                <span style={{ fontWeight: '600' }}>{sub.email}</span>
                            </div>
                            <div className="interest-grid">
                                <div><strong>Utility:</strong> {sub.utility} {sub.utilityProviderName && `(${sub.utilityProviderName})`}</div>
                                <div><strong>Battery:</strong> {sub.battery} {sub.batteryName && `(${sub.batteryName})`}</div>
                                {sub.state && <div><strong>State:</strong> {sub.state}</div>}
                                {sub.planName && <div><strong>Plan:</strong> {sub.planName}</div>}
                            </div>
                            {sub.comments && (
                                <div className="interest-comments">
                                    {sub.comments}
                                </div>
                            )}
                        </div>
                    ))
                )}
            </div>
        </div>
    );
};

export default AdminPage;
