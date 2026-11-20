import { useEffect, useState } from 'react';

const TeslaCallback = () => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get('code');
    const state = params.get('state');
    const error = params.get('error');
    const errorDescription = params.get('error_description');

    // Calculate initial status
    let initialStatus = 'Processing authentication...';
    if (!window.opener) {
        initialStatus = error
            ? 'Authorization was not completed. Please return to the settings page and try again.'
            : !code
                ? 'Error: No authorization code received. Please return to the settings page and try again.'
                : 'Error: Could not find parent window. Please return to the settings page and try again.';
    } else if (error || !code) {
        initialStatus = 'Authorization was not completed. Closing window...';
    }

    const [status, setStatus] = useState(initialStatus);

    useEffect(() => {
        if (!window.opener) return;

        let timeoutId: ReturnType<typeof setTimeout> | undefined;
        if (!error && code) {
            try {
                window.opener.postMessage(
                    { type: 'OAUTH_CODE', code, state },
                    window.location.origin
                );
                // Use setTimeout to move the status update after the main effect execution,
                // avoiding the "synchronous setState in effect" lint error.
                timeoutId = setTimeout(() => {
                    setStatus('Success! You can close this window.');
                    window.close();
                }, 500);
            } catch (err) {
                timeoutId = setTimeout(() => {
                    setStatus('An error occurred while processing the callback.');
                    console.error(err);
                }, 0);
            }
        } else {
            try {
                window.opener.postMessage(
                    {
                        type: 'OAUTH_ERROR',
                        error: error || 'missing_code',
                        errorDescription: errorDescription || undefined,
                        state,
                    },
                    window.location.origin
                );
                timeoutId = setTimeout(() => {
                    setStatus('Authorization cancelled. You can close this window.');
                    window.close();
                }, 300);
            } catch (err) {
                timeoutId = setTimeout(() => {
                    setStatus('An error occurred while processing the callback.');
                    console.error(err);
                }, 0);
            }
        }

        return () => {
            if (timeoutId !== undefined) {
                clearTimeout(timeoutId);
            }
        };
    }, [code, state, error, errorDescription]);

    return (
        <div style={{
            display: 'flex',
            justifyContent: 'center',
            alignItems: 'center',
            height: '100vh',
            fontFamily: 'sans-serif',
            color: '#333',
            background: '#fcfcfc',
            textAlign: 'center',
            padding: '20px'
        }}>
            <h2>{status}</h2>
        </div>
    );
};

export default TeslaCallback;
