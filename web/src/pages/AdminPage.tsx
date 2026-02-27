import React, { useEffect, useState } from 'react';
import { listSites } from '../api';
import type { AdminSite } from '../api';
import { Link } from 'wouter';
import { Separator } from '@base-ui/react/separator';
import './AdminPage.css';

const AdminPage: React.FC = () => {
    const [sites, setSites] = useState<AdminSite[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        listSites()
            .then((data) => {
                setSites(data || []);
                setError(null);
            })
            .catch((err) => {
                console.error("Failed to list sites:", err);
                setError(err.message || 'Failed to list sites. Ensure you have admin access.');
            })
            .finally(() => {
                setLoading(false);
            });
    }, []);

    if (loading) {
        return <div className="loading-screen">Loading Sites...</div>;
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
                        <Link href={`/dashboard?viewSite=${site.id}`} className="btn admin-primary-btn">
                            View Dashboard
                        </Link>
                    </div>
                ))}
            </div>
        </div>
    );
};

export default AdminPage;
