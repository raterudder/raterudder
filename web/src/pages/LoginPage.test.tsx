import { render, screen } from '@testing-library/react';
import { Router } from 'wouter';
import LoginPage from './LoginPage';
import { describe, it, expect, vi } from 'vitest';

// Mock GoogleLogin
vi.mock('@react-oauth/google', () => ({
    GoogleOAuthProvider: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    GoogleLogin: () => <button>Mock Google Login</button>,
}));

describe('LoginPage Component', () => {
    it('renders correctly', () => {
        render(<Router><LoginPage onLoginSuccess={vi.fn()} onLoginError={vi.fn()} authEnabled={true} clientIDs={{ google: "test-id" }} /></Router>);

        expect(screen.getByText('RateRudder')).toBeInTheDocument();
        expect(screen.getByText('Log in or sign up to manage your home energy.')).toBeInTheDocument();
        expect(screen.getByText('Mock Google Login')).toBeInTheDocument();
    });

    it('renders disabled message when auth is disabled', () => {
        render(<Router><LoginPage onLoginSuccess={vi.fn()} onLoginError={vi.fn()} authEnabled={false} clientIDs={{}} /></Router>);
        expect(screen.getByText('Authentication is currently disabled.')).toBeInTheDocument();
    });
});
