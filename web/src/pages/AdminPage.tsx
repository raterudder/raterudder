import React, { useEffect, useState } from 'react';
import { listSites, listFeedback, listInterest } from '../api';
import type { AdminSite, Feedback, InterestSubmission } from '../api';
import { Separator } from '@base-ui/react/separator';
import './AdminPage.css';

const AdminPage: React.FC = () => {
    const [sites, setSites] = useState<AdminSite[]>([]);
    const [feedbacks, setFeedbacks] = useState<Feedback[]>([]);
    const [interest, setInterest] = useState<InterestSubmission[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

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
