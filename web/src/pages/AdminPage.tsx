import React, { useEffect, useState } from 'react';
import { listSites, listFeedback } from '../api';
import type { AdminSite, Feedback } from '../api';
import { Separator } from '@base-ui/react/separator';
import './AdminPage.css';

const AdminPage: React.FC = () => {
    const [sites, setSites] = useState<AdminSite[]>([]);
    const [feedbacks, setFeedbacks] = useState<Feedback[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        Promise.all([
            listSites(),
            listFeedback(50)
        ])
            .then(([sitesData, feedbackData]) => {
                setSites(sitesData || []);
                setFeedbacks(feedbackData || []);
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

    if (loading) {
        return <div className="loading-screen">Loading Admin Data...</div>;
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

            <div className="admin-list">
                {sites.map((site) => (
                    <div key={site.id} className="card admin-site-card">
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
                ))}
            </div>

            <div className="admin-header" style={{ marginTop: '2rem' }}>
                <h1>Feedback</h1>
            </div>

            <Separator className="admin-separator" />

            <div className="admin-list">
                {feedbacks.length > 0 && (
                    feedbacks.map((fb) => (
                        <div key={fb.id} className="card admin-site-card" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: '0.5rem' }}>
                            <div style={{ display: 'flex', justifyContent: 'space-between', width: '100%', fontSize: '0.875rem', color: 'var(--text-muted-color)' }}>
                                <span>{new Date(fb.timestamp).toLocaleString()}</span>
                                <span>Site: {fb.siteID} | User: {fb.userID}</span>
                            </div>
                            <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
                                <span style={{ fontSize: '1.5rem' }}>
                                    {fb.sentiment === 'happy' ? '😀' : fb.sentiment === 'sad' ? '😞' : '😐'}
                                </span>
                                <div style={{ fontSize: '1rem' }}>{fb.comment || <em>No comment</em>}</div>
                            </div>
                            {fb.extra && Object.keys(fb.extra).length > 0 && (
                                <div style={{ fontSize: '0.75rem', color: 'var(--text-muted-color)', marginTop: '0.5rem', wordBreak: 'break-all' }}>
                                    {Object.entries(fb.extra).map(([k, v]) => `${k}: ${v}`).join(' | ')}
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
