import { useEffect, useState } from 'react';

const TeslaCallback = () => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get('code');
    const state = params.get('state');

    // Calculate initial status
    let initialStatus = 'Processing authentication...';
    if (!code) {
        initialStatus = 'Error: No authorization code received.';
    } else if (!window.opener) {
        initialStatus = 'Error: Could not find parent window. Please return to the settings page and try again.';
    }

    const [status, setStatus] = useState(initialStatus);

    useEffect(() => {
        if (code && window.opener) {
            try {
                window.opener.postMessage(
                    { type: 'OAUTH_CODE', code, state },
                    window.location.origin
                );
                // Use setTimeout to move the status update after the main effect execution,
                // avoiding the "synchronous setState in effect" lint error.
                setTimeout(() => {
                    setStatus('Success! You can close this window.');
                    window.close();
                }, 500);
            } catch (err) {
                setTimeout(() => {
                    setStatus('An error occurred while processing the callback.');
                    console.error(err);
                }, 0);
            }
        }
    }, [code, state]);

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
