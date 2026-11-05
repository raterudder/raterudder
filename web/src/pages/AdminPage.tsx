import React, { useEffect, useState } from 'react';
import {
    listSites,
    listFeedback,
    listInterest,
    setSiteAlias,
    listUserSites,
    listFeatures,
    updateFeatures,
} from '../api';
import type {
    AdminSite,
    Feedback,
    InterestSubmission,
    UserSite,
    AdminFeatureFlag,
    FeatureCriteria,
} from '../api';
import { Collapsible } from '@base-ui/react/collapsible';
import { Autocomplete } from '@base-ui/react/autocomplete';
import { Combobox } from '@base-ui/react/combobox';
import './AdminPage.css';

const LAST_SEEN_FEEDBACK_KEY = 'admin_last_seen_feedback_ts';
const LAST_SEEN_INTEREST_KEY = 'admin_last_seen_interest_ts';
const ONE_WEEK_MS = 7 * 24 * 60 * 60 * 1000;

function fuzzyMatch(query: string, target: string): boolean {
    const q = query.trim().toLowerCase();
    if (!q) return false;
    const t = target.toLowerCase();
    if (t.includes(q)) return true;

    let qi = 0;
    for (let ti = 0; ti < t.length && qi < q.length; ti++) {
        while (qi < q.length && q[qi] === ' ') {
            qi++;
        }
        if (qi < q.length && t[ti] === q[qi]) {
            qi++;
        }
    }
    while (qi < q.length && q[qi] === ' ') {
        qi++;
    }
    return qi === q.length;
}

function sortFeatureFlags(flags: AdminFeatureFlag[]): AdminFeatureFlag[] {
    return [...flags].sort((a, b) => a.name.localeCompare(b.name));
}

function getFlagSiteOverride(flag: AdminFeatureFlag, siteID: string): 'default' | 'on' | 'off' {
    for (const c of flag.criteria || []) {
        if (c.siteIDs && c.siteIDs.includes(siteID)) {
            return c.value ? 'on' : 'off';
        }
    }
    return 'default';
}

function getFlagRollout(flag: AdminFeatureFlag): { sitePercent: number; value: boolean } {
    for (const c of flag.criteria || []) {
        if (c.sitePercent && c.sitePercent > 0) {
            return { sitePercent: c.sitePercent, value: c.value };
        }
    }
    return { sitePercent: 0, value: true };
}

function normalizeCriteria(criteria: FeatureCriteria[]): FeatureCriteria[] {
    const trueSites: string[] = [];
    const falseSites: string[] = [];
    const seen = new Set<string>();
    let percentCriterion: FeatureCriteria | null = null;

    for (const c of criteria) {
        for (const rawId of c.siteIDs || []) {
            const id = rawId.trim();
            if (!id || seen.has(id)) continue;
            seen.add(id);
            if (c.value) {
                trueSites.push(id);
            } else {
                falseSites.push(id);
            }
        }
        if (c.sitePercent && c.sitePercent > 0 && !percentCriterion) {
            percentCriterion = {
                sitePercent: c.sitePercent,
                value: c.value,
            };
        }
    }

    const out: FeatureCriteria[] = [];
    if (trueSites.length > 0) {
        out.push({ siteIDs: trueSites, value: true });
    }
    if (falseSites.length > 0) {
        out.push({ siteIDs: falseSites, value: false });
    }
    if (percentCriterion) {
        out.push(percentCriterion);
    }
    return out;
}

function buildUpdatedCriteriaForSiteOverrides(
    flag: AdminFeatureFlag,
    siteIDs: string[],
    override: 'default' | 'on' | 'off'
): FeatureCriteria[] {
    const targetSet = new Set(siteIDs);
    const next: FeatureCriteria[] = [];
    for (const c of flag.criteria || []) {
        const remainingSites = (c.siteIDs || []).filter((id) => !targetSet.has(id));
        if (remainingSites.length > 0 || (c.sitePercent && c.sitePercent > 0)) {
            next.push({
                ...(remainingSites.length > 0 ? { siteIDs: remainingSites } : {}),
                ...(c.sitePercent && c.sitePercent > 0 ? { sitePercent: c.sitePercent } : {}),
                value: c.value,
            });
        }
    }
    if (override === 'on' || override === 'off') {
        next.push({
            siteIDs,
            value: override === 'on',
        });
    }
    return normalizeCriteria(next);
}

function buildUpdatedCriteriaForRollout(
    flag: AdminFeatureFlag,
    sitePercent: number,
    value: boolean
): FeatureCriteria[] {
    const next: FeatureCriteria[] = [];
    for (const c of flag.criteria || []) {
        if (c.siteIDs && c.siteIDs.length > 0) {
            next.push({
                siteIDs: c.siteIDs,
                value: c.value,
            });
        }
    }
    if (sitePercent > 0) {
        next.push({
            sitePercent,
            value,
        });
    }
    return normalizeCriteria(next);
}

const AdminPage: React.FC = () => {
    const [feedbacks, setFeedbacks] = useState<Feedback[]>([]);
    const [interest, setInterest] = useState<InterestSubmission[]>([]);
    const [features, setFeatures] = useState<AdminFeatureFlag[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    // Collapsible Section States
    const [feedbackOpen, setFeedbackOpen] = useState(false);
    const [interestOpen, setInterestOpen] = useState(false);
    const [featuresOpen, setFeaturesOpen] = useState(false);
    const [sitesOpen, setSitesOpen] = useState(false);

    // Seen timestamps in localStorage
    const [lastSeenFeedbackTs, setLastSeenFeedbackTs] = useState<string | null>(() =>
        localStorage.getItem(LAST_SEEN_FEEDBACK_KEY)
    );
    const [lastSeenInterestTs, setLastSeenInterestTs] = useState<string | null>(() =>
        localStorage.getItem(LAST_SEEN_INTEREST_KEY)
    );

    // Lazy-loaded Sites State
    const [sites, setSites] = useState<AdminSite[]>([]);
    const [sitesLoaded, setSitesLoaded] = useState(false);
    const [loadingSites, setLoadingSites] = useState(false);
    const [sitesError, setSitesError] = useState<string | null>(null);

    // Search and Alias State
    const [userSites, setUserSites] = useState<UserSite[] | null>(null);
    const [searchQuery, setSearchQuery] = useState('');
    const [activeSearchQuery, setActiveSearchQuery] = useState('');
    const [loadingSearch, setLoadingSearch] = useState(false);
    const [editingSiteId, setEditingSiteId] = useState<string | null>(null);
    const [editingAliasValue, setEditingAliasValue] = useState('');

    // Feature Flags Editing State
    const [rolloutEdits, setRolloutEdits] = useState<Record<string, { sitePercent: number; value: boolean }>>({});
    const [newSiteInputs, setNewSiteInputs] = useState<
        Record<string, { siteID: string; pendingSiteIDs: string[]; value: boolean }>
    >({});
    const [savingFlag, setSavingFlag] = useState<string | null>(null);

    const syncRolloutEdits = (sortedFlags: AdminFeatureFlag[]) => {
        const edits: Record<string, { sitePercent: number; value: boolean }> = {};
        for (const f of sortedFlags) {
            edits[f.name] = getFlagRollout(f);
        }
        setRolloutEdits(edits);
    };

    useEffect(() => {
        Promise.all([
            listFeedback(50),
            listInterest(50),
            Promise.resolve(listFeatures()).catch(() => [] as AdminFeatureFlag[]),
        ])
            .then(([feedbackData, interestData, featuresData]) => {
                const loadedFeedbacks = feedbackData || [];
                const loadedInterest = interestData || [];
                setFeedbacks(loadedFeedbacks);
                setInterest(loadedInterest);

                const savedFeedbackTs = localStorage.getItem(LAST_SEEN_FEEDBACK_KEY);
                const hasUnseenFeedback = loadedFeedbacks.some((fb) =>
                    savedFeedbackTs
                        ? new Date(fb.timestamp).getTime() > new Date(savedFeedbackTs).getTime()
                        : true
                );
                setFeedbackOpen(hasUnseenFeedback);

                const savedInterestTs = localStorage.getItem(LAST_SEEN_INTEREST_KEY);
                const hasUnseenInterest = loadedInterest.some((sub) =>
                    savedInterestTs
                        ? new Date(sub.timestamp).getTime() > new Date(savedInterestTs).getTime()
                        : true
                );
                setInterestOpen(hasUnseenInterest);

                const sorted = sortFeatureFlags(featuresData || []);
                setFeatures(sorted);
                syncRolloutEdits(sorted);
                setError(null);
            })
            .catch((err) => {
                console.error('Failed to load admin data:', err);
                setError(err.message || 'Failed to load admin data. Ensure you have admin access.');
            })
            .finally(() => {
                setLoading(false);
            });
    }, []);

    const handleSitesOpenChange = (open: boolean) => {
        setSitesOpen(open);
        if (open && !sitesLoaded && !loadingSites) {
            setLoadingSites(true);
            setSitesError(null);
            listSites()
                .then((sitesData) => {
                    setSites(sitesData || []);
                    setSitesLoaded(true);
                })
                .catch((err) => {
                    console.error('Failed to load sites:', err);
                    setSitesError(err.message || 'Failed to load sites.');
                })
                .finally(() => {
                    setLoadingSites(false);
                });
        }
    };

    const handleMarkFeedbackSeen = (e: React.MouseEvent) => {
        e.stopPropagation();
        if (feedbacks.length === 0) return;
        const newestTs = feedbacks.reduce(
            (max, fb) => (new Date(fb.timestamp).getTime() > new Date(max).getTime() ? fb.timestamp : max),
            feedbacks[0].timestamp
        );
        localStorage.setItem(LAST_SEEN_FEEDBACK_KEY, newestTs);
        setLastSeenFeedbackTs(newestTs);
    };

    const handleMarkInterestSeen = (e: React.MouseEvent) => {
        e.stopPropagation();
        if (interest.length === 0) return;
        const newestTs = interest.reduce(
            (max, sub) => (new Date(sub.timestamp).getTime() > new Date(max).getTime() ? sub.timestamp : max),
            interest[0].timestamp
        );
        localStorage.setItem(LAST_SEEN_INTEREST_KEY, newestTs);
        setLastSeenInterestTs(newestTs);
    };

    const handleApplyFeatureCriteria = async (
        flagName: string,
        nextCriteria: FeatureCriteria[],
        siteOverrideUpdate?: { siteIDs: string[]; effectiveValue: boolean }
    ) => {
        const normalized = normalizeCriteria(nextCriteria);
        setSavingFlag(flagName);
        try {
            await updateFeatures({ [flagName]: normalized });
            setFeatures((prev) => {
                const updated = prev.map((f) =>
                    f.name === flagName ? { ...f, criteria: normalized } : f
                );
                syncRolloutEdits(updated);
                return updated;
            });
            if (siteOverrideUpdate) {
                const updatedSiteSet = new Set(siteOverrideUpdate.siteIDs);
                setSites((prev) =>
                    prev.map((s) =>
                        updatedSiteSet.has(s.id)
                            ? {
                                  ...s,
                                  features: {
                                      ...(s.features || {}),
                                      [flagName]: siteOverrideUpdate.effectiveValue,
                                  },
                              }
                            : s
                    )
                );
            }
        } catch (err: any) {
            alert(err.message || 'Failed to update feature flag');
        } finally {
            setSavingFlag(null);
        }
    };

    const handleSaveRollout = async (flag: AdminFeatureFlag) => {
        const edit = rolloutEdits[flag.name] ?? getFlagRollout(flag);
        const nextCriteria = buildUpdatedCriteriaForRollout(flag, edit.sitePercent, edit.value);
        await handleApplyFeatureCriteria(flag.name, nextCriteria);
    };

    const handleAddSiteToFlag = async (flag: AdminFeatureFlag) => {
        const input = newSiteInputs[flag.name] ?? {
            siteID: '',
            pendingSiteIDs: [],
            value: true,
        };
        const trimmedID = input.siteID.trim();
        const allSiteIDs = [...input.pendingSiteIDs];
        if (trimmedID && !allSiteIDs.includes(trimmedID)) {
            allSiteIDs.push(trimmedID);
        }
        if (allSiteIDs.length === 0) return;

        const nextCriteria = buildUpdatedCriteriaForSiteOverrides(
            flag,
            allSiteIDs,
            input.value ? 'on' : 'off'
        );
        await handleApplyFeatureCriteria(flag.name, nextCriteria, {
            siteIDs: allSiteIDs,
            effectiveValue: input.value,
        });
        setNewSiteInputs((prev) => ({
            ...prev,
            [flag.name]: { siteID: '', pendingSiteIDs: [], value: input.value },
        }));
    };

    const handleRemoveSiteFromFlag = async (flag: AdminFeatureFlag, siteID: string) => {
        const nextCriteria = buildUpdatedCriteriaForSiteOverrides(flag, [siteID], 'default');
        await handleApplyFeatureCriteria(flag.name, nextCriteria, {
            siteIDs: [siteID],
            effectiveValue: flag.defaultValue,
        });
    };

    const handleSiteCardFeatureOverride = async (
        site: AdminSite,
        flag: AdminFeatureFlag,
        override: 'default' | 'on' | 'off'
    ) => {
        const nextCriteria = buildUpdatedCriteriaForSiteOverrides(flag, [site.id], override);
        const effectiveValue =
            override === 'on' ? true : override === 'off' ? false : flag.defaultValue;
        await handleApplyFeatureCriteria(flag.name, nextCriteria, {
            siteIDs: [site.id],
            effectiveValue,
        });
    };

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
                console.error('Failed to load user sites for search:', err);
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

    const nowMs = Date.now();
    const recentFeedbackCount = feedbacks.filter(
        (fb) => nowMs - new Date(fb.timestamp).getTime() <= ONE_WEEK_MS
    ).length;
    const unseenFeedbackCount = feedbacks.filter((fb) =>
        lastSeenFeedbackTs
            ? new Date(fb.timestamp).getTime() > new Date(lastSeenFeedbackTs).getTime()
            : true
    ).length;

    const recentInterestCount = interest.filter(
        (sub) => nowMs - new Date(sub.timestamp).getTime() <= ONE_WEEK_MS
    ).length;
    const unseenInterestCount = interest.filter((sub) =>
        lastSeenInterestTs
            ? new Date(sub.timestamp).getTime() > new Date(lastSeenInterestTs).getTime()
            : true
    ).length;

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
                <h1>Admin Dashboard</h1>
            </div>

            {/* 1. Feedback Section */}
            <Collapsible.Root
                open={feedbackOpen}
                onOpenChange={setFeedbackOpen}
                className="admin-collapsible-section"
            >
                <div className="admin-section-header-row">
                    <Collapsible.Trigger className="admin-collapsible-trigger">
                        <span className="admin-collapsible-chevron" aria-hidden="true">
                            {feedbackOpen ? '▼' : '▶'}
                        </span>
                        <h2>Feedback</h2>
                        <div className="admin-badges">
                            <span className="admin-badge">{feedbacks.length} total</span>
                            {recentFeedbackCount > 0 && (
                                <span className="admin-badge admin-badge-recent">
                                    {recentFeedbackCount} in last 7d
                                </span>
                            )}
                            {unseenFeedbackCount > 0 && (
                                <span className="admin-badge admin-badge-new">
                                    {unseenFeedbackCount} new
                                </span>
                            )}
                        </div>
                    </Collapsible.Trigger>
                    {unseenFeedbackCount > 0 && (
                        <button
                            type="button"
                            className="btn admin-mark-seen-btn"
                            onClick={handleMarkFeedbackSeen}
                        >
                            Mark Feedback Seen
                        </button>
                    )}
                </div>
                <Collapsible.Panel className="admin-collapsible-panel">
                    <div className="admin-list">
                        {feedbacks.length === 0 ? (
                            <div className="admin-empty">No feedback submitted yet.</div>
                        ) : (
                            feedbacks.map((fb) => {
                                const isNew = lastSeenFeedbackTs
                                    ? new Date(fb.timestamp).getTime() >
                                      new Date(lastSeenFeedbackTs).getTime()
                                    : false;
                                return (
                                    <div key={fb.id} className="card admin-site-card admin-card-col">
                                        <div className="admin-card-meta">
                                            <span>
                                                {new Date(fb.timestamp).toLocaleString()}
                                                {isNew && (
                                                    <span className="admin-item-new-pill">NEW</span>
                                                )}
                                            </span>
                                            <span>
                                                Site: {fb.siteID} | User: {fb.userID}
                                            </span>
                                        </div>
                                        <div className="feedback-row">
                                            <span className="feedback-sentiment">
                                                {fb.sentiment === 'happy'
                                                    ? '😀'
                                                    : fb.sentiment === 'sad'
                                                      ? '😞'
                                                      : '😐'}
                                            </span>
                                            <div className="feedback-comment">
                                                {fb.comment || <em>No comment</em>}
                                            </div>
                                        </div>
                                        {fb.extra && Object.keys(fb.extra).length > 0 && (
                                            <div className="feedback-extra">
                                                {Object.entries(fb.extra)
                                                    .map(([k, v]) => `${k}: ${v}`)
                                                    .join(' | ')}
                                            </div>
                                        )}
                                    </div>
                                );
                            })
                        )}
                    </div>
                </Collapsible.Panel>
            </Collapsible.Root>

            {/* 2. Interest Submissions Section */}
            <Collapsible.Root
                open={interestOpen}
                onOpenChange={setInterestOpen}
                className="admin-collapsible-section"
            >
                <div className="admin-section-header-row">
                    <Collapsible.Trigger className="admin-collapsible-trigger">
                        <span className="admin-collapsible-chevron" aria-hidden="true">
                            {interestOpen ? '▼' : '▶'}
                        </span>
                        <h2>Interest Submissions</h2>
                        <div className="admin-badges">
                            <span className="admin-badge">{interest.length} total</span>
                            {recentInterestCount > 0 && (
                                <span className="admin-badge admin-badge-recent">
                                    {recentInterestCount} in last 7d
                                </span>
                            )}
                            {unseenInterestCount > 0 && (
                                <span className="admin-badge admin-badge-new">
                                    {unseenInterestCount} new
                                </span>
                            )}
                        </div>
                    </Collapsible.Trigger>
                    {unseenInterestCount > 0 && (
                        <button
                            type="button"
                            className="btn admin-mark-seen-btn"
                            onClick={handleMarkInterestSeen}
                        >
                            Mark Interest Seen
                        </button>
                    )}
                </div>
                <Collapsible.Panel className="admin-collapsible-panel">
                    <div className="admin-list">
                        {interest.length === 0 ? (
                            <div className="admin-empty">No interest submissions yet.</div>
                        ) : (
                            interest.map((sub) => {
                                const isNew = lastSeenInterestTs
                                    ? new Date(sub.timestamp).getTime() >
                                      new Date(lastSeenInterestTs).getTime()
                                    : false;
                                return (
                                    <div
                                        key={sub.email}
                                        className="card admin-site-card admin-card-col"
                                    >
                                        <div className="admin-card-meta">
                                            <span>
                                                {new Date(sub.timestamp).toLocaleString()}
                                                {isNew && (
                                                    <span className="admin-item-new-pill">NEW</span>
                                                )}
                                            </span>
                                            <span style={{ fontWeight: '600' }}>{sub.email}</span>
                                        </div>
                                        <div className="interest-grid">
                                            <div>
                                                <strong>Utility:</strong> {sub.utility}{' '}
                                                {sub.utilityProviderName &&
                                                    `(${sub.utilityProviderName})`}
                                            </div>
                                            <div>
                                                <strong>Battery:</strong> {sub.battery}{' '}
                                                {sub.batteryName && `(${sub.batteryName})`}
                                            </div>
                                            {sub.state && (
                                                <div>
                                                    <strong>State:</strong> {sub.state}
                                                </div>
                                            )}
                                            {sub.planName && (
                                                <div>
                                                    <strong>Plan:</strong> {sub.planName}
                                                </div>
                                            )}
                                        </div>
                                        {sub.comments && (
                                            <div className="interest-comments">{sub.comments}</div>
                                        )}
                                    </div>
                                );
                            })
                        )}
                    </div>
                </Collapsible.Panel>
            </Collapsible.Root>

            {/* 3. Feature Flags Section */}
            <Collapsible.Root
                open={featuresOpen}
                onOpenChange={setFeaturesOpen}
                className="admin-collapsible-section"
            >
                <div className="admin-section-header-row">
                    <Collapsible.Trigger className="admin-collapsible-trigger">
                        <span className="admin-collapsible-chevron" aria-hidden="true">
                            {featuresOpen ? '▼' : '▶'}
                        </span>
                        <h2>Feature Flags</h2>
                        <div className="admin-badges">
                            <span className="admin-badge">{features.length} defined</span>
                        </div>
                    </Collapsible.Trigger>
                </div>
                <Collapsible.Panel className="admin-collapsible-panel">
                    <div className="admin-list">
                        {features.length === 0 ? (
                            <div className="admin-empty">No feature flags defined.</div>
                        ) : (
                            features.map((flag) => {
                                const rolloutEdit =
                                    rolloutEdits[flag.name] ?? getFlagRollout(flag);
                                const siteInput = newSiteInputs[flag.name] ?? {
                                    siteID: '',
                                    pendingSiteIDs: [],
                                    value: true,
                                };
                                const enabledSites = (flag.criteria || [])
                                    .filter((c) => c.value && c.siteIDs && c.siteIDs.length > 0)
                                    .flatMap((c) => c.siteIDs || []);
                                const disabledSites = (flag.criteria || [])
                                    .filter((c) => !c.value && c.siteIDs && c.siteIDs.length > 0)
                                    .flatMap((c) => c.siteIDs || []);

                                return (
                                    <div
                                        key={flag.name}
                                        className="card admin-site-card admin-card-col admin-feature-card"
                                    >
                                        <div className="admin-feature-header">
                                            <span className="admin-feature-name">{flag.name}</span>
                                            <span
                                                className={`admin-badge ${
                                                    flag.defaultValue
                                                        ? 'admin-badge-on'
                                                        : 'admin-badge-off'
                                                }`}
                                            >
                                                Default: {flag.defaultValue ? 'ON' : 'OFF'}
                                            </span>
                                        </div>

                                        <div className="admin-feature-controls">
                                            <form
                                                className="admin-feature-rollout-row"
                                                onSubmit={(e) => {
                                                    e.preventDefault();
                                                    handleSaveRollout(flag);
                                                }}
                                            >
                                                <label className="admin-feature-label">
                                                    Rollout %
                                                    <input
                                                        type="number"
                                                        min={0}
                                                        max={100}
                                                        aria-label={`Rollout percentage for ${flag.name}`}
                                                        value={rolloutEdit.sitePercent}
                                                        onChange={(e) =>
                                                            setRolloutEdits((prev) => ({
                                                                ...prev,
                                                                [flag.name]: {
                                                                    ...rolloutEdit,
                                                                    sitePercent: Math.max(
                                                                        0,
                                                                        Math.min(
                                                                            100,
                                                                            Number(e.target.value) || 0
                                                                        )
                                                                    ),
                                                                },
                                                            }))
                                                        }
                                                        className="admin-feature-number-input"
                                                    />
                                                </label>
                                                <label className="admin-feature-label">
                                                    Rollout Value
                                                    <select
                                                        aria-label={`Rollout value for ${flag.name}`}
                                                        value={rolloutEdit.value ? 'true' : 'false'}
                                                        onChange={(e) =>
                                                            setRolloutEdits((prev) => ({
                                                                ...prev,
                                                                [flag.name]: {
                                                                    ...rolloutEdit,
                                                                    value: e.target.value === 'true',
                                                                },
                                                            }))
                                                        }
                                                        className="admin-feature-select"
                                                    >
                                                        <option value="true">ON</option>
                                                        <option value="false">OFF</option>
                                                    </select>
                                                </label>
                                                <button
                                                    type="submit"
                                                    className="btn admin-search-btn"
                                                    disabled={savingFlag === flag.name}
                                                >
                                                    Save Rollout
                                                </button>
                                            </form>

                                            <div className="admin-feature-sites-section">
                                                <div className="admin-feature-chips-group">
                                                    <span className="admin-feature-sublabel">
                                                        Enabled Sites (ON):
                                                    </span>
                                                    {enabledSites.length === 0 ? (
                                                        <span className="admin-feature-none">
                                                            None
                                                        </span>
                                                    ) : (
                                                        enabledSites.map((id) => (
                                                            <span
                                                                key={id}
                                                                className="admin-site-chip admin-site-chip-on"
                                                            >
                                                                {id}
                                                                <button
                                                                    type="button"
                                                                    aria-label={`Remove ${id} from ${flag.name}`}
                                                                    className="admin-chip-remove-btn"
                                                                    onClick={() =>
                                                                        handleRemoveSiteFromFlag(
                                                                            flag,
                                                                            id
                                                                        )
                                                                    }
                                                                >
                                                                    ×
                                                                </button>
                                                            </span>
                                                        ))
                                                    )}
                                                </div>

                                                <div className="admin-feature-chips-group">
                                                    <span className="admin-feature-sublabel">
                                                        Disabled Sites (OFF):
                                                    </span>
                                                    {disabledSites.length === 0 ? (
                                                        <span className="admin-feature-none">
                                                            None
                                                        </span>
                                                    ) : (
                                                        disabledSites.map((id) => (
                                                            <span
                                                                key={id}
                                                                className="admin-site-chip admin-site-chip-off"
                                                            >
                                                                {id}
                                                                <button
                                                                    type="button"
                                                                    aria-label={`Remove ${id} from ${flag.name}`}
                                                                    className="admin-chip-remove-btn"
                                                                    onClick={() =>
                                                                        handleRemoveSiteFromFlag(
                                                                            flag,
                                                                            id
                                                                        )
                                                                    }
                                                                >
                                                                    ×
                                                                </button>
                                                            </span>
                                                        ))
                                                    )}
                                                </div>

                                                <form
                                                    className="admin-feature-add-site-row"
                                                    onSubmit={(e) => {
                                                        e.preventDefault();
                                                        handleAddSiteToFlag(flag);
                                                    }}
                                                >
                                                    <Combobox.Root
                                                        multiple
                                                        open={false}
                                                        value={siteInput.pendingSiteIDs}
                                                        onValueChange={(nextPending) =>
                                                            setNewSiteInputs((prev) => {
                                                                const current = prev[flag.name] ?? {
                                                                    siteID: '',
                                                                    pendingSiteIDs: [],
                                                                    value: true,
                                                                };
                                                                return {
                                                                    ...prev,
                                                                    [flag.name]: {
                                                                        ...current,
                                                                        pendingSiteIDs: nextPending,
                                                                    },
                                                                };
                                                            })
                                                        }
                                                        inputValue={siteInput.siteID}
                                                        onInputValueChange={(nextSiteID) =>
                                                            setNewSiteInputs((prev) => {
                                                                const current = prev[flag.name] ?? {
                                                                    siteID: '',
                                                                    pendingSiteIDs: [],
                                                                    value: true,
                                                                };
                                                                return {
                                                                    ...prev,
                                                                    [flag.name]: {
                                                                        ...current,
                                                                        siteID: nextSiteID,
                                                                    },
                                                                };
                                                            })
                                                        }
                                                    >
                                                        <Combobox.Chips className="admin-pill-input-container">
                                                            {siteInput.pendingSiteIDs.map(
                                                                (pendingID) => (
                                                                    <Combobox.Chip
                                                                        key={pendingID}
                                                                        className="admin-site-chip admin-site-chip-pending"
                                                                    >
                                                                        {pendingID}
                                                                        <Combobox.ChipRemove
                                                                            aria-label={`Remove pending ${pendingID}`}
                                                                            className="admin-chip-remove-btn"
                                                                        >
                                                                            ×
                                                                        </Combobox.ChipRemove>
                                                                    </Combobox.Chip>
                                                                )
                                                            )}
                                                            <Combobox.Input
                                                                placeholder={
                                                                    siteInput.pendingSiteIDs
                                                                        .length === 0
                                                                        ? `Add site ID to ${flag.name}...`
                                                                        : 'Add another site ID...'
                                                                }
                                                                aria-label={`Add site ID to ${flag.name}`}
                                                                onKeyDown={(e) => {
                                                                    if (e.key === 'Tab') {
                                                                        const trimmed =
                                                                            siteInput.siteID.trim();
                                                                        if (trimmed) {
                                                                            e.preventDefault();
                                                                            setNewSiteInputs(
                                                                                (prev) => {
                                                                                    const current =
                                                                                        prev[
                                                                                            flag
                                                                                                .name
                                                                                        ] ?? {
                                                                                            siteID: '',
                                                                                            pendingSiteIDs:
                                                                                                [],
                                                                                            value: true,
                                                                                        };
                                                                                    const nextPending =
                                                                                        current.pendingSiteIDs.includes(
                                                                                            trimmed
                                                                                        )
                                                                                            ? current.pendingSiteIDs
                                                                                            : [
                                                                                                  ...current.pendingSiteIDs,
                                                                                                  trimmed,
                                                                                              ];
                                                                                    return {
                                                                                        ...prev,
                                                                                        [flag.name]:
                                                                                            {
                                                                                                ...current,
                                                                                                siteID: '',
                                                                                                pendingSiteIDs:
                                                                                                    nextPending,
                                                                                            },
                                                                                    };
                                                                                }
                                                                            );
                                                                        }
                                                                    }
                                                                }}
                                                                className="admin-pill-inner-input"
                                                            />
                                                        </Combobox.Chips>
                                                    </Combobox.Root>
                                                    <select
                                                        aria-label={`Site override value for ${flag.name}`}
                                                        value={siteInput.value ? 'true' : 'false'}
                                                        onChange={(e) =>
                                                            setNewSiteInputs((prev) => ({
                                                                ...prev,
                                                                [flag.name]: {
                                                                    ...siteInput,
                                                                    value: e.target.value === 'true',
                                                                },
                                                            }))
                                                        }
                                                        className="admin-feature-select"
                                                    >
                                                        <option value="true">ON</option>
                                                        <option value="false">OFF</option>
                                                    </select>
                                                    <button
                                                        type="submit"
                                                        className="btn admin-alias-save-btn"
                                                        disabled={savingFlag === flag.name}
                                                    >
                                                        Add Site
                                                    </button>
                                                </form>
                                            </div>
                                        </div>
                                    </div>
                                );
                            })
                        )}
                    </div>
                </Collapsible.Panel>
            </Collapsible.Root>

            {/* 4. Sites Section (Collapsed by default, lazy-loaded when opened) */}
            <Collapsible.Root
                open={sitesOpen}
                onOpenChange={handleSitesOpenChange}
                className="admin-collapsible-section"
            >
                <div className="admin-section-header-row">
                    <Collapsible.Trigger className="admin-collapsible-trigger">
                        <span className="admin-collapsible-chevron" aria-hidden="true">
                            {sitesOpen ? '▼' : '▶'}
                        </span>
                        <h2>Site List</h2>
                        {sitesLoaded && (
                            <div className="admin-badges">
                                <span className="admin-badge">{sites.length} sites</span>
                            </div>
                        )}
                    </Collapsible.Trigger>
                </div>
                <Collapsible.Panel className="admin-collapsible-panel">
                    {loadingSites ? (
                        <div className="admin-empty">Loading sites...</div>
                    ) : sitesError ? (
                        <div className="admin-error">{sitesError}</div>
                    ) : (
                        <>
                            <form onSubmit={handleSearch} className="admin-search-container">
                                <Autocomplete.Root
                                    openOnInputClick
                                    value={searchQuery}
                                    onValueChange={(val, details) => {
                                        setSearchQuery(val);
                                        if (details.reason === 'item-press') {
                                            setActiveSearchQuery(val);
                                        }
                                    }}
                                    items={sites}
                                    itemToStringValue={(site: AdminSite) => site.id}
                                    filter={(site: AdminSite, query: string) => {
                                        if (!query.trim()) return true;
                                        return (
                                            fuzzyMatch(query, site.id) ||
                                            (site.alias ? fuzzyMatch(query, site.alias) : false)
                                        );
                                    }}
                                >
                                    <div className="admin-autocomplete-wrapper">
                                        <Autocomplete.Input
                                            placeholder="Search site names, IDs or aliases..."
                                            className="admin-search-input"
                                        />
                                    </div>
                                    <Autocomplete.Portal>
                                        <Autocomplete.Positioner className="select-positioner">
                                            <Autocomplete.Popup className="select-popup admin-autocomplete-popup">
                                                <Autocomplete.List>
                                                    {(site: AdminSite) => (
                                                        <Autocomplete.Item
                                                            key={site.id}
                                                            value={site}
                                                            className="select-item admin-autocomplete-item"
                                                            onClick={() => {
                                                                setSearchQuery(site.id);
                                                                setActiveSearchQuery(site.id);
                                                            }}
                                                        >
                                                            <span className="admin-autocomplete-id">
                                                                {site.id}
                                                            </span>
                                                            {site.alias && (
                                                                <span className="admin-autocomplete-alias">
                                                                    ({site.alias})
                                                                </span>
                                                            )}
                                                        </Autocomplete.Item>
                                                    )}
                                                </Autocomplete.List>
                                            </Autocomplete.Popup>
                                        </Autocomplete.Positioner>
                                    </Autocomplete.Portal>
                                </Autocomplete.Root>
                                <button
                                    type="submit"
                                    className="btn admin-search-btn"
                                    disabled={loadingSearch}
                                >
                                    {loadingSearch ? 'Searching...' : 'Search'}
                                </button>
                                {activeSearchQuery && (
                                    <button
                                        type="button"
                                        onClick={handleClearSearch}
                                        className="btn admin-clear-btn"
                                    >
                                        Clear
                                    </button>
                                )}
                            </form>

                            <div className="admin-list">
                                {filteredSites.length === 0 ? (
                                    <div className="admin-empty">
                                        No sites match the search criteria.
                                    </div>
                                ) : (
                                    filteredSites.map((site) => (
                                        <div
                                            key={site.id}
                                            className="card admin-site-card admin-card-col"
                                        >
                                            <div className="admin-site-row">
                                                <div className="admin-site-info">
                                                    <h3 className="admin-site-id">{site.id}</h3>
                                                    {site.lastAction && (
                                                        <div className="admin-site-action">
                                                            Last Action:{' '}
                                                            {site.lastAction.description} @{' '}
                                                            {new Date(
                                                                site.lastAction.timestamp
                                                            ).toLocaleString()}
                                                            <br />
                                                            Battery SOC:{' '}
                                                            {site.lastAction.systemStatus?.batterySOC?.toFixed(
                                                                1
                                                            ) || '0'}
                                                            %
                                                        </div>
                                                    )}
                                                </div>
                                                <a
                                                    href={`/dashboard?viewSite=${site.id}`}
                                                    className="btn admin-primary-btn"
                                                >
                                                    View Dashboard
                                                </a>
                                            </div>

                                            <div className="admin-site-footer">
                                                {editingSiteId === site.id ? (
                                                    <div className="admin-alias-edit-form">
                                                        <input
                                                            type="text"
                                                            value={editingAliasValue}
                                                            onChange={(e) =>
                                                                setEditingAliasValue(e.target.value)
                                                            }
                                                            className="admin-alias-input"
                                                            placeholder="Add site alias..."
                                                            autoFocus
                                                        />
                                                        <button
                                                            onClick={() => handleSaveAlias(site.id)}
                                                            className="btn admin-alias-save-btn"
                                                        >
                                                            Save
                                                        </button>
                                                        <button
                                                            onClick={() => setEditingSiteId(null)}
                                                            className="btn admin-alias-cancel-btn"
                                                        >
                                                            Cancel
                                                        </button>
                                                    </div>
                                                ) : (
                                                    <div className="admin-alias-display">
                                                        <span className="admin-alias-text">
                                                            {site.alias
                                                                ? `Alias: ${site.alias}`
                                                                : 'No alias set'}
                                                        </span>
                                                        <button
                                                            onClick={() => {
                                                                setEditingSiteId(site.id);
                                                                setEditingAliasValue(
                                                                    site.alias || ''
                                                                );
                                                            }}
                                                            className="admin-alias-edit-btn"
                                                            title="Edit Alias"
                                                        >
                                                            ✏️
                                                        </button>
                                                    </div>
                                                )}

                                                {features.length > 0 && (
                                                    <Collapsible.Root className="admin-site-features-collapsible">
                                                        <Collapsible.Trigger className="admin-site-features-trigger">
                                                            <span>Features</span>
                                                        </Collapsible.Trigger>
                                                        <Collapsible.Panel className="admin-site-features-panel">
                                                            {features.map((flag) => {
                                                                const override =
                                                                    getFlagSiteOverride(
                                                                        flag,
                                                                        site.id
                                                                    );
                                                                const effective =
                                                                    override === 'on'
                                                                        ? true
                                                                        : override === 'off'
                                                                          ? false
                                                                          : (site.features?.[
                                                                                flag.name
                                                                            ] ?? flag.defaultValue);
                                                                return (
                                                                    <div
                                                                        key={flag.name}
                                                                        className="admin-site-feature-row"
                                                                    >
                                                                        <div className="admin-site-feature-info">
                                                                            <span className="admin-site-feature-name">
                                                                                {flag.name}
                                                                            </span>
                                                                            <span
                                                                                className={`admin-badge ${
                                                                                    effective
                                                                                        ? 'admin-badge-on'
                                                                                        : 'admin-badge-off'
                                                                                }`}
                                                                            >
                                                                                {effective
                                                                                    ? 'ON'
                                                                                    : 'OFF'}
                                                                            </span>
                                                                        </div>
                                                                        <div className="admin-site-feature-actions">
                                                                            <button
                                                                                type="button"
                                                                                className={`btn admin-override-btn ${
                                                                                    override ===
                                                                                    'default'
                                                                                        ? 'active'
                                                                                        : ''
                                                                                }`}
                                                                                disabled={
                                                                                    savingFlag ===
                                                                                    flag.name
                                                                                }
                                                                                onClick={() =>
                                                                                    handleSiteCardFeatureOverride(
                                                                                        site,
                                                                                        flag,
                                                                                        'default'
                                                                                    )
                                                                                }
                                                                            >
                                                                                Default
                                                                            </button>
                                                                            <button
                                                                                type="button"
                                                                                className={`btn admin-override-btn ${
                                                                                    override ===
                                                                                    'on'
                                                                                        ? 'active'
                                                                                        : ''
                                                                                }`}
                                                                                disabled={
                                                                                    savingFlag ===
                                                                                    flag.name
                                                                                }
                                                                                onClick={() =>
                                                                                    handleSiteCardFeatureOverride(
                                                                                        site,
                                                                                        flag,
                                                                                        'on'
                                                                                    )
                                                                                }
                                                                            >
                                                                                Force ON
                                                                            </button>
                                                                            <button
                                                                                type="button"
                                                                                className={`btn admin-override-btn ${
                                                                                    override ===
                                                                                    'off'
                                                                                        ? 'active'
                                                                                        : ''
                                                                                }`}
                                                                                disabled={
                                                                                    savingFlag ===
                                                                                    flag.name
                                                                                }
                                                                                onClick={() =>
                                                                                    handleSiteCardFeatureOverride(
                                                                                        site,
                                                                                        flag,
                                                                                        'off'
                                                                                    )
                                                                                }
                                                                            >
                                                                                Force OFF
                                                                            </button>
                                                                        </div>
                                                                    </div>
                                                                );
                                                            })}
                                                        </Collapsible.Panel>
                                                    </Collapsible.Root>
                                                )}
                                            </div>
                                        </div>
                                    ))
                                )}
                            </div>
                        </>
                    )}
                </Collapsible.Panel>
            </Collapsible.Root>
        </div>
    );
};

export default AdminPage;
