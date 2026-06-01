import React, { useState, useEffect } from 'react';
import { useLocation } from 'wouter';
import { fetchUtilities, fetchESSList, type UtilityProviderInfo, type ESSProviderInfo } from '../api';
import { InterestForm } from '../components/InterestForm';
import './LoginPage.css';

const BetaInterstitialPage: React.FC = () => {
    const [, navigate] = useLocation();
    const [utilitiesList, setUtilitiesList] = useState<UtilityProviderInfo[]>([]);
    const [essList, setEssList] = useState<ESSProviderInfo[]>([]);
    const [isLoadingData, setIsLoadingData] = useState(true);

    useEffect(() => {
        const load = async () => {
            try {
                const [utils, ess] = await Promise.all([
                    fetchUtilities(),
                    fetchESSList()
                ]);
                setUtilitiesList(utils);
                setEssList(ess);
            } catch (err) {
                console.error("Failed to load options", err);
            } finally {
                setIsLoadingData(false);
            }
        };
        load();
    }, []);

    const handleContinue = () => {
        navigate('/new-site');
    };

    return (
        <div className="auth-page">
            <div className="auth-card" style={{ maxWidth: '440px' }}>
                <h1 className="beta-interstitial-title">RateRudder Beta</h1>
                {isLoadingData ? (
                    <div className="loading-screen" style={{ minHeight: '200px' }}>
                        <span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>
                        Loading options...
                    </div>
                ) : (
                    <InterestForm
                        utilitiesList={utilitiesList}
                        essList={essList}
                        hideBattery={false}
                        onContinue={handleContinue}
                        header={
                            <p className="beta-interstitial-desc">
                                To get started, please confirm your equipment and utility provider.
                            </p>
                        }
                    />
                )}
            </div>
        </div>
    );
};

export default BetaInterstitialPage;
