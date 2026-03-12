import { useEffect, useState } from 'react';

const TeslaCallback = () => {
    const [status, setStatus] = useState('Processing authentication...');

    useEffect(() => {
        try {
            const params = new URLSearchParams(window.location.search);
            const code = params.get('code');
            const state = params.get('state');

            if (code) {
                if (window.opener) {
                    window.opener.postMessage(
                        { type: 'OAUTH_CODE', code, state },
                        window.location.origin
                    );
                    setStatus('Success! You can close this window.');
                    setTimeout(() => {
                        window.close();
                    }, 500);
                } else {
                    setStatus('Error: Could not find parent window. Please return to the settings page and try again.');
                }
            } else {
                setStatus('Error: No authorization code received.');
            }
        } catch (err) {
            setStatus('An error occurred while processing the callback.');
            console.error(err);
        }
    }, []);

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
