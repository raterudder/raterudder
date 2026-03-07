import React from 'react';
import { GoogleLogin, GoogleOAuthProvider } from '@react-oauth/google';
import AppleSignin from 'react-apple-signin-auth';
import { Link } from 'wouter';
import { Separator } from '@base-ui/react/separator';
import './LoginPage.css';

interface LoginPageProps {
    onLoginSuccess: (credentialResponse: any, client?: string) => void;
    onLoginError?: () => void;
    authEnabled: boolean;
    clientIDs: Record<string, string>;
}

const LoginPage: React.FC<LoginPageProps> = ({ onLoginSuccess, onLoginError, authEnabled, clientIDs }) => {
    const handleAppleSuccess = (response: any) => {
        if (response.authorization && response.authorization.id_token) {
            onLoginSuccess({ credential: response.authorization.id_token }, 'apple');
        } else {
            console.error("Apple login failed: ", response);
            if (onLoginError) onLoginError();
        }
    };

    return (
        <div className="auth-page">
            <div className="auth-card">
                <h1>RateRudder</h1>
                <p>Log in or sign up to manage your home energy.</p>

                <div className="google-btn-wrapper">
                    {authEnabled && (clientIDs["google"] || clientIDs["apple"]) ? (
                        <>
                            {clientIDs["google"] && (
                                <GoogleOAuthProvider clientId={clientIDs["google"]}>
                                    <GoogleLogin
                                        onSuccess={(res) => onLoginSuccess(res, 'google')}
                                        onError={onLoginError}
                                        theme="filled_blue"
                                        size="large"
                                        text="signin_with"
                                        shape="pill"
                                        width="250"
                                        auto_select
                                        use_fedcm_for_prompt
                                        use_fedcm_for_button
                                    />
                                </GoogleOAuthProvider>
                            )}
                            {clientIDs["apple"] && (
                                    <AppleSignin
                                        authOptions={{
                                            clientId: clientIDs["apple"],
                                            scope: 'email',
                                            redirectURI: `${window.location.origin}/login`,
                                            state: 'state',
                                            nonce: 'nonce',
                                            usePopup: true
                                        }}
                                        uiType="dark"
                                        className="apple-auth-btn"
                                        noDefaultStyle={false}
                                        buttonExtraChildren="Sign in with Apple"
                                        onSuccess={handleAppleSuccess}
                                        onError={onLoginError || (() => {})}
                                    />
                            )}
                        </>
                    ) : (
                        <div className="auth-disabled-message">
                            {!authEnabled ? "Authentication is currently disabled." : "No login providers are configured correctly."}
                        </div>
                    )}
                </div>
                <p className="beta-notice" style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', marginTop: '0', marginBottom: '1.5rem', lineHeight: '1.5' }}>
                    During our limited beta, we currently only support Ameren and ComEd utility providers, and FranklinWH battery systems.
                </p>
                <div className="login-footer">
                    <Link to="/privacy">Privacy Policy</Link>
                    <Separator className="separator" />
                    <Link to="/terms">Terms of Service</Link>
                </div>
            </div>
        </div>
    );
};

export default LoginPage;
