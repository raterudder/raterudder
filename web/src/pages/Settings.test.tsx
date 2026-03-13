import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import App from '../App';
import * as api from '../api';
import { setupDefaultApiMocks, defaultAuthStatus, defaultSettings } from '../test/apiMocks';

const { fetchAuthStatus, fetchSettings, updateSettings, login, logout } = api;

vi.mock('../api');

// Mock Google OAuth
vi.mock('@react-oauth/google', () => ({
    GoogleOAuthProvider: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    GoogleLogin: ({ onSuccess }: { onSuccess: (res: any) => void }) => (
        <button onClick={() => onSuccess({ credential: 'test-token' })}>
            Google Sign In
        </button>
    ),
}));

// Helper to navigate to settings page
const navigateToSettings = async () => {
    (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });
    render(<App />);
    fireEvent.click(screen.getByText(/Log In/));
    await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
    await screen.findByRole('heading', { name: /Settings/i });
};

describe('App & Settings', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('shows login button when auth required and not logged in', async () => {
        (fetchAuthStatus as any).mockResolvedValue({
            ...defaultAuthStatus,
            loggedIn: false
        });

        render(<App />);

        // On LandingPage, click Login link in header
        fireEvent.click(screen.getByText(/Log In/));

        await waitFor(() => {
            expect(screen.getByText('Google Sign In')).toBeInTheDocument();
        });
    });

    it('calls login api on successful google login', async () => {
         (fetchAuthStatus as any).mockResolvedValueOnce({
            ...defaultAuthStatus,
            loggedIn: false
        }).mockResolvedValueOnce({
            ...defaultAuthStatus,
            loggedIn: true
        });

        (login as any).mockResolvedValue(undefined);

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));

        await waitFor(() => {
            expect(screen.getByText('Google Sign In')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Google Sign In'));

        await waitFor(() => {
            expect(login).toHaveBeenCalledWith('test-token', 'google');
        });
    });

    it('shows logout button when logged in and calls logout on click', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });
        (logout as any).mockResolvedValue(undefined);

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));

        await waitFor(() => {
            expect(screen.getByText('Log Out')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Log Out'));

        await waitFor(() => {
            expect(logout).toHaveBeenCalled();
        });
    });

    it('shows settings link when logged in', async () => {
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));

        await waitFor(() => {
            expect(screen.getByText('Settings')).toBeInTheDocument();
        });
    });

    it('navigates to settings and loads data', async () => {
        await navigateToSettings();

        await waitFor(() => {
            expect(screen.getByLabelText(/Minimum Battery %/i)).toBeInTheDocument();
            expect(screen.getByDisplayValue('10')).toBeInTheDocument();
        });
    });

    it('can update settings', async () => {
         await navigateToSettings();

         // Change input
         const input = await screen.findByLabelText(/Minimum Battery %/i);
         fireEvent.change(input, { target: { value: '20' } });

         // Mock update success
         (updateSettings as any).mockResolvedValue(undefined);

         // Helper to click save
         const saveBtn = screen.getByText('Save Settings');
         fireEvent.click(saveBtn);

         await waitFor(() => {
             expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 minBatterySOC: 20
             }), expect.any(String), undefined);
         });
    });

    it('can toggle pause setting', async () => {
         await navigateToSettings();

         // Toggle Pause switch
         const switchEl = await screen.findByRole('switch', { name: /Pause Automation/i });
         fireEvent.click(switchEl);

         // Mock update success
         (updateSettings as any).mockResolvedValue(undefined);

         // Helper to click save
         const saveBtn = screen.getByText('Save Settings');
         fireEvent.click(saveBtn);

         await waitFor(() => {
             expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 pause: true
             }), expect.any(String), undefined);
         });
    });

    it('can toggle grid strategy settings', async () => {
         await navigateToSettings();

         // Toggle Export Battery switch
         const switchEl = await screen.findByRole('switch', { name: /Export Battery to Grid/i });
         fireEvent.click(switchEl);

         // Mock update success
         (updateSettings as any).mockResolvedValue(undefined);

         // Helper to click save
         const saveBtn = screen.getByText('Save Settings');
         fireEvent.click(saveBtn);

         await waitFor(() => {
             expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 gridExportBatteries: true
             }), expect.any(String), undefined);
         });
    });

    it('renders solar settings inputs on settings page', async () => {
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Advanced Tuning Settings');
        fireEvent.click(advancedBtn);

        await waitFor(() => {
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
            expect(screen.getByDisplayValue('3')).toBeInTheDocument();
            expect(screen.getByDisplayValue('1')).toBeInTheDocument();
        });
    });

    it('can update solar bell curve multiplier', async () => {
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Advanced Tuning Settings');
        fireEvent.click(advancedBtn);

        await waitFor(() => expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument());
        const input = screen.getByLabelText(/Solar Bell Curve Multiplier/i);
        fireEvent.change(input, { target: { value: '0.5' } });

        (updateSettings as any).mockResolvedValue(undefined);
        fireEvent.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                solarBellCurveMultiplier: 0.5
            }), expect.any(String), undefined);
        });
    });

    it('shows location settings only when release is staging', async () => {
        const stagingSettings = { release: 'staging', minArbitrageDifferenceDollarsPerKWH: 0.05, minBatterySOC: 20, minLoadForSolarHedgeKWH: 2.0, ess: 'mock', hasCredentials: { mock: true } };
        (fetchSettings as any).mockResolvedValue(stagingSettings);
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));
        await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
        fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
        await screen.findByRole('heading', { name: /Settings/i });

        await waitFor(() => {
            expect(screen.getByText('Location')).toBeInTheDocument();
            expect(screen.getByLabelText(/Zip\/Postal Code/i)).toBeInTheDocument();
        });

        const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
        fireEvent.change(zipInput, { target: { value: '90210' } });

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({ postalCode: '90210' }), expect.any(String), undefined);
        });
    });

    it('hides location settings when release is not staging', async () => {
        const prodSettings = { release: 'production', minArbitrageDifferenceDollarsPerKWH: 0.05, minBatterySOC: 20, minLoadForSolarHedgeKWH: 2.0, ess: 'mock', hasCredentials: { mock: true } };
        (fetchSettings as any).mockResolvedValue(prodSettings);
        (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

        render(<App />);
        fireEvent.click(screen.getByText(/Log In/));
        await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
        fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
        await screen.findByRole('heading', { name: /Settings/i });

        await waitFor(() => {
            expect(screen.queryByText('Location')).not.toBeInTheDocument();
            expect(screen.queryByLabelText(/Zip\/Postal Code/i)).not.toBeInTheDocument();
        });
    });

    it('can update ComEd rate options', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'comed',
            utilityRate: 'comed_besh',
            utilityRateOptions: {}
        });
        await navigateToSettings();

        // Wait for Utility Options section
        await screen.findByText('Configured');
        fireEvent.click(screen.getByText('Change'));

        await waitFor(() => expect(screen.getByRole('switch', { name: /Delivery Time-of-Day/i })).toBeInTheDocument());

        // Toggle Delivery Time-of-Day switch
        const dtodSwitch = screen.getByRole('switch', { name: /Delivery Time-of-Day/i });
        fireEvent.click(dtodSwitch);

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                utilityRateOptions: expect.objectContaining({
                    rateClass: "singleFamilyWithoutElectricHeat",
                    variableDeliveryRate: true
                })
            }), expect.any(String), undefined);
        });
    });

    it('can update price threshold fields and shows warning appropriately', async () => {
        await navigateToSettings();

        // Expand advanced tuning settings to see the price threshold
        const advancedBtn = await screen.findByText('Advanced Tuning Settings');
        fireEvent.click(advancedBtn);

        // Check fields are accessible
        const priceInput = await screen.findByLabelText(/Always Charge Below/i);

        // At initial value, warning should not be shown
        expect(screen.queryByText(/Are you sure you want to force charging the batteries from the grid when it's below this price/i)).not.toBeInTheDocument();

        fireEvent.change(priceInput, { target: { value: '0.10' } });

        // Warning should be shown now that value > 0.05
        await waitFor(() => {
            expect(screen.getByText(/Are you sure you want to force charging the batteries from the grid when it's below this price/i)).toBeInTheDocument();
        });

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                alwaysChargeUnderDollarsPerKWH: 0.10
            }), expect.any(String), undefined);
        });
    });

    it('can select utility provider and then rate', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '',
            utilityRate: '',
            utilityRateOptions: {}
        });
        await navigateToSettings();

        // Select Service (Provider)
        const serviceSelect = await screen.findByLabelText(/Service/i);
        await user.click(serviceSelect);
        const comedOption = await screen.findByRole('option', { name: 'ComEd' });
        await user.click(comedOption);

        // Rate/Plan should be auto-selected since ComEd only has one in the mock
        await screen.findByText(/Hourly Pricing Program/i);

        // Verify options appear - wait for the label to be stable
        const switchEl = await screen.findByRole('switch', { name: /Delivery Time-of-Day/i });
        expect(switchEl).toBeInTheDocument();

        // Save and verify
        fireEvent.click(screen.getByText('Save Settings'));
        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                utilityRateOptions: expect.objectContaining({
                    rateClass: "singleFamilyWithoutElectricHeat",
                    variableDeliveryRate: false
                })
            }), expect.any(String), undefined);
        });
    });

    it('can submit ESS credentials and passes raw password', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'franklin',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select ESS
        const essSelect = await screen.findByLabelText(/ESS/i);
        await user.click(essSelect);
        const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        // Fill in credentials based on apiMocks
        const emailInput = await screen.findByLabelText('Email');
        await user.type(emailInput, 'user@example.com');

        const passInput = await screen.findByLabelText(/Password/i, { selector: 'input[type="password"]' });
        await user.type(passInput, 'myrawpassword');

        // Target Gateway ID is optional, we skip it

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();

            // Should pass the credentials untouched
            expect(updateSettings).toHaveBeenCalledWith(expect.anything(), expect.any(String), {
                franklin: {
                    username: 'user@example.com',
                    password: 'myrawpassword'
                }
            });
        });
    });

    it('hides providers with hidden true unless already selected', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '', // Not secret utility
            ess: '', // Not secret ess
        });

        await navigateToSettings();

        // Check utility
        const serviceSelect = await screen.findByLabelText(/Service/i);
        await user.click(serviceSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'ComEd' })).toBeInTheDocument());
        expect(screen.queryByRole('option', { name: 'Secret Utility' })).not.toBeInTheDocument();
        // Try to close dropdown by clicking document body
        fireEvent.pointerDown(document.body);

        // Check ESS
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'FranklinWH' })).toBeInTheDocument());
        expect(screen.queryByRole('option', { name: 'Secret ESS' })).not.toBeInTheDocument();
    });

    it('shows hidden providers if they are currently configured', async () => {
         const user = userEvent.setup();
         (fetchSettings as any).mockResolvedValue({
             ...defaultSettings,
             utilityProvider: 'hidden_utility',
             utilityRate: 'hidden_rate',
             ess: 'hidden_ess',
         });

         await navigateToSettings();

         // The configured summary should show the hidden provider's name
         await waitFor(() => expect(screen.getByText("Secret Utility")).toBeInTheDocument());
         expect(screen.getByText("Secret ESS")).toBeInTheDocument();

         // In edit mode (change), the option should also be visible in the dropdown
         const utilityChangeBtn = screen.getByText('Change');
         fireEvent.click(utilityChangeBtn); // click Utility Service "Change"

         const serviceSelect = await screen.findByLabelText(/Service/i);
         await user.click(serviceSelect);
         await waitFor(() => expect(screen.getByRole('option', { name: 'Secret Utility' })).toBeInTheDocument());
    });

    it('shows edit ess if they are set without any credentials', async () => {
         const user = userEvent.setup();
         (fetchSettings as any).mockResolvedValue({
             ...defaultSettings,
             ess: 'missing_ess',
             hasCredentials: {
                 missing_ess: false,
             }
         });

         await navigateToSettings();

         // The select should show the ess's name
         await waitFor(() => expect(screen.getByText("Missing ESS")).toBeInTheDocument());

         const usernameInput = await screen.findByLabelText('Username');
         expect(usernameInput).toBeInTheDocument();
         await user.type(usernameInput, 'username');

         const doneBtn = await screen.findByRole('button', { name: 'Done' });
         expect(doneBtn).toBeInTheDocument();
         await user.click(doneBtn);

         await waitFor(() => expect(screen.getByText("Pending Save")).toBeInTheDocument());
    });

    it('handles oAuthKey dropdown and routes to correct oAuthURL', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select Multi Region ESS
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        const multiRegionOption = await screen.findByRole('option', { name: 'Multi Region ESS' });
        await user.click(multiRegionOption);

        // Verify Region select is shown and defaults to North America
        const regionSelect = await screen.findByLabelText(/Region/i);
        expect(screen.getByText('North America')).toBeInTheDocument();

        // Verify Login button works and goes to NA url
        const windowOpenSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        fireEvent.click(loginBtn);
        expect(windowOpenSpy).toHaveBeenCalledWith('https://na.example.com/?state=site1', 'OAuthLogin', expect.any(String));

        // Change select to EU
        await user.click(regionSelect);
        const euOption = await screen.findByRole('option', { name: 'Europe' });
        await user.click(euOption);

        // Fill in required authCode based on apiMocks
        const passInput = await screen.findByLabelText(/Authorization Code/i, { selector: 'input[type="password"]' });
        await user.type(passInput, 'testauthcode');

        // Save
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.anything(), expect.any(String), {
                multi_region_ess: {
                    region: 'EU',
                    authCode: 'testauthcode'
                }
            });
        });
        windowOpenSpy.mockRestore();
    });

    it('shows error when required ESS credential field is missing', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: '',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select ESS
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        // Click Save without filling anything
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('The Email field is required.')).toBeInTheDocument();
        });

        // Fill Email, still missing Password
        const emailInput = await screen.findByLabelText('Email');
        await user.type(emailInput, 'user@example.com');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('The Password field is required.')).toBeInTheDocument();
        });
    });
});
