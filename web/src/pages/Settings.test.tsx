import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import App from '../App';
import * as api from '../api';
import { setupDefaultApiMocks, defaultAuthStatus, defaultSettings, defaultESSProviders } from '../test/apiMocks';

const { fetchAuthStatus, fetchSettings, updateSettings, login, logout, deleteSite, deleteUser } = api;

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
const navigateToSettings = async (authStatus = defaultAuthStatus) => {
    window.history.pushState(null, '', '/');
    (fetchAuthStatus as any).mockResolvedValue({ ...authStatus });
    render(<App />);
    fireEvent.click(screen.getByText(/Log In/));
    await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

    // If onboarding wizard renders, skip it so the standard settings inputs are available
    await waitFor(() => {
        const hasSettingsHeading = screen.queryByRole('heading', { name: /^Settings$/i });
        const skipBtn = screen.queryByRole('button', { name: /Skip/i });
        expect(hasSettingsHeading || skipBtn).toBeTruthy();
    });

    const skipBtn = screen.queryByRole('button', { name: /Skip/i });
    if (skipBtn) {
        fireEvent.click(skipBtn);
    }

    await screen.findByRole('heading', { name: /^Settings$/i });
};

describe('App & Settings', () => {
    beforeEach(() => {
        vi.unstubAllGlobals();
        window.history.pushState(null, '', '/');
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
                 minBatterySOC: 20,
                 release: "production"
             }), expect.any(String), undefined);
         });
    });

    it('can toggle pause setting', async () => {
         await navigateToSettings();
         (updateSettings as any).mockResolvedValue(undefined);

         const pauseBtn = await screen.findByRole('button', { name: /^Pause$/i });
         fireEvent.click(pauseBtn);

         await waitFor(() => {
             expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                 pause: true
             }), expect.any(String));
         });
    });

    it('can toggle grid strategy settings', async () => {
         await navigateToSettings();

         // Click Update on ESS to reveal grid strategy switches
         const updateBtn = await screen.findByRole('button', { name: /Update Energy Storage System/i });
         fireEvent.click(updateBtn);

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
                 gridExportBatteries: true,
                 customGridSettings: true
             }), expect.any(String), undefined);
         });
    });

    it('renders solar settings inputs on settings page', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            postalCode: ''
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        await waitFor(() => {
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
            expect(screen.getByDisplayValue('3')).toBeInTheDocument();
            expect(screen.getByDisplayValue('1')).toBeInTheDocument();
        });
    });

    it('can update solar bell curve multiplier', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            postalCode: ''
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
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

    it('can update home load prediction strategy', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            homeLoadPredictionStrategy: 'default'
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        // Find strategy select trigger by label
        const strategySelect = await screen.findByRole('combobox', { name: /Home Load Prediction Strategy/i });

        // Caution message should not be visible initially
        expect(screen.queryByTestId('conservative-strategy-warning')).not.toBeInTheDocument();

        // Open select and choose Conservative
        await user.click(strategySelect);
        const conservativeOption = await screen.findByRole('option', { name: 'Conservative (High Protection)' });
        await user.click(conservativeOption);

        // Caution message should show now
        expect(screen.getByTestId('conservative-strategy-warning')).toBeInTheDocument();

        // Save
        (updateSettings as any).mockResolvedValue(undefined);
        fireEvent.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                homeLoadPredictionStrategy: 'conservative'
            }), expect.any(String), undefined);
        });
    });

    it('hides solar trend ratio max and solar bell curve multiplier when zip code is entered', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            postalCode: ''
        });
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        // They should be visible initially because zip code is empty
        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Advanced Solar Settings/i })).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
        });

        // Now enter a zip code in the Zip/Postal Code field
        const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
        fireEvent.change(zipInput, { target: { value: '90210' } });

        // They should be hidden now along with the section header (when net metering is inactive)
        await waitFor(() => {
            expect(screen.queryByRole('heading', { name: /Advanced Solar Settings/i })).not.toBeInTheDocument();
            expect(screen.queryByLabelText(/Solar Trend Ratio Max/i)).not.toBeInTheDocument();
            expect(screen.queryByLabelText(/Solar Bell Curve Multiplier/i)).not.toBeInTheDocument();
        });

        // Now clear the zip code
        fireEvent.change(zipInput, { target: { value: '' } });

        // They should show up again
        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Advanced Solar Settings/i })).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Trend Ratio Max/i)).toBeInTheDocument();
            expect(screen.getByLabelText(/Solar Bell Curve Multiplier/i)).toBeInTheDocument();
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
        await screen.findByRole('heading', { name: /^Settings$/i });

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
        fireEvent.click(screen.getByRole('button', { name: 'Change Utility Service' }));

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

    it('can update battery export margin field when grid export batteries is enabled', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            gridExportBatteries: true,
            minBatteryExportDifferenceDollarsPerKWH: 0.07
        });
        await navigateToSettings();

        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        const exportMarginInput = await screen.findByLabelText(/Battery Export Margin/i);
        expect(exportMarginInput).toBeInTheDocument();
        expect(exportMarginInput).toHaveValue(0.07);

        fireEvent.change(exportMarginInput, { target: { value: '0.12' } });

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                minBatteryExportDifferenceDollarsPerKWH: 0.12
            }), expect.any(String), undefined);
        });
    });

    it('hides battery export margin field when grid export batteries is disabled', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            gridExportBatteries: false
        });
        await navigateToSettings();

        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        expect(screen.queryByLabelText(/Battery Export Margin/i)).not.toBeInTheDocument();
    });

    it('shows solar net metering credits value setting when netMeteringScheme is net', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'srp',
            utilityRate: 'srp_e27',
            utilityRateOptions: {
                netMeteringScheme: 'net'
            }
        });
        await navigateToSettings();

        const advancedBtn = await screen.findByText('Show Advanced Settings');
        fireEvent.click(advancedBtn);

        const nmSelect = await screen.findByLabelText(/Solar Net Metering Credits Value/i);
        expect(nmSelect).toBeInTheDocument();
        await user.click(nmSelect);

        const highestOption = await screen.findByRole('option', { name: 'Highest Price' });
        await user.click(highestOption);

        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                solarNetMeteringCreditsValue: 'highest'
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
        const serviceSelect = await screen.findByRole('combobox', { name: /Utility Provider/i });
        await user.click(serviceSelect);
        const comedOption = await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' });
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

    it('displays ratesNotice when utility provider has a notice configured', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '',
            utilityRate: '',
            utilityRateOptions: {}
        });
        await navigateToSettings();

        const serviceSelect = await screen.findByRole('combobox', { name: /Utility Provider/i });
        await user.click(serviceSelect);
        const octopusOption = await screen.findByRole('option', { name: 'Octopus Energy (UK)' });
        await user.click(octopusOption);

        expect(await screen.findByTestId('utility-rates-notice')).toHaveTextContent('Intelligent Octopus Flux is not supported at this time.');
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
        const serviceSelect = await screen.findByRole('combobox', { name: /Utility Provider/i });
        await user.click(serviceSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'Commonwealth Edison (ComEd)' })).toBeInTheDocument());
        expect(screen.queryByRole('option', { name: 'Secret Utility' })).not.toBeInTheDocument();
        // Try to close dropdown by clicking document body
        fireEvent.pointerDown(document.body);

        // Check ESS
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'FranklinWH' })).toBeInTheDocument());
        expect(screen.queryByRole('option', { name: 'Secret ESS' })).not.toBeInTheDocument();
    });

    it('shows hidden ESS providers if present in enabled localStorage', async () => {
        const user = userEvent.setup();
        localStorage.setItem('enabled', 'hidden_ess');
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '',
            ess: '',
        });

        await navigateToSettings();

        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        await waitFor(() => expect(screen.getByRole('option', { name: 'FranklinWH' })).toBeInTheDocument());
        expect(screen.getByRole('option', { name: 'Secret ESS' })).toBeInTheDocument();
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
         const utilityChangeBtn = await screen.findByRole('button', { name: 'Edit Utility Service' });
         await user.click(utilityChangeBtn); // click Utility Service "Change"

         const serviceSelect = await screen.findByRole('combobox', { name: /Utility Provider/i });
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

         const doneBtn = await screen.findByRole('button', { name: 'Finish editing Energy Storage System' });
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
            expect(screen.getAllByText('The Email field is required.').length).toBeGreaterThan(0);
        });

        // Fill Email, still missing Password
        const emailInput = await screen.findByLabelText('Email');
        await user.type(emailInput, 'user@example.com');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getAllByText('The Password field is required.').length).toBeGreaterThan(0);
        });
    });

    it('hides options that have hidden true', async () => {
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'test_provider',
            utilityRate: 'test_rate',
            utilityRateOptions: {}
        });
        (api.fetchUtilities as any).mockResolvedValue([
            {
                id: 'test_provider',
                name: 'Test Provider',
                rates: [
                    {
                        id: 'test_rate',
                        name: 'Test Rate',
                        options: [
                            {
                                field: 'visibleOption',
                                name: 'Visible Option',
                                type: 'switch',
                                default: true
                            },
                            {
                                field: 'hiddenOption',
                                name: 'Hidden Option',
                                type: 'switch',
                                default: true,
                                hidden: true
                            }
                        ]
                    }
                ]
            }
        ] as any);

        await navigateToSettings();

        // Check that the configured rates view shows up
        await screen.findByText('Configured');
        fireEvent.click(screen.getByRole('button', { name: 'Change Utility Service' }));

        // Check that visible option is rendered but hidden option is not
        await waitFor(() => expect(screen.getByRole('switch', { name: /Visible Option/i })).toBeInTheDocument());
        expect(screen.queryByRole('switch', { name: /Hidden Option/i })).not.toBeInTheDocument();
    });

    it('shows and hides options based on dependsOn conditions', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'sdge',
            utilityRate: 'sdge_tou_dr1',
            utilityRateOptions: {}
        });
        (api.fetchUtilities as any).mockResolvedValue([
            {
                id: 'sdge',
                name: 'San Diego Gas & Electric (SDG&E)',
                rates: [
                    {
                        id: 'sdge_tou_dr1',
                        name: 'Schedule TOU-DR1 (Standard Residential TOU)',
                        options: [
                            {
                                field: 'generationRate',
                                name: 'Generation Rate',
                                type: 'select',
                                choices: [
                                    { value: 'sdge', name: 'SDG&E Bundled' },
                                    { value: 'sdcp_power_on', name: 'SDCP PowerOn' },
                                    { value: 'sdcp_power_100', name: 'SDCP Power100' },
                                ],
                                default: 'sdcp_power_on',
                            },
                            {
                                field: 'location',
                                name: 'Location / Community',
                                type: 'select',
                                choices: [
                                    { value: 'san_diego', name: 'Chula Vista, Encinitas, Imperial Beach, La Mesa and San Diego' },
                                    { value: 'unincorporated', name: 'National City and Unincorporated County of San Diego' },
                                ],
                                default: 'san_diego',
                                dependsOn: {
                                    generationRate: ['sdcp_power_on', 'sdcp_power_100'],
                                },
                            },
                            {
                                field: 'sdcpUnincorporatedNote',
                                name: 'Unincorporated Note',
                                type: 'switch',
                                default: false,
                                dependsOn: {
                                    generationRate: ['sdcp_power_on', 'sdcp_power_100'],
                                    location: ['unincorporated'],
                                },
                            },
                        ],
                    },
                ],
            },
        ] as any);

        await navigateToSettings();

        await screen.findByText('Configured');
        fireEvent.click(screen.getByRole('button', { name: 'Edit Utility Service' }));

        // Default generationRate is sdcp_power_on, so Location / Community should be visible
        // Multi-key dependsOn should remain hidden until location is also 'unincorporated'
        const genSelect = await screen.findByLabelText('Generation Rate');
        expect(screen.getByLabelText('Location / Community')).toBeInTheDocument();
        expect(screen.queryByRole('switch', { name: 'Unincorporated Note' })).not.toBeInTheDocument();

        // Select 'unincorporated' -> both keys in dependsOn match, so Unincorporated Note appears
        await user.click(screen.getByLabelText('Location / Community'));
        await user.click(await screen.findByRole('option', { name: 'National City and Unincorporated County of San Diego' }));
        await waitFor(() => {
            expect(screen.getByRole('switch', { name: 'Unincorporated Note' })).toBeInTheDocument();
        });

        // Switch to SDG&E Bundled -> Location / Community and Unincorporated Note should both be hidden
        await user.click(genSelect);
        await user.click(await screen.findByRole('option', { name: 'SDG&E Bundled' }));

        await waitFor(() => {
            expect(screen.queryByLabelText('Location / Community')).not.toBeInTheDocument();
            expect(screen.queryByRole('switch', { name: 'Unincorporated Note' })).not.toBeInTheDocument();
        });

        // Switch back to SDCP Power100 -> both Location / Community and Unincorporated Note should reappear
        await user.click(genSelect);
        await user.click(await screen.findByRole('option', { name: 'SDCP Power100' }));

        await waitFor(() => {
            expect(screen.getByLabelText('Location / Community')).toBeInTheDocument();
            expect(screen.getByRole('switch', { name: 'Unincorporated Note' })).toBeInTheDocument();
        });
    });

    it('can select SCE utility and net metering options', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: '',
            utilityRate: '',
            utilityRateOptions: {}
        });
        (api.fetchUtilities as any).mockResolvedValue([
            {
                id: 'sce',
                name: 'Southern California Edison',
                rates: [
                    {
                        id: 'sce_tou_d_prime',
                        name: 'TOU-D-PRIME',
                        options: [
                            {
                                field: 'netMeteringScheme',
                                name: 'Net Metering / Export Scheme',
                                type: 'select',
                                choices: [
                                    { value: 'nem1', name: 'NEM 1.0 (Full Retail 1:1 Credits)' },
                                    { value: 'nem2', name: 'NEM 2.0 (Retail Credits minus NBCs)' },
                                    { value: 'sbp', name: 'Solar Billing Plan (NEM 3.0)' }
                                ],
                                default: 'sbp'
                            }
                        ]
                    }
                ]
            }
        ] as any);

        await navigateToSettings();

        // Select Service (Provider)
        const serviceSelect = await screen.findByRole('combobox', { name: /Utility Provider/i });
        await user.click(serviceSelect);
        const sceOption = await screen.findByRole('option', { name: 'Southern California Edison' });
        await user.click(sceOption);

        // Rate/Plan should be auto-selected since SCE only has one in this mock
        await screen.findByText(/TOU-D-PRIME/i);

        // Verify select option appears for export scheme
        const schemeSelect = await screen.findByLabelText(/Net Metering \/ Export Scheme/i);
        expect(schemeSelect).toBeInTheDocument();
        expect(screen.getByText('Solar Billing Plan (NEM 3.0)')).toBeInTheDocument();

        // Change select option to NEM 2.0
        await user.click(schemeSelect);
        const nem2Option = await screen.findByRole('option', { name: 'NEM 2.0 (Retail Credits minus NBCs)' });
        await user.click(nem2Option);

        // Save
        (updateSettings as any).mockResolvedValue(undefined);
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                utilityProvider: 'sce',
                utilityRate: 'sce_tou_d_prime',
                utilityRateOptions: expect.objectContaining({
                    netMeteringScheme: 'nem2'
                })
            }), expect.any(String), undefined);
        });
    });

    it('supports multi-stage OTP credential staging flow for Enphase', async () => {
        const user = userEvent.setup();
        const enphaseSettings = {
            ...defaultSettings,
            ess: 'enphase',
            hasCredentials: {}
        };
        (fetchSettings as any).mockResolvedValue(enphaseSettings);
        (api.fetchESSList as any).mockResolvedValue([
            ...defaultESSProviders,
            {
                id: 'enphase',
                name: 'Enphase',
                credentials: [
                    { field: 'username', name: 'Email', type: 'string', required: true, stage: 0 },
                    { field: 'code', name: 'Email Code', type: 'string', required: false, stage: 1 }
                ]
            }
        ]);
        (api.submitESSStage as any).mockResolvedValue(undefined);

        await navigateToSettings();

        const emailInput = await screen.findByLabelText('Email');
        expect(emailInput).toBeInTheDocument();
        expect(screen.queryByLabelText(/Email Code/i)).not.toBeInTheDocument();

        await user.type(emailInput, 'test@example.com');

        const continueBtn = screen.getByRole('button', { name: 'Continue' });
        expect(continueBtn).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: 'Save Settings' })).not.toBeInTheDocument();

        await user.click(continueBtn);

        expect(api.submitESSStage).toHaveBeenCalledWith('enphase', { username: 'test@example.com' }, expect.any(String));

        const codeInput = await screen.findByLabelText(/Email Code/i);
        expect(codeInput).toBeInTheDocument();

        expect(screen.queryByRole('button', { name: 'Continue' })).not.toBeInTheDocument();
        const saveBtn = screen.getByRole('button', { name: 'Save Settings' });
        expect(saveBtn).toBeInTheDocument();

        await user.type(codeInput, '654321');

        (updateSettings as any).mockResolvedValue(undefined);
        await user.click(saveBtn);

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(expect.anything(), expect.any(String), {
                enphase: {
                    username: 'test@example.com',
                    code: '654321'
                }
            });
        });
    });

    it('holds opaque stageToken from submitESSStage across resend and passes it to updateSettings on OTP completion', async () => {
        const user = userEvent.setup();
        (api.fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'enphase',
            hasCredentials: { enphase: false }
        });
        (api.fetchESSList as any).mockResolvedValue([
            {
                id: 'enphase',
                name: 'Enphase',
                credentials: [
                    { field: 'username', name: 'Email', type: 'string', required: true, stage: 0 },
                    { field: 'code', name: 'Email Code', type: 'string', required: false, stage: 1, oneTime: true }
                ]
            }
        ]);
        (api.submitESSStage as any)
            .mockResolvedValueOnce({ success: true, stageToken: 'mock-stage-blob-1' })
            .mockResolvedValueOnce({ success: true, stageToken: 'mock-stage-blob-2' });

        await navigateToSettings();

        const emailInput = await screen.findByLabelText('Email');
        await user.type(emailInput, 'owner@example.com');
        await user.click(screen.getByRole('button', { name: 'Continue' }));

        expect(api.submitESSStage).toHaveBeenNthCalledWith(1, 'enphase', { username: 'owner@example.com' }, expect.any(String));

        // Resend refreshes the opaque stageToken
        const resendBtn = await screen.findByRole('button', { name: /Resend Code/i });
        await user.click(resendBtn);
        expect(api.submitESSStage).toHaveBeenNthCalledWith(2, 'enphase', { username: 'owner@example.com' }, expect.any(String));

        const codeInput = await screen.findByLabelText(/Email Code/i);
        await user.type(codeInput, '654321');

        // Expired stageToken resets back to stage 0 so the user can request a fresh token
        (updateSettings as any).mockRejectedValueOnce(
            new Error('ESS verification session expired. Please start verification again.')
        );
        await user.click(screen.getByRole('button', { name: 'Save Settings' }));

        await waitFor(() => {
            expect(updateSettings).toHaveBeenCalledWith(
                expect.objectContaining({ ess: 'enphase' }),
                expect.any(String),
                { enphase: { username: 'owner@example.com', code: '654321' } },
                'mock-stage-blob-2'
            );
        });
        expect(await screen.findByRole('button', { name: 'Continue' })).toBeInTheDocument();

        // Start stage 0 again and complete save cleanly
        (api.submitESSStage as any).mockResolvedValueOnce({ success: true, stageToken: 'mock-stage-blob-3' });
        await user.click(screen.getByRole('button', { name: 'Continue' }));

        const freshCodeInput = await screen.findByLabelText(/Email Code/i);
        await user.type(freshCodeInput, '112233');
        (updateSettings as any).mockResolvedValueOnce(undefined);
        await user.click(screen.getByRole('button', { name: 'Save Settings' }));

        await waitFor(() => {
            expect(updateSettings).toHaveBeenLastCalledWith(
                expect.objectContaining({ ess: 'enphase' }),
                expect.any(String),
                { enphase: { username: 'owner@example.com', code: '112233' } },
                'mock-stage-blob-3'
            );
        });
    });

    it('can request a new utility rate/option via the dialog', async () => {
        const user = userEvent.setup();
        (api.fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            utilityProvider: 'comed',
            utilityRate: 'comed_besh'
        });
        await navigateToSettings();

        // 1. Click "Change" in the Utility Service section to open/edit utility configuration
        fireEvent.click(screen.getByRole('button', { name: 'Change Utility Service' }));

        // 2. Click the link to open the dialog
        const unsupportedLink = await screen.findByRole('button', { name: "Don't see your rate or options?" });
        await user.click(unsupportedLink);

        // 3. Verify dialog is open and shows correct title
        const dialogTitle = await screen.findByRole('heading', { name: 'Request a Rate or Option' });
        expect(dialogTitle).toBeInTheDocument();

        // 4. Select utility 'Other'
        const serviceSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
        await user.click(serviceSelect);
        const otherOption = await screen.findByRole('option', { name: 'Other' });
        await user.click(otherOption);

        // 5. Fill out the details
        const providerInput = screen.getByLabelText('Utility Provider Name');
        await user.type(providerInput, 'National Grid');

        const stateInput = screen.getByLabelText('State');
        await user.type(stateInput, 'NY');

        const rateInput = screen.getByLabelText('Rate / Plan Name');
        await user.type(rateInput, 'TOU-123');

        const commentInput = screen.getByLabelText(/Anything else to share or comments?/i);
        await user.type(commentInput, 'Please add this rate!');

        // Mock API submission
        (api.submitInterest as any).mockResolvedValue(undefined);

        // 6. Click submit
        const submitBtn = screen.getByRole('button', { name: 'Express Interest' });
        await user.click(submitBtn);

        // 7. Verify success message
        await waitFor(() => {
            expect(screen.getByText('Success!')).toBeInTheDocument();
            expect(screen.getByText(/We've received your interest!/)).toBeInTheDocument();
        });

        // 8. Verify the submitInterest API was called with the correct arguments
        expect(api.submitInterest).toHaveBeenCalledWith({
            utility: 'other',
            battery: 'none',
            utilityProviderName: 'National Grid',
            state: 'NY',
            planName: 'TOU-123',
            batteryName: '',
            comments: 'Please add this rate!'
        });

        // 9. Close the dialog
        const closeBtn = screen.getByRole('button', { name: 'Close' });
        await user.click(closeBtn);

        // 10. Verify dialog is closed (title is no longer visible)
        await waitFor(() => {
            expect(screen.queryByRole('heading', { name: 'Request a Rate or Option' })).not.toBeInTheDocument();
        });
    });

    it('can update optimizationProfile setting', async () => {
        const user = userEvent.setup();
        await navigateToSettings();

        // Expand advanced tuning settings
        const advancedBtn = await screen.findByText('Show Advanced Settings');
        await user.click(advancedBtn);

        await waitFor(() => {
            expect(screen.getByRole('combobox', { name: /Optimization Profile/i })).toBeInTheDocument();
        });

        const profileSelect = screen.getByRole('combobox', { name: /Optimization Profile/i });
        expect(profileSelect).toHaveTextContent('Balanced');

        await user.click(profileSelect);
        const aggressiveOption = await screen.findByRole('option', { name: 'Aggressive' });
        await user.click(aggressiveOption);

        (updateSettings as any).mockResolvedValue(undefined);
        const saveBtn = screen.getByText('Save Settings');
        fireEvent.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                optimizationProfile: 'aggressive'
            }), expect.any(String), undefined);
        });
    });

    it('clears one-time credentials on save failure to force restart', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'multi_region_ess',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Fill in required authCode based on apiMocks
        const passInput = await screen.findByLabelText(/Authorization Code/i, { selector: 'input[type="password"]' });
        await user.type(passInput, 'testauthcode');

        // Mock update to fail
        (updateSettings as any).mockRejectedValue(new Error('Failed to verify credentials'));

        // Save
        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        // Expect error to be shown
        await waitFor(() => {
            expect(screen.getAllByText('Failed to verify credentials').length).toBeGreaterThan(0);
        });

        // Click save again - now it should fail because authCode was cleared and is required
        await user.click(saveBtn);
        await waitFor(() => {
            expect(screen.getAllByText('Login to connect your energy system.').length).toBeGreaterThan(0);
        });
    });

    it('allows retry without re-login when server saved rotated credentials on failure', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: {}
        });

        await navigateToSettings();

        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        await user.click(loginBtn);

        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'tesla-auth-code', state: defaultAuthStatus.sites![0].id }
            }));
        });

        await waitFor(() => {
            expect(screen.getByText(/Received code! Save Settings below to complete./i)).toBeInTheDocument();
        });

        // First save fails after Tesla exchanged the auth code and server saved the tokens
        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: upstream status 503'), { credentialsSaved: true })
        );

        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        await waitFor(() => {
            expect(screen.getAllByText('failed to verify ess credentials: upstream status 503').length).toBeGreaterThan(0);
        });
        expect(updateSettings).toHaveBeenCalledTimes(1);
        expect(updateSettings).toHaveBeenNthCalledWith(1, expect.objectContaining({
            ess: 'tesla',
        }), expect.any(String), {
            tesla: {
                authCode: 'tesla-auth-code',
            },
        });

        // Retry should reach the server with saved tokens without requiring another login
        (updateSettings as any).mockResolvedValueOnce(undefined);
        await user.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
        });
        expect(screen.queryByText('Login to connect your energy system.')).not.toBeInTheDocument();
        expect(updateSettings).toHaveBeenCalledTimes(2);
        expect(updateSettings).toHaveBeenNthCalledWith(2, expect.objectContaining({
            ess: 'tesla',
        }), expect.any(String), {
            tesla: {},
        });

        windowOpenSpy.mockRestore();
    });

    it('blocks Tesla reconnect retry from reusing the old account when new authCode exchange fails without credentialsSaved', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: { tesla: true },
        });

        await navigateToSettings();

        const editBtn = await screen.findByLabelText('Edit Energy Storage System');
        await user.click(editBtn);

        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        await user.click(screen.getByRole('button', { name: /Login to link account/i }));
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'expired-reconnect-code', state: defaultAuthStatus.sites![0].id }
            }));
        });

        await waitFor(() => {
            expect(screen.getByText(/Received code! Save Settings below to complete./i)).toBeInTheDocument();
        });

        // Token exchange fails BEFORE credentialsSaved (old account tokens are still in storage)
        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: authorization code is invalid'), { credentialsSaved: false })
        );

        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        await waitFor(() => {
            expect(screen.getAllByText(/The authorization code expired\. Please click 'Login to link account' again/i).length).toBeGreaterThan(0);
        });
        expect(updateSettings).toHaveBeenCalledTimes(1);
        expect(updateSettings).toHaveBeenNthCalledWith(1, expect.objectContaining({
            ess: 'tesla',
        }), expect.any(String), {
            tesla: {
                authCode: 'expired-reconnect-code',
            },
        });

        // Clicking Save Settings again without re-authenticating MUST NOT reuse hasCredentials.tesla on the old account
        await user.click(saveBtn);
        await waitFor(() => {
            expect(screen.getAllByText('Login to connect your energy system.').length).toBeGreaterThan(0);
        });
        expect(updateSettings).toHaveBeenCalledTimes(1);

        // Fresh login whose token exchange succeeds (credentialsSaved: true) before site verification fails CAN retry
        await user.click(screen.getByRole('button', { name: /Login to link account/i }));
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'fresh-reconnect-code', state: defaultAuthStatus.sites![0].id }
            }));
        });

        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: upstream status 503'), { credentialsSaved: true })
        );
        await user.click(saveBtn);

        await waitFor(() => {
            expect(screen.getAllByText('failed to verify ess credentials: upstream status 503').length).toBeGreaterThan(0);
            expect(screen.getByText(/Account linked! Click Save Settings below to retry/i)).toBeInTheDocument();
        });
        expect(updateSettings).toHaveBeenCalledTimes(2);

        // Opening another OAuth popup and cancelling/closing it without receiving a new code must NOT erase saved-token retry state
        await user.click(screen.getByRole('button', { name: /Login to link account/i }));
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: {
                    type: 'OAUTH_ERROR',
                    error: 'access_denied',
                    state: defaultAuthStatus.sites![0].id,
                }
            }));
        });
        await waitFor(() => {
            expect(screen.getAllByText(/Authorization was cancelled or permissions were not granted/i).length).toBeGreaterThan(0);
            expect(screen.getByText(/Account linked! Click Save Settings below to retry/i)).toBeInTheDocument();
        });

        // Also verify closing the popup directly without receiving a new code preserves retry state
        vi.useFakeTimers({ shouldAdvanceTime: true });
        try {
            mockPopup.closed = false;
            await user.click(screen.getByRole('button', { name: /Login to link account/i }));
            mockPopup.closed = true;
            act(() => {
                vi.advanceTimersByTime(600);
            });
            await waitFor(() => {
                expect(screen.getByRole('button', { name: /Login to link account/i })).not.toBeDisabled();
            });
        } finally {
            vi.useRealTimers();
        }

        // Editing an optional non-login field (e.g. serialNumber / energySiteID) after credentialsSaved: true must NOT require another login
        const optionalSiteInput = screen.getByLabelText(/Energy Site ID \(Optional\)/i);
        await user.type(optionalSiteInput, 'TG-SERIAL-42');
        expect(screen.getByText(/Account linked! Click Save Settings below to retry/i)).toBeInTheDocument();

        (updateSettings as any).mockResolvedValueOnce(undefined);
        await user.click(saveBtn);

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
        });
        expect(updateSettings).toHaveBeenCalledTimes(3);
        expect(updateSettings).toHaveBeenNthCalledWith(3, expect.objectContaining({
            ess: 'tesla',
        }), expect.any(String), {
            tesla: {
                energySiteID: 'TG-SERIAL-42',
            },
        });

        windowOpenSpy.mockRestore();
    });

    it('handles Tesla OAuth permission rejection cleanly so user can immediately retry', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: {}
        });

        await navigateToSettings();

        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        await user.click(loginBtn);
        expect(screen.getByRole('button', { name: /Awaiting link/i })).toBeDisabled();

        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: {
                    type: 'OAUTH_ERROR',
                    error: 'access_denied',
                    errorDescription: 'User rejected permissions',
                    state: defaultAuthStatus.sites![0].id,
                }
            }));
        });

        await waitFor(() => {
            expect(screen.getByText(/Authorization was cancelled or permissions were not granted/i)).toBeInTheDocument();
        });
        expect(screen.getByRole('button', { name: /Login to link account/i })).not.toBeDisabled();

        windowOpenSpy.mockRestore();
    });

    it('supports Enphase Resend Code and resets stage when editing stage-0 email', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'enphase',
            hasCredentials: {}
        });
        (api.fetchESSList as any).mockResolvedValue([
            ...defaultESSProviders,
            {
                id: 'enphase',
                name: 'Enphase',
                credentials: [
                    { field: 'username', name: 'Email', type: 'string', required: true, stage: 0 },
                    { field: 'code', name: 'Email Code', type: 'string', required: false, stage: 1, oneTime: true },
                ],
            },
        ]);
        (api.submitESSStage as any).mockResolvedValue({ success: true });

        await navigateToSettings();

        const emailInput = await screen.findByPlaceholderText(/Enter Email/i);
        await user.type(emailInput, 'owner@example.com');
        await user.click(screen.getByRole('button', { name: 'Continue' }));

        const codeInput = await screen.findByPlaceholderText(/Enter Email Code/i);
        await user.type(codeInput, '123456');

        // Click Resend Code -> re-submits stage 0, clears old code, and shows confirmation
        const resendBtn = screen.getByRole('button', { name: /Resend Code/i });
        await user.click(resendBtn);

        await waitFor(() => {
            expect(screen.getByText(/A new verification code has been sent/i)).toBeInTheDocument();
        });
        expect(api.submitESSStage).toHaveBeenCalledTimes(2);
        expect(api.submitESSStage).toHaveBeenNthCalledWith(2, 'enphase', { username: 'owner@example.com' }, expect.any(String));
        expect(screen.getByPlaceholderText(/Enter Email Code/i)).toHaveValue('');

        // Editing stage-0 Email resets back to stage 0 so Continue reappears
        await user.type(emailInput, '.uk');
        await waitFor(() => {
            expect(screen.queryByPlaceholderText(/Enter Email Code/i)).not.toBeInTheDocument();
            expect(screen.getByRole('button', { name: 'Continue' })).toBeInTheDocument();
        });
    });

    it('preserves unconsumed Tesla authCode when save fails for a non-ESS validation error', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: {}
        });

        await navigateToSettings();

        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        await user.click(screen.getByRole('button', { name: /Login to link account/i }));
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'fresh-unconsumed-code', state: defaultAuthStatus.sites![0].id }
            }));
        });

        await waitFor(() => {
            expect(screen.getByText(/Received code! Save Settings below to complete./i)).toBeInTheDocument();
        });

        (updateSettings as any).mockRejectedValueOnce(new Error('failed to fetch location data: invalid postal code'));
        await user.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(screen.getAllByText('failed to fetch location data: invalid postal code').length).toBeGreaterThan(0);
        });
        // Auth code should NOT have been cleared since the server rejected before ESS auth
        expect(screen.getByText(/Received code! Save Settings below to complete./i)).toBeInTheDocument();

        windowOpenSpy.mockRestore();
    });

    it('clears consumed Tesla authCode and retries with saved tokens even when error mentions network', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: {}
        });

        await navigateToSettings();

        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        await user.click(screen.getByRole('button', { name: /Login to link account/i }));
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'tesla-network-code', state: defaultAuthStatus.sites![0].id }
            }));
        });

        await waitFor(() => {
            expect(screen.getByText(/Received code! Save Settings below to complete./i)).toBeInTheDocument();
        });

        // Upstream network error AFTER tokens were saved must still clear authCode
        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: dial tcp: network is unreachable'), { credentialsSaved: true })
        );
        await user.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(screen.getAllByText('failed to verify ess credentials: dial tcp: network is unreachable').length).toBeGreaterThan(0);
            expect(screen.getByText(/Account linked! Click Save Settings below to retry/i)).toBeInTheDocument();
        });

        (updateSettings as any).mockResolvedValueOnce(undefined);
        await user.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
        });
        expect(updateSettings).toHaveBeenNthCalledWith(2, expect.objectContaining({
            ess: 'tesla',
        }), expect.any(String), {
            tesla: {},
        });

        windowOpenSpy.mockRestore();
    });

    it('ignores OAUTH_ERROR from a mismatched site state without breaking active OAuth flow', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: {}
        });

        await navigateToSettings();

        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        await user.click(screen.getByRole('button', { name: /Login to link account/i }));
        expect(screen.getByRole('button', { name: /Awaiting link/i })).toBeDisabled();

        // Stale popup from a different site posts OAUTH_ERROR -> must be ignored
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_ERROR', error: 'access_denied', state: 'other-site-id' }
            }));
        });
        expect(screen.getByRole('button', { name: /Awaiting link/i })).toBeDisabled();

        // Legitimate popup posts OAUTH_CODE -> still received
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'valid-code', state: defaultAuthStatus.sites![0].id }
            }));
        });
        await waitFor(() => {
            expect(screen.getByText(/Received code! Save Settings below to complete./i)).toBeInTheDocument();
        });

        windowOpenSpy.mockRestore();
    });

    it('shows Enphase verified retry banner, hides Resend Code, and clears stale state on provider switch', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'enphase',
            hasCredentials: {}
        });
        (api.fetchESSList as any).mockResolvedValue([
            ...defaultESSProviders,
            {
                id: 'enphase',
                name: 'Enphase',
                credentials: [
                    { field: 'username', name: 'Email', type: 'string', required: true, stage: 0 },
                    { field: 'code', name: 'Email Code', type: 'string', required: false, stage: 1, oneTime: true },
                ],
            },
        ]);
        (api.submitESSStage as any).mockResolvedValue({ success: true });

        await navigateToSettings();

        const emailInput = await screen.findByPlaceholderText(/Enter Email/i);
        await user.type(emailInput, 'owner@example.com');
        await user.click(screen.getByRole('button', { name: 'Continue' }));

        const codeInput = await screen.findByPlaceholderText(/Enter Email Code/i);
        await user.type(codeInput, '654321');

        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: upstream 503'), { credentialsSaved: true })
        );
        await user.click(screen.getByText('Save Settings'));

        await waitFor(() => {
            expect(screen.getByText(/Account verified! Click Save Settings below to retry without entering a new code./i)).toBeInTheDocument();
        });
        expect(screen.queryByRole('button', { name: /Resend Code/i })).not.toBeInTheDocument();

        // Switching provider away and back must NOT leave a stale verified banner at stage 0
        await user.click(screen.getByRole('combobox', { name: /System Type/i }));
        await user.click(await screen.findByRole('option', { name: 'Tesla' }));
        await user.click(screen.getByRole('combobox', { name: /System Type/i }));
        await user.click(await screen.findByRole('option', { name: 'Enphase' }));

        expect(screen.queryByText(/Account verified!/i)).not.toBeInTheDocument();
    });

    it('blocks Tesla reconnect retry from reusing old account when new authCode exchange fails, and clears retry state on unauthorized', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: { tesla: true },
        });

        await navigateToSettings();

        await user.click(await screen.findByLabelText('Edit Energy Storage System'));

        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);

        // 1. Replacement Tesla authCode exchange fails (credentialsSaved: false)
        await user.click(await screen.findByRole('button', { name: /Login to link account/i }));
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'NA_bad_reconnect_code', state: defaultAuthStatus.sites![0].id },
            }));
        });
        await screen.findByText('Received code! Save Settings below to complete.');

        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: failed to exchange auth code: invalid_grant'), { credentialsSaved: false })
        );
        await user.click(screen.getByText('Save Settings'));
        await screen.findAllByText(/failed to exchange auth code: invalid_grant/i);
        expect(updateSettings).toHaveBeenCalledTimes(1);

        // Clicking Save Settings again MUST NOT send { tesla: {} } against the old account
        await user.click(screen.getByText('Save Settings'));
        await screen.findAllByText('Login to connect your energy system.');
        expect(updateSettings).toHaveBeenCalledTimes(1);

        // 2. Fresh authCode exchanges (credentialsSaved: true), then fails validation with 503
        await user.click(await screen.findByRole('button', { name: /Login to link account/i }));
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'NA_good_reconnect_code', state: defaultAuthStatus.sites![0].id },
            }));
        });
        await screen.findByText('Received code! Save Settings below to complete.');

        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: upstream 503'), { credentialsSaved: true })
        );
        await user.click(screen.getByText('Save Settings'));
        await screen.findByText(/Account linked! Click Save Settings below to retry/i);
        expect(updateSettings).toHaveBeenCalledTimes(2);

        // 3. If the retry fails with unauthorized, retry state is cleared so user must re-login
        (updateSettings as any).mockRejectedValueOnce(
            Object.assign(new Error('failed to verify ess credentials: unauthorized'), { credentialsSaved: false })
        );
        await user.click(screen.getByText('Save Settings'));
        await screen.findAllByText(/failed to verify ess credentials: unauthorized/i);
        expect(screen.queryByText(/Account linked!/i)).not.toBeInTheDocument();
        expect(updateSettings).toHaveBeenCalledTimes(3);

        await user.click(screen.getByText('Save Settings'));
        await screen.findAllByText('Login to connect your energy system.');
        expect(updateSettings).toHaveBeenCalledTimes(3);

        windowOpenSpy.mockRestore();
    });

    it('handles invalid authorization code error and expands ESS section with custom message', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'franklin',
            hasCredentials: { franklin: true }
        });

        await navigateToSettings();

        // Initially ESS is not in edit mode (it's configured)
        expect(screen.queryByLabelText('Email')).not.toBeInTheDocument();

        // Make updateSettings fail with invalid auth code error
        (updateSettings as any).mockRejectedValue(new Error('failed to exchange auth code: upstream error status 400: The authorization code is invalid. It may have already been used or expired.'));

        // Submit form
        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        // Expect custom message and ESS section to be open
        await waitFor(() => {
            expect(screen.getByText("The authorization code expired. Please click 'Login to link account' again and save immediately.")).toBeInTheDocument();
            expect(screen.getByLabelText('Email')).toBeInTheDocument();
        });
    });

    it('handles oauthStatus transitions and popup button disabled/spinner states', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'multi_region_ess',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Mock window.open
        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        // Click login button
        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        await user.click(loginBtn);

        // Expect button to be disabled and show "Awaiting link..." with guidance text
        expect(screen.getByRole('button', { name: /Awaiting link.../i })).toBeDisabled();
        expect(screen.getByText('Please complete authentication in the popup window.')).toBeInTheDocument();

        // Simulate successful OAuth message
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'success_code_123', state: 'site1' }
            }));
        });

        // Expect success badge to render and button to be gone
        await waitFor(() => {
            expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            expect(screen.queryByRole('button', { name: /Login to link account/i })).not.toBeInTheDocument();
        });

        windowOpenSpy.mockRestore();
    });

    it('clears Tesla oauth success message and shows login button on save failure', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: 'tesla',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Initially we see the Login button, not the success message
        expect(screen.getByRole('button', { name: /Login to link account/i })).toBeInTheDocument();
        expect(screen.queryByText('Received code! Save Settings below to complete.')).not.toBeInTheDocument();

        // Mock window.open
        const mockPopup = { closed: false };
        const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

        // Click login button
        const loginBtn = screen.getByRole('button', { name: /Login to link account/i });
        await user.click(loginBtn);

        // Simulate successful OAuth message
        act(() => {
            window.dispatchEvent(new MessageEvent('message', {
                origin: window.location.origin,
                data: { type: 'OAUTH_CODE', code: 'tesla_success_code', state: 'site1' }
            }));
        });

        // Expect success badge to render and button to be gone
        await waitFor(() => {
            expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            expect(screen.queryByRole('button', { name: /Login to link account/i })).not.toBeInTheDocument();
        });

        // Mock update to fail
        (updateSettings as any).mockRejectedValue(new Error('Failed to verify credentials'));

        // Save
        const saveBtn = screen.getByText('Save Settings');
        await user.click(saveBtn);

        // Expect error to be shown, success banner to be gone, and login button to be back
        await waitFor(() => {
            expect(screen.getAllByText('Failed to verify credentials').length).toBeGreaterThan(0);
            expect(screen.queryByText('Received code! Save Settings below to complete.')).not.toBeInTheDocument();
            expect(screen.getByRole('button', { name: /Login to link account/i })).toBeInTheDocument();
        });

        windowOpenSpy.mockRestore();
    });

    it('hides grid strategy switches when connecting an ESS for the first time before saving credentials', async () => {
        const user = userEvent.setup();
        (fetchSettings as any).mockResolvedValue({
            ...defaultSettings,
            ess: '',
            hasCredentials: {}
        });

        await navigateToSettings();

        // Select an ESS for the first time
        const essSelect = await screen.findByLabelText(/System Type/i);
        await user.click(essSelect);
        const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
        await user.click(franklinOption);

        // Grid switches and warning should NOT be displayed before credentials are saved
        expect(screen.queryByRole('switch', { name: /Grid Can Charge Battery/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('switch', { name: /Export Solar to Grid/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('switch', { name: /Export Battery to Grid/i })).not.toBeInTheDocument();
        expect(screen.queryByTestId('grid-restrictions-warning')).not.toBeInTheDocument();
    });

    it('shows a warning below checkboxes when all grid strategy settings are unchecked', async () => {
        await navigateToSettings();

        // Open ESS edit mode to reveal grid strategy settings
        const updateBtn = await screen.findByRole('button', { name: /Update Energy Storage System/i });
        fireEvent.click(updateBtn);

        // Warning should not be shown initially (gridChargeBatteries is true by default)
        expect(screen.queryByTestId('grid-restrictions-warning')).not.toBeInTheDocument();

        // Toggle "Grid Can Charge Battery" to false (so all three grid settings are false)
        const chargeSwitch = await screen.findByRole('switch', { name: /Grid Can Charge Battery/i });
        fireEvent.click(chargeSwitch);

        // Warning should be displayed now
        await waitFor(() => {
            expect(screen.getByTestId('grid-restrictions-warning')).toBeInTheDocument();
            expect(screen.getByText(/Warning: All grid interactions are disabled/i)).toBeInTheDocument();
        });

        // Toggle it back on
        fireEvent.click(chargeSwitch);

        // Warning should disappear
        await waitFor(() => {
            expect(screen.queryByTestId('grid-restrictions-warning')).not.toBeInTheDocument();
        });
    });

    describe('Onboarding Wizard & Checklist Flow', () => {
        it('renders onboarding wizard and walks through steps to completion', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => {
                expect(screen.getByText('Google Sign In')).toBeInTheDocument();
            });
            fireEvent.click(screen.getByText('Google Sign In'));

            // Automatically redirected to settings onboarding wizard Step 1
            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });

            // Fill Location
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');

            (updateSettings as any).mockResolvedValue(undefined);

            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Verify updateSettings was not called yet
            expect(updateSettings).not.toHaveBeenCalled();

            // Step 2 Utility should be shown
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });

            // Select Utility Provider
            const providerSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
            await user.click(providerSelect);
            const comedOption = await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' });
            await user.click(comedOption);

            // Select Utility Rate
            const rateSelect = await screen.findByRole('combobox', { name: /Rate Plan/i });
            await user.click(rateSelect);
            const rateOption = await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' });
            await user.click(rateOption);

            const nextBtn2 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn2);

            // Verify updateSettings was still not called
            expect(updateSettings).not.toHaveBeenCalled();

            // Step 3 ESS should be shown
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });

            // Select ESS Provider
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const franklinOption = await screen.findByRole('option', { name: 'FranklinWH' });
            await user.click(franklinOption);

            // Type credentials
            const emailInput = await screen.findByPlaceholderText(/Enter Email/i);
            const passInput = await screen.findByPlaceholderText(/Enter Password/i);
            await user.type(emailInput, 'test@franklin.com');
            await user.type(passInput, 'secretPass');

            // Mock refetch return for completion
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'franklin',
                gridExportSolar: true,
                manageTOUSchedules: true,
                hasCredentials: { franklin: true }
            });

            const doneBtn = screen.getByRole('button', { name: /Complete Setup/i });
            await user.click(doneBtn);

            // Verify final updateSettings call (called once with all collected settings)
            expect(updateSettings).toHaveBeenCalledTimes(1);
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'franklin',
            }), expect.any(String), expect.objectContaining({
                franklin: expect.objectContaining({
                    username: 'test@franklin.com',
                    password: 'secretPass'
                })
            }));

            // Confirm Grid Settings modal should appear for FranklinWH with TOU schedule management enabled and notice visible
            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByText(/Based on your FranklinWH configuration/i)).toBeInTheDocument();
                const touSwitch = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
                expect(touSwitch).toBeChecked();
                expect(screen.getByText(/Notice: This feature requires RateRudder to manage your battery's Time-Of-Use/i)).toBeInTheDocument();
            });

            const confirmBtn = screen.getByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            // Should navigate to dashboard
            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
        });

        it('allows navigating back in the onboarding wizard and retains entered input', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => {
                expect(screen.getByText('Google Sign In')).toBeInTheDocument();
            });
            fireEvent.click(screen.getByText('Google Sign In'));

            // Automatically redirected to settings onboarding wizard Step 1
            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });

            // Fill Location
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '12345');

            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Step 2 Utility should be shown
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });

            // Click Back
            const backBtn = screen.getByRole('button', { name: 'Back' });
            await user.click(backBtn);

            // Step 1 Location should be shown again
            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });

            // Check that zip code is retained
            expect(screen.getByLabelText(/Zip\/Postal Code/i)).toHaveValue('12345');
        });

        it('shows getting started checklist and highlights incomplete sections in checklist mode', async () => {
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            // Navigate to Settings
            await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
            fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

            // Checklist banner should be visible
            await screen.findByTestId('checklist-banner');
            expect(screen.getByText('Set Location')).toBeInTheDocument();
            expect(screen.getByText('Choose Utility')).toBeInTheDocument();
            expect(screen.getByText('Connect Battery (ESS)')).toBeInTheDocument();

            // Assert Location section is highlighted and incomplete
            expect(screen.getByTestId('location-section')).toHaveClass('highlighted-section');
            // Assert ESS section is highlighted and incomplete
            expect(screen.getByTestId('ess-section')).toHaveClass('highlighted-section');
            // Assert Utility section is NOT highlighted (since it is configured)
            expect(screen.getByTestId('utility-section')).not.toHaveClass('highlighted-section');
        });

        it('does not show checklist or highlighted-section on ESS section when changing ESS on configured site', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'franklin',
                hasCredentials: { franklin: true }
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            // Navigate to Settings
            await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
            fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

            await screen.findByRole('heading', { name: /^Settings$/i });

            // Checklist banner should NOT be visible
            expect(screen.queryByTestId('checklist-banner')).not.toBeInTheDocument();
            expect(screen.getByTestId('ess-section')).not.toHaveClass('highlighted-section');

            // Click Update on ESS section to open fields
            const updateBtn = screen.getByRole('button', { name: /Update Energy Storage System/i });
            await user.click(updateBtn);

            // Change ESS provider in the dropdown
            const essSelect = await screen.findByLabelText(/ESS Type/i);
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            // Checklist banner and highlighted-section should still NOT appear
            expect(screen.queryByTestId('checklist-banner')).not.toBeInTheDocument();
            expect(screen.getByTestId('ess-section')).not.toHaveClass('highlighted-section');
        });

        it('allows skipping onboarding wizard and going directly to all settings', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => {
                expect(screen.getByText('Google Sign In')).toBeInTheDocument();
            });
            fireEvent.click(screen.getByText('Google Sign In'));

            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });

            const skipBtn = screen.getByRole('button', { name: /Skip setup wizard/i });
            await user.click(skipBtn);

            // Standard settings form should render now
            await screen.findByLabelText(/Minimum Battery %/i);
            expect(screen.queryByRole('heading', { name: /Step 1: Set Location/i })).not.toBeInTheDocument();
        });

        it('scrolls to the corresponding section when a checklist item is clicked', async () => {
            const scrollIntoViewMock = vi.fn();
            window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));

            await waitFor(() => expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument());
            fireEvent.click(screen.getByRole('link', { name: 'Settings' }));

            // Click "Set Location"
            const locationItem = await screen.findByText('Set Location');
            fireEvent.click(locationItem);
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' });

            scrollIntoViewMock.mockClear();

            // Click "Connect Battery (ESS)"
            const essItem = screen.getByText('Connect Battery (ESS)');
            fireEvent.click(essItem);
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' });
        });

        it('shows Tesla grid settings confirmation dialog after onboarding and saves custom settings', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));
            await waitFor(() => expect(screen.getByText('Google Sign In')).toBeInTheDocument());
            fireEvent.click(screen.getByText('Google Sign In'));

            // Step 1: Location
            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');
            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Step 2: Utility
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });
            const providerSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
            await user.click(providerSelect);
            const comedOption = await screen.getByRole('option', { name: 'Commonwealth Edison (ComEd)' });
            await user.click(comedOption);

            const rateSelect = await screen.findByRole('combobox', { name: /Rate Plan/i });
            await user.click(rateSelect);
            const rateOption = await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' });
            await user.click(rateOption);

            const nextBtn2 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn2);

            // Step 3: ESS Tesla
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });
            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'tesla',
                gridChargeBatteries: true,
                gridExportSolar: false,
                gridExportBatteries: false,
                hasCredentials: { tesla: true }
            });

            const doneBtn = screen.getByRole('button', { name: /Complete Setup/i });
            await user.click(doneBtn);

            // Tesla confirmation dialog should be displayed
            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByRole('switch', { name: /Grid Can Charge Battery/i })).toBeInTheDocument();
            });

            const modalChargeSwitch = screen.getByRole('switch', { name: /Grid Can Charge Battery/i });
            expect(modalChargeSwitch).toBeChecked();
            await user.click(modalChargeSwitch);

            const confirmBtn = screen.getByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            // Verify updateSettings was called with customGridSettings: true and gridChargeBatteries: false
            expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                gridChargeBatteries: false,
                customGridSettings: true
            }), expect.any(String));

            await waitFor(() => {
                expect(screen.getByText('Welcome to RateRudder! 🚀')).toBeInTheDocument();
            });

            const nextBtn = screen.getByRole('button', { name: /Next/i });
            await user.click(nextBtn);

            const gotItBtn = screen.getByRole('button', { name: /Got It/i });
            await user.click(gotItBtn);

            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
        });

        it('skips saving when Tesla grid settings confirmation dialog is confirmed without changes', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));
            await waitFor(() => expect(screen.getByText('Google Sign In')).toBeInTheDocument());
            fireEvent.click(screen.getByText('Google Sign In'));

            // Step 1: Location
            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');
            const nextBtn1 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn1);

            // Step 2: Utility
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });
            const providerSelect = screen.getByRole('combobox', { name: /Utility Provider/i });
            await user.click(providerSelect);
            const comedOption = await screen.getByRole('option', { name: 'Commonwealth Edison (ComEd)' });
            await user.click(comedOption);

            const rateSelect = await screen.findByRole('combobox', { name: /Rate Plan/i });
            await user.click(rateSelect);
            const rateOption = await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' });
            await user.click(rateOption);

            const nextBtn2 = screen.getByRole('button', { name: /Save & Continue/i });
            await user.click(nextBtn2);

            // Step 3: ESS Tesla
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });
            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'tesla',
                gridChargeBatteries: true,
                gridExportSolar: false,
                gridExportBatteries: false,
                hasCredentials: { tesla: true }
            });

            const doneBtn = screen.getByRole('button', { name: /Complete Setup/i });
            await user.click(doneBtn);

            // Tesla confirmation dialog should be displayed
            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByRole('switch', { name: /Grid Can Charge Battery/i })).toBeInTheDocument();
            });

            // Clear updateSettings mock call history
            (updateSettings as any).mockClear();

            const confirmBtn = screen.getByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            // Verify updateSettings was NOT called because nothing changed
            expect(updateSettings).not.toHaveBeenCalled();

            await waitFor(() => {
                expect(screen.getByText('Welcome to RateRudder! 🚀')).toBeInTheDocument();
            });

            const nextBtn = screen.getByRole('button', { name: /Next/i });
            await user.click(nextBtn);

            const gotItBtn = screen.getByRole('button', { name: /Got It/i });
            await user.click(gotItBtn);

            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
        });

        it('shows Tesla grid settings confirmation dialog when connecting Tesla from regular settings page', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: '',
                hasCredentials: {}
            });

            await navigateToSettings();

            // Select Tesla
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(essSelect);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });
            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'tesla',
                gridChargeBatteries: true,
                gridExportSolar: true,
                gridExportBatteries: false,
                manageTOUSchedules: true,
                hasCredentials: { tesla: true }
            });

            const saveBtn = screen.getByText('Save Settings');
            await user.click(saveBtn);

            // Tesla confirmation dialog should be displayed with Direct Solar and Battery Export Management checked and TOU warning visible
            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByRole('switch', { name: /Grid Can Charge Battery/i })).toBeInTheDocument();
                const touSwitch = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
                expect(touSwitch).toBeChecked();
                expect(screen.getByText(/Notice: This feature requires RateRudder to manage your battery's Time-Of-Use/i)).toBeInTheDocument();
            });

            const confirmBtn = screen.getByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            // Should stay on settings page
            await waitFor(() => {
                expect(screen.queryByText('Confirm Grid Settings')).not.toBeInTheDocument();
            });
        });

        it('shows Confirm Grid Settings modal for Enphase on initial credential save', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'enphase',
                hasCredentials: {},
                features: { enphaseExportSchedules: true },
            });
            (api.fetchESSList as any).mockResolvedValue([
                ...defaultESSProviders,
                {
                    id: 'enphase',
                    name: 'Enphase',
                    credentials: [
                        { field: 'username', name: 'Email', type: 'string', required: true, stage: 0 },
                        { field: 'code', name: 'Email Code', type: 'string', required: true, stage: 1, oneTime: true },
                    ],
                },
            ]);
            (api.submitESSStage as any).mockResolvedValue({ success: true });

            await navigateToSettings();

            const emailInput = await screen.findByLabelText('Email');
            await user.type(emailInput, 'test@example.com');
            await user.click(screen.getByRole('button', { name: 'Continue' }));

            const codeInput = await screen.findByLabelText(/Email Code/i);
            await user.type(codeInput, '654321');

            (updateSettings as any).mockResolvedValue(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'enphase',
                gridChargeBatteries: true,
                gridExportSolar: true,
                gridExportBatteries: true,
                manageTOUSchedules: true,
                hasCredentials: { enphase: true },
                features: { enphaseExportSchedules: true },
            });

            await user.click(screen.getByRole('button', { name: 'Save Settings' }));

            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
                expect(screen.getByText(/Based on your Enphase configuration/i)).toBeInTheDocument();
                const touSwitch = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
                expect(touSwitch).toBeChecked();
                expect(screen.getByText(/sync your Enphase Electricity Rate/i)).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Confirm & Finish/i }));

            await waitFor(() => {
                expect(screen.queryByText('Confirm Grid Settings')).not.toBeInTheDocument();
            });
        });

        it('clears consumed one-time code on onboarding wizard save failure so retry uses saved session', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false,
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true,
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {},
                features: { enphaseExportSchedules: true },
            });
            (api.fetchESSList as any).mockResolvedValue([
                ...defaultESSProviders,
                {
                    id: 'enphase',
                    name: 'Enphase',
                    credentials: [
                        { field: 'username', name: 'Email', type: 'string', required: true, stage: 0 },
                        { field: 'code', name: 'Email Code', type: 'string', required: false, stage: 1, oneTime: true },
                    ],
                },
            ]);
            (api.submitESSStage as any).mockResolvedValue({ stage: 1 });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));
            await waitFor(() => expect(screen.getByText('Google Sign In')).toBeInTheDocument());
            fireEvent.click(screen.getByText('Google Sign In'));

            // Step 1: Location
            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');
            await user.click(screen.getByRole('button', { name: /Save & Continue/i }));

            // Step 2: Utility
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });
            await user.click(screen.getByRole('combobox', { name: /Utility Provider/i }));
            await user.click(await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' }));
            await user.click(screen.getByRole('combobox', { name: /Rate Plan/i }));
            await user.click(await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' }));
            await user.click(screen.getByRole('button', { name: /Save & Continue/i }));

            // Step 3: ESS (Enphase)
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });
            await user.click(screen.getByRole('combobox', { name: /System Type/i }));
            await user.click(await screen.findByRole('option', { name: 'Enphase' }));

            const emailInput = await screen.findByPlaceholderText(/Enter Email/i);
            await user.type(emailInput, 'test@example.com');
            await user.click(screen.getByRole('button', { name: 'Continue' }));

            const codeInput = await screen.findByPlaceholderText(/Enter Email Code/i);
            await user.type(codeInput, '654321');

            // First Complete Setup fails after OTP is redeemed
            (updateSettings as any).mockRejectedValueOnce(new Error('credential validation failed: upstream 503'));
            await user.click(screen.getByRole('button', { name: /Complete Setup/i }));

            await waitFor(() => {
                expect(screen.getByText('credential validation failed: upstream 503')).toBeInTheDocument();
            });
            expect(updateSettings).toHaveBeenCalledTimes(1);
            expect(updateSettings).toHaveBeenNthCalledWith(1, expect.objectContaining({
                ess: 'enphase',
            }), expect.any(String), {
                enphase: {
                    username: 'test@example.com',
                    code: '654321',
                },
            });

            // One-time code input must be cleared so retry does not resend consumed OTP
            expect(screen.getByPlaceholderText(/Enter Email Code/i)).toHaveValue('');

            // Second Complete Setup succeeds without resending code
            (updateSettings as any).mockResolvedValueOnce(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'enphase',
                gridChargeBatteries: true,
                gridExportSolar: true,
                gridExportBatteries: true,
                manageTOUSchedules: true,
                hasCredentials: { enphase: true },
                features: { enphaseExportSchedules: true },
            });
            await user.click(screen.getByRole('button', { name: /Complete Setup/i }));

            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
            });
            expect(updateSettings).toHaveBeenCalledTimes(2);
            expect(updateSettings).toHaveBeenNthCalledWith(2, expect.objectContaining({
                ess: 'enphase',
            }), expect.any(String), {
                enphase: {
                    username: 'test@example.com',
                },
            });
        });

        it('allows Tesla wizard retry using saved tokens after temporary provider failure', async () => {
            const user = userEvent.setup();
            (fetchAuthStatus as any).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: false,
            }).mockResolvedValueOnce({
                ...defaultAuthStatus,
                loggedIn: true,
            });
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {},
            });

            render(<App />);
            fireEvent.click(screen.getByText(/Log In/));
            await waitFor(() => expect(screen.getByText('Google Sign In')).toBeInTheDocument());
            fireEvent.click(screen.getByText('Google Sign In'));

            // Step 1: Location
            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.clear(zipInput);
            await user.type(zipInput, '90210');
            await user.click(screen.getByRole('button', { name: /Save & Continue/i }));

            // Step 2: Utility
            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });
            await user.click(screen.getByRole('combobox', { name: /Utility Provider/i }));
            await user.click(await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' }));
            await user.click(screen.getByRole('combobox', { name: /Rate Plan/i }));
            await user.click(await screen.findByRole('option', { name: 'Hourly Pricing Program (BESH)' }));
            await user.click(screen.getByRole('button', { name: /Save & Continue/i }));

            // Step 3: ESS (Tesla)
            await screen.findByRole('heading', { name: /Step 3: Connect Battery/i });
            await user.click(screen.getByRole('combobox', { name: /System Type/i }));
            await user.click(await screen.findByRole('option', { name: 'Tesla' }));

            const mockPopup = { closed: false };
            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(mockPopup as any);

            const loginBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(loginBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'tesla-wizard-code', state: defaultAuthStatus.sites![0].id }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText(/Received code! Save Settings below to complete./i)).toBeInTheDocument();
            });

            // First Complete Setup fails after Tesla exchanges authCode and server saves tokens
            (updateSettings as any).mockRejectedValueOnce(
                Object.assign(new Error('failed to verify ess credentials: upstream status 503'), { credentialsSaved: true })
            );
            await user.click(screen.getByRole('button', { name: /Complete Setup/i }));

            await waitFor(() => {
                expect(screen.getByText('failed to verify ess credentials: upstream status 503')).toBeInTheDocument();
                expect(screen.getByText(/Account linked! Click Complete Setup below to retry/i)).toBeInTheDocument();
            });
            expect(updateSettings).toHaveBeenCalledTimes(1);
            expect(updateSettings).toHaveBeenNthCalledWith(1, expect.objectContaining({
                ess: 'tesla',
            }), expect.any(String), {
                tesla: {
                    authCode: 'tesla-wizard-code',
                },
            });

            // Second Complete Setup succeeds without requiring another Tesla login
            (updateSettings as any).mockResolvedValueOnce(undefined);
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: 'US',
                postalCode: '90210',
                utilityProvider: 'comed',
                utilityRate: 'comed_besh',
                ess: 'tesla',
                gridChargeBatteries: true,
                gridExportSolar: true,
                gridExportBatteries: true,
                manageTOUSchedules: true,
                hasCredentials: { tesla: true },
            });
            await user.click(screen.getByRole('button', { name: /Complete Setup/i }));

            await waitFor(() => {
                expect(screen.getByText('Confirm Grid Settings')).toBeInTheDocument();
            });
            expect(screen.queryByText('Login to connect your energy system.')).not.toBeInTheDocument();
            expect(updateSettings).toHaveBeenCalledTimes(2);
            expect(updateSettings).toHaveBeenNthCalledWith(2, expect.objectContaining({
                ess: 'tesla',
            }), expect.any(String), {
                tesla: {},
            });

            windowOpenSpy.mockRestore();
        });

        it('filters country and utility provider options when typing in onboarding wizard', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: '',
                utilityRate: '',
                ess: '',
                hasCredentials: {}
            });

            window.history.pushState(null, '', '/settings');
            render(<App />);

            await screen.findByRole('heading', { name: /Step 1: Set Location/i }, { timeout: 5000 });

            // Type into Country combobox and select a filtered match
            const countryInput = screen.getByRole('combobox', { name: /Country/i });
            await user.clear(countryInput);
            await user.type(countryInput, 'Canada');
            const canadaOption = await screen.findByRole('option', { name: 'Canada' });
            await user.click(canadaOption);
            expect(countryInput).toHaveValue('Canada');

            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.type(zipInput, '90210');
            await user.click(screen.getByRole('button', { name: /Save & Continue/i }));

            await screen.findByRole('heading', { name: /Step 2: Choose Utility/i });

            // Type into Utility Provider combobox and verify filtering works
            const providerInput = screen.getByRole('combobox', { name: /Utility Provider/i });
            await user.type(providerInput, 'Comed');
            expect(await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' })).toBeInTheDocument();
            expect(screen.queryByText('No utilities found.')).not.toBeInTheDocument();

            // Non-matching search shows "No utilities found."
            await user.clear(providerInput);
            await user.type(providerInput, 'UnknownUtilityXYZ');
            expect(await screen.findByText('No utilities found.')).toBeInTheDocument();

            // Close popup and verify "Don't see your rate or options?" link is available during onboarding
            await user.keyboard('{Escape}');
            const unsupportedLink = await screen.findByRole('button', { name: "Don't see your rate or options?" });
            await user.click(unsupportedLink);
            expect(await screen.findByRole('heading', { name: 'Request a Rate or Option' })).toBeInTheDocument();
            await user.click(screen.getByRole('button', { name: 'Close' }));

            // Filter again and select the matching utility
            await user.clear(providerInput);
            await user.type(providerInput, 'Comed');
            await user.click(await screen.findByRole('option', { name: 'Commonwealth Edison (ComEd)' }));

            expect(providerInput).toHaveValue('Commonwealth Edison (ComEd)');
            expect(await screen.findByRole('combobox', { name: /Rate Plan/i })).toBeInTheDocument();
        });
    });

    describe('Site and Account Deletion', () => {
        let hrefValue = '';
        const originalLocation = window.location;

        beforeEach(() => {
            (deleteSite as any).mockResolvedValue(undefined);
            (deleteUser as any).mockResolvedValue(undefined);
            hrefValue = '';
            const locationProxy = new Proxy(originalLocation, {
                get(target, prop) {
                    if (prop === 'href') return hrefValue || target.href;
                    return (target as any)[prop];
                },
                set(target, prop, value) {
                    if (prop === 'href') {
                        hrefValue = value;
                        return true;
                    }
                    (target as any)[prop] = value;
                    return true;
                }
            });
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: locationProxy
            });
        });

        afterEach(() => {
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: originalLocation
            });
        });

        it('shows confirmation dialog when Delete Site button is clicked', async () => {
            await navigateToSettings();

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                expect(screen.getByRole('heading', { name: 'Delete Site' })).toBeInTheDocument();
                expect(screen.getByText(/Are you sure you want to delete this site?/i)).toBeInTheDocument();
            });
        });

        it('enables Delete Account checkbox when user has only 1 site', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [{ id: 'site1', name: 'Site 1' }]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                const deleteAccountSwitch = screen.getByRole('switch', { name: /Delete Account/i });
                expect(deleteAccountSwitch).not.toBeDisabled();
            });
        });

        it('disables Delete Account checkbox with hover tooltip when user has multiple sites', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [
                    { id: 'site1', name: 'Site 1' },
                    { id: 'site2', name: 'Site 2' }
                ]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                const deleteAccountSwitch = screen.getByRole('switch', { name: /Delete Account/i });
                expect(deleteAccountSwitch).toHaveAttribute('aria-disabled', 'true');

                const switchRow = deleteAccountSwitch.closest('.switch-row');
                expect(switchRow).toHaveAttribute('title', 'All sites must be deleted first');
            });
        });

        it('calls deleteSite and redirects to dashboard/welcome on success', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [
                    { id: 'site1', name: 'Site 1' },
                    { id: 'site2', name: 'Site 2' }
                ]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Delete$/i })).toBeInTheDocument();
            });

            fireEvent.click(screen.getByRole('button', { name: /^Delete$/i }));

            await waitFor(() => {
                expect(deleteSite).toHaveBeenCalledWith('site1');
                expect(window.location.href).toBe('/dashboard');
            });
        });

        it('calls deleteUser when deleteAccount is checked and redirects to homepage', async () => {
            const status = {
                ...defaultAuthStatus,
                sites: [{ id: 'site1', name: 'Site 1' }]
            };
            await navigateToSettings(status);

            const deleteSiteBtn = screen.getByRole('button', { name: /Delete Site/i });
            fireEvent.click(deleteSiteBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Delete$/i })).toBeInTheDocument();
            });

            const deleteAccountSwitch = screen.getByRole('switch', { name: /Delete Account/i });
            fireEvent.click(deleteAccountSwitch);

            fireEvent.click(screen.getByRole('button', { name: /^Delete$/i }));

            await waitFor(() => {
                expect(deleteSite).toHaveBeenCalledWith('site1');
                expect(deleteUser).toHaveBeenCalled();
                expect(window.location.href).toBe('/');
            });
        });
    });

    describe('ESS Credentials Tutorial Dialog Integration', () => {
        it('shows tutorial dialog when saving ESS credentials for the first time', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: '',
                hasCredentials: {}
            });

            await navigateToSettings();

            const selectEl = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(selectEl);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });

            windowOpenSpy.mockRestore();

            (updateSettings as any).mockResolvedValue(undefined);

            const saveBtn = screen.getByText('Save Settings');
            await user.click(saveBtn);

            const confirmBtn = await screen.findByRole('button', { name: /Confirm & Finish/i });
            await user.click(confirmBtn);

            await waitFor(() => {
                expect(screen.getByText('Welcome to RateRudder! 🚀')).toBeInTheDocument();
            });

            const nextBtn = screen.getByRole('button', { name: /Next/i });
            await user.click(nextBtn);

            expect(screen.getByText('Manual Charging 💡')).toBeInTheDocument();

            const gotItBtn = screen.getByRole('button', { name: /Got It/i });
            await user.click(gotItBtn);

            await waitFor(() => {
                expect(screen.queryByText('Manual Charging 💡')).not.toBeInTheDocument();
            });
        });

        it('does not show tutorial dialog when ESS credentials are saved but pause is enabled', async () => {
            const user = userEvent.setup();
            (fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: '',
                hasCredentials: {}
            });
            (updateSettings as any).mockResolvedValue(undefined);

            await navigateToSettings();

            const pauseBtn = await screen.findByRole('button', { name: /^Pause$/i });
            fireEvent.click(pauseBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Resume$/i })).toBeInTheDocument();
            });

            const selectEl = await screen.findByRole('combobox', { name: /System Type/i });
            await user.click(selectEl);
            const teslaOption = await screen.findByRole('option', { name: 'Tesla' });
            await user.click(teslaOption);

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            const linkBtn = await screen.findByRole('button', { name: /Login to link account/i });
            await user.click(linkBtn);

            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'mock-code', state: 'site1' }
                }));
            });

            await waitFor(() => {
                expect(screen.getByText('Received code! Save Settings below to complete.')).toBeInTheDocument();
            });

            windowOpenSpy.mockRestore();



            (updateSettings as any).mockResolvedValue(undefined);

            const saveBtn = screen.getByText('Save Settings');
            await user.click(saveBtn);

            await waitFor(() => {
                expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            });
            expect(screen.queryByText('Welcome to RateRudder! 🚀')).not.toBeInTheDocument();
        });

        it('shows Advanced button when in production release without ?variable=true', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                minBatterySOCPeriods: undefined,
            });
            await navigateToSettings();

            expect(screen.getByRole('button', { name: /^Advanced$/i })).toBeInTheDocument();
        });

        it('allows editing existing custom or rate-based schedule even in production release without ?variable=true', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'Peak' },
                { name: 'Off-Peak' }
            ]);
            await navigateToSettings();

            const changeBtn = screen.getByRole('button', { name: /^Change$/i });
            fireEvent.click(changeBtn);

            await waitFor(() => {
                expect(screen.getAllByLabelText(/Peak Reserve %/i)[0]).toBeInTheDocument();
                expect(screen.getAllByLabelText(/Off-Peak Reserve %/i)[0]).toBeInTheDocument();
                expect(screen.getByRole('button', { name: /^Revert to Simple$/i })).toBeInTheDocument();
            });
        });

        it('configures rate period reserve schedule by default when Advanced button clicked', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'On-Peak' },
                { name: 'Off-Peak' }
            ]);
            await navigateToSettings();

            const configBtn = await screen.findByRole('button', { name: /^Advanced$/i });
            fireEvent.click(configBtn);

            await waitFor(() => {
                expect(screen.getByLabelText(/On-Peak Reserve %/i)).toBeInTheDocument();
                expect(screen.getByLabelText(/Off-Peak Reserve %/i)).toBeInTheDocument();
            });

            // Header Advanced button disappears when editing
            expect(screen.queryByRole('button', { name: /^Advanced$/i })).not.toBeInTheDocument();
            expect(screen.getByRole('button', { name: /^Done$/i })).toBeInTheDocument();
            expect(screen.getByRole('button', { name: /^Revert to Simple$/i })).toBeInTheDocument();
        });

        it('switches to custom time reserve schedule when button is clicked', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'On-Peak' },
                { name: 'Off-Peak' }
            ]);
            await navigateToSettings();

            const configBtn = await screen.findByRole('button', { name: /^Advanced$/i });
            fireEvent.click(configBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Custom Mode$/i })).toBeInTheDocument();
            });

            const switchCustomBtn = screen.getByRole('button', { name: /^Custom Mode$/i });
            fireEvent.click(switchCustomBtn);

            await waitFor(() => {
                expect(screen.getByText('From:')).toBeInTheDocument();
                expect(screen.getByText('To:')).toBeInTheDocument();
            });
        });

        it('reverts to simple reserve schedule when Revert to Simple button is clicked', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([]);
            await navigateToSettings();

            const configBtn = await screen.findByRole('button', { name: /^Advanced$/i });
            fireEvent.click(configBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Revert to Simple$/i })).toBeInTheDocument();
            });

            const revertBtn = screen.getByRole('button', { name: /^Revert to Simple$/i });
            fireEvent.click(revertBtn);

            await waitFor(() => {
                expect(screen.getByLabelText(/Minimum Battery %/i)).toBeInTheDocument();
            });
        });

        it('toggles pause and immediately updates settings when Pause/Resume button is clicked', async () => {
            await navigateToSettings();
            const pauseBtn = await screen.findByRole('button', { name: /^Pause$/i });
            fireEvent.click(pauseBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalled();
                expect(screen.getByRole('button', { name: /^Resume$/i })).toBeInTheDocument();
            });

            const resumeBtn = screen.getByRole('button', { name: /^Resume$/i });
            fireEvent.click(resumeBtn);

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /^Pause$/i })).toBeInTheDocument();
            });
        });

        it('skips straight to custom mode without error when utility has no rate periods', async () => {
            (api.fetchUtilities as any).mockResolvedValue([
                { id: 'pg_e', name: 'Pacific Gas & Electric (PG&E)', rates: [{ id: 'pg_e_e_tou_c', name: 'E-TOU-C' }] },
                { id: 'comed', name: 'Commonwealth Edison (ComEd)', rates: [{ id: 'comed_besh', name: 'BESH' }] },
            ]);
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
                utilityProvider: 'pg_e',
                utilityRate: 'pg_e_e_tou_c',
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            });
            (api.fetchUtilityPeriods as any).mockImplementation((_siteID?: string, provider?: string) => {
                if (provider === 'comed') {
                    return Promise.resolve([]);
                }
                return Promise.resolve([
                    { name: 'Peak' },
                    { name: 'Off-Peak' },
                ]);
            });

            await navigateToSettings();

            const user = userEvent.setup();
            const editUtilBtn = screen.getByRole('button', { name: 'Change Utility Service' });
            await user.click(editUtilBtn);

            const utilInput = await screen.findByRole('combobox', { name: /Utility Provider/i });
            await user.click(utilInput);

            const item = await screen.findByRole('option', { name: /ComEd/i });
            await user.click(item);

            await waitFor(() => {
                expect(api.fetchUtilityPeriods).toHaveBeenCalledWith(
                    expect.anything(),
                    'comed',
                    expect.anything(),
                    expect.anything()
                );
            });

            await waitFor(() => {
                expect(screen.queryByTestId('battery-period-error')).not.toBeInTheDocument();
                expect(screen.getByText(/Custom Hours/i)).toBeInTheDocument();
            });

            const summaryBtn = screen.getByRole('button', { name: 'Edit Battery settings' });
            fireEvent.click(summaryBtn);

            await waitFor(() => {
                expect(screen.getByText('From:')).toBeInTheDocument();
                expect(screen.getByText('To:')).toBeInTheDocument();
                expect(screen.queryByRole('button', { name: /^Rates Mode$/i })).not.toBeInTheDocument();
            });
        });

        it('updates period labels automatically when changing to utility with different period names (e.g. FPL On-Peak/Off-Peak)', async () => {
            (api.fetchUtilities as any).mockResolvedValue([
                { id: 'pg_e', name: 'Pacific Gas & Electric (PG&E)', rates: [{ id: 'pg_e_e_tou_c', name: 'E-TOU-C' }] },
                { id: 'fpl', name: 'Florida Power & Light (FPL)', rates: [{ id: 'fpl_tou', name: 'FPL TOU' }] },
            ]);
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                utilityProvider: 'pg_e',
                utilityRate: 'pg_e_e_tou_c',
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            });
            (api.fetchUtilityPeriods as any).mockImplementation((_siteID?: string, provider?: string) => {
                if (provider === 'fpl') {
                    return Promise.resolve([
                        { name: 'On-Peak' },
                        { name: 'Off-Peak' },
                    ]);
                }
                return Promise.resolve([
                    { name: 'Peak' },
                    { name: 'Off-Peak' },
                ]);
            });

            await navigateToSettings();

            const user = userEvent.setup();
            const editUtilBtn = screen.getByRole('button', { name: 'Change Utility Service' });
            await user.click(editUtilBtn);

            const utilInput = await screen.findByRole('combobox', { name: /Utility Provider/i });
            await user.click(utilInput);

            const item = await screen.findByRole('option', { name: /Florida Power & Light/i });
            await user.click(item);

            await waitFor(() => {
                expect(screen.getByText(/On-Peak: 50%/i)).toBeInTheDocument();
                expect(screen.getByText(/Off-Peak: 20%/i)).toBeInTheDocument();
            });
        });

        it('opens help dialog when help button next to Minimum Battery % is clicked', async () => {
            const user = userEvent.setup();
            await navigateToSettings();

            const helpBtns = screen.getAllByRole('button', { name: /more info/i });
            const helpBtn = helpBtns[0];
            expect(helpBtn).toBeInTheDocument();

            await user.click(helpBtn);

            expect(await screen.findByRole('dialog')).toBeInTheDocument();
            expect(screen.getByText(/Sets the minimum state-of-charge \(SOC\) level/i)).toBeInTheDocument();
        });

        it('opens help dialog when help button in variable reserve schedule section (advanced mode) is clicked', async () => {
            const user = userEvent.setup();
            const settingsWithPeriods = {
                ...defaultSettings,
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'On-Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            };
            (api.fetchSettings as any).mockResolvedValue(settingsWithPeriods);
            await navigateToSettings();

            expect(screen.getByText(/Variable Reserve Schedule/i)).toBeInTheDocument();
            const batterySection = screen.getByTestId('battery-section');
            const editBtn = within(batterySection).getByText('Change');
            await user.click(editBtn);

            const helpBtns = screen.getAllByRole('button', { name: /more info/i });
            const helpBtn = helpBtns[0];
            expect(helpBtn).toBeInTheDocument();

            await user.click(helpBtn);

            expect(await screen.findByRole('dialog')).toBeInTheDocument();
            expect(screen.getByText(/Rate Period Reserve Schedule/i)).toBeInTheDocument();
        });

        it('clearing variable SOC input does not reset to 0 and allows entering new numbers cleanly (rate periods mode)', async () => {
            const settingsWithPeriods = {
                ...defaultSettings,
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 50 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                ],
            };
            (api.fetchSettings as any).mockResolvedValue(settingsWithPeriods);
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'Peak' },
                { name: 'Off-Peak' }
            ]);
            (api.updateSettings as any).mockResolvedValue(undefined);
            await navigateToSettings();

            const batterySection = screen.getByTestId('battery-section');
            const editBtn = within(batterySection).getByText('Change');
            fireEvent.click(editBtn);

            await waitFor(() => {
                expect(screen.getAllByLabelText(/Peak Reserve %/i)[0]).toBeInTheDocument();
            });

            const peakInput = screen.getAllByLabelText(/Peak Reserve %/i)[0];
            expect(peakInput).toHaveValue(50);

            // Clear the input
            fireEvent.change(peakInput, { target: { value: '' } });
            expect(peakInput).toHaveValue(null);

            // Type 25
            fireEvent.change(peakInput, { target: { value: '25' } });
            expect(peakInput).toHaveValue(25);

            // Save and verify settings payload
            const saveBtn = screen.getByText('Save Settings');
            fireEvent.click(saveBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledWith(
                    expect.objectContaining({
                        minBatterySOC: 20,
                        minBatterySOCPeriods: [
                            { utilityPeriodName: 'Peak', minBatterySOC: 25 },
                            { utilityPeriodName: 'Off-Peak', minBatterySOC: 20 },
                        ],
                    }),
                    expect.any(String),
                    undefined
                );
            });
        });

        it('clearing variable SOC input does not reset to 0 and allows entering new numbers cleanly (custom hours mode)', async () => {
            const settingsWithPeriods = {
                ...defaultSettings,
                minBatterySOCPeriods: [
                    { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: 50 },
                ],
            };
            (api.fetchSettings as any).mockResolvedValue(settingsWithPeriods);
            (api.fetchUtilityPeriods as any).mockResolvedValue([]);
            (api.updateSettings as any).mockResolvedValue(undefined);
            await navigateToSettings();

            const batterySection = screen.getByTestId('battery-section');
            const editBtn = within(batterySection).getByText('Change');
            fireEvent.click(editBtn);

            await waitFor(() => {
                expect(screen.getByText('SOC %:')).toBeInTheDocument();
            });

            const socInput = screen.getByDisplayValue('50');
            expect(socInput).toHaveValue(50);

            // Clear the input
            fireEvent.change(socInput, { target: { value: '' } });
            expect(socInput).toHaveValue(null);

            // Type 25
            fireEvent.change(socInput, { target: { value: '25' } });
            expect(socInput).toHaveValue(25);

            // Save and verify settings payload
            const saveBtn = screen.getByText('Save Settings');
            fireEvent.click(saveBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledWith(
                    expect.objectContaining({
                        minBatterySOC: 25,
                        minBatterySOCPeriods: [
                            { hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 24, minuteEnd: 0 }], minBatterySOC: 25 },
                        ],
                    }),
                    expect.any(String),
                    undefined
                );
            });
        });

        it('always sets minBatterySOC to the lowest SOC across all configured reserve periods when saving or pausing', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                minBatterySOC: 10,
                minBatterySOCPeriods: [
                    { utilityPeriodName: 'Peak', minBatterySOC: 55 },
                    { utilityPeriodName: 'Off-Peak', minBatterySOC: 35 },
                ],
            });
            (api.fetchUtilityPeriods as any).mockResolvedValue([
                { name: 'Peak' },
                { name: 'Off-Peak' },
            ]);
            (api.updateSettings as any).mockResolvedValue(undefined);
            await navigateToSettings();

            // Pause should send minBatterySOC = 35 (lowest of 55 and 35) even without editing the periods
            const pauseBtn = await screen.findByRole('button', { name: /^Pause$/i });
            fireEvent.click(pauseBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledWith(
                    expect.objectContaining({
                        pause: true,
                        minBatterySOC: 35,
                        minBatterySOCPeriods: [
                            { utilityPeriodName: 'Peak', minBatterySOC: 55 },
                            { utilityPeriodName: 'Off-Peak', minBatterySOC: 35 },
                        ],
                    }),
                    expect.any(String)
                );
            });
        });

        it('passes unsaved utility selections to fetchUtilityPeriods and clears periods when rate is empty', async () => {
            const user = userEvent.setup();
            (api.fetchUtilities as any).mockResolvedValue([
                {
                    id: 'comed',
                    name: 'Commonwealth Edison (ComEd)',
                    rates: [{ id: 'comed_besh', name: 'Hourly Pricing Program' }],
                },
                {
                    id: 'pg_e',
                    name: 'Pacific Gas & Electric (PG&E)',
                    rates: [
                        { id: 'pg_e_e_1', name: 'E-1 Flat' },
                        { id: 'pg_e_e_tou_c', name: 'E-TOU-C' },
                    ],
                },
            ]);
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
                utilityProvider: 'pg_e',
                utilityRate: 'pg_e_e_tou_c',
                utilityRateOptions: {},
            });
            (api.fetchUtilityPeriods as any).mockImplementation(
                (_siteID?: string, provider?: string, rate?: string) => {
                    if (provider === 'pg_e' && rate === 'pg_e_e_tou_c') {
                        return Promise.resolve([
                            { name: 'On-Peak' },
                            { name: 'Off-Peak' },
                        ]);
                    }
                    return Promise.resolve([]);
                }
            );

            await navigateToSettings();

            // Wait for initial periods load
            await waitFor(() => {
                expect(api.fetchUtilityPeriods).toHaveBeenCalledWith('site1', 'pg_e', 'pg_e_e_tou_c', {});
            });

            // Switch to ComEd, then back to PG&E (which has multiple rates, so utilityRate becomes '')
            await user.click(screen.getByRole('button', { name: 'Change Utility Service' }));
            await user.click(await screen.findByRole('combobox', { name: /Utility Provider/i }));
            await user.click(await screen.findByRole('option', { name: /ComEd/i }));

            await user.click(await screen.findByRole('combobox', { name: /Utility Provider/i }));
            await user.click(await screen.findByRole('option', { name: /Pacific Gas & Electric/i }));

            (api.fetchUtilityPeriods as any).mockClear();

            // Clicking Advanced with unsaved provider='pg_e' and empty rate='' passes unsaved selections and opens Custom Mode without Rates Mode
            const advancedBtn = await screen.findByRole('button', { name: /^Advanced$/i });
            await user.click(advancedBtn);

            await waitFor(() => {
                expect(api.fetchUtilityPeriods).toHaveBeenCalledWith('site1', 'pg_e', '', {});
                expect(screen.getByText('From:')).toBeInTheDocument();
                expect(screen.queryByRole('button', { name: /^Rates Mode$/i })).not.toBeInTheDocument();
            });

            // Selecting E-TOU-C fetches periods for unsaved rate and reveals Rates Mode button
            await user.click(await screen.findByRole('combobox', { name: /Rate\/Plan/i }));
            await user.click(await screen.findByRole('option', { name: 'E-TOU-C' }));

            const ratesModeBtn = await screen.findByRole('button', { name: /^Rates Mode$/i });
            await user.click(ratesModeBtn);

            await waitFor(() => {
                expect(screen.getByLabelText(/On-Peak Reserve %/i)).toBeInTheDocument();
                expect(screen.getByLabelText(/Off-Peak Reserve %/i)).toBeInTheDocument();
            });
        });
    });

    describe('EV Charging Battery Protection', () => {
        it('is visible by default in production', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                evChargingStandby: undefined,
            });
            await navigateToSettings();
            expect(screen.getByTestId('ev-charging-section')).toBeInTheDocument();
            expect(screen.getByText('Avoid Battery for EV Charging')).toBeInTheDocument();
        });

        it('toggling switch ON enables evChargingStandby and saves without requiring charging hours', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                evChargingStandby: false,
            });

            await navigateToSettings();

            const evSwitch = screen.getByRole('switch', { name: /Avoid Battery for EV Charging/i });
            expect(evSwitch).not.toBeChecked();

            await user.click(evSwitch);
            expect(evSwitch).toBeChecked();

            const saveBtn = screen.getByText('Save Settings');
            await user.click(saveBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledWith(
                    expect.objectContaining({
                        evChargingStandby: true,
                    }),
                    expect.any(String),
                    undefined
                );
            });
        });

        it('toggling switch OFF disables evChargingStandby', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                evChargingStandby: true,
            });

            await navigateToSettings();

            const evSwitch = screen.getByRole('switch', { name: /Avoid Battery for EV Charging/i });
            expect(evSwitch).toBeChecked();

            await user.click(evSwitch);

            await waitFor(() => {
                expect(evSwitch).not.toBeChecked();
            });
        });

        it('highlights and scrolls to EV Charging section when visiting /settings?ev=1', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                evChargingStandby: false,
            });
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

            const scrollIntoViewMock = vi.fn();
            window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

            window.history.pushState(null, '', '/settings?ev=1');
            render(<App />);

            await screen.findByRole('heading', { name: /^Settings$/i });

            await waitFor(() => {
                const evSection = screen.getByTestId('ev-charging-section');
                expect(evSection).toHaveClass('highlighted-section');
                expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
            });
        });
    });

    describe('Direct Solar and Battery Export Management', () => {
        it('renders toggle and warning notice when export is enabled for supported ESS', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'tesla',
                hasCredentials: { tesla: true },
                manageTOUSchedules: false,
                release: 'production',
                gridExportSolar: true,
            });

            await navigateToSettings();

            // Open ESS edit mode from summary card
            const editBtn = screen.getByLabelText('Edit Energy Storage System');
            await user.click(editBtn);

            // Toggle should be present
            const exportOptSwitch = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
            expect(exportOptSwitch).not.toBeChecked();
            expect(screen.queryByText(/Notice: This feature requires RateRudder to manage your battery's Time-Of-Use/i)).not.toBeInTheDocument();

            // Click switch to enable
            await user.click(exportOptSwitch);
            expect(exportOptSwitch).toBeChecked();
            expect(screen.getByText(/Notice: This feature requires RateRudder to manage your battery's Time-Of-Use/i)).toBeInTheDocument();

            // Save settings
            const saveBtn = screen.getByRole('button', { name: /Save Settings/i });
            await user.click(saveBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledWith(
                    expect.objectContaining({
                        manageTOUSchedules: true,
                    }),
                    'site1',
                    undefined
                );
            });
        });

        it('renders toggle checked when manageTOUSchedules is already enabled', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: { franklin: true },
                manageTOUSchedules: true,
                release: 'production',
                gridExportBatteries: true,
            });

            await navigateToSettings();

            // Open ESS edit mode from summary card
            const editBtn = screen.getByLabelText('Edit Energy Storage System');
            await user.click(editBtn);

            const exportOptSwitch = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
            expect(exportOptSwitch).toBeInTheDocument();
            expect(exportOptSwitch).toBeChecked();
        });

        it('does not render toggle for unsupported ESS provider', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'enphase',
                hasCredentials: { enphase: true },
                release: 'staging',
                gridExportSolar: true,
            });

            await navigateToSettings();

            // Open ESS edit mode from summary card
            const editBtn = screen.getByLabelText('Edit Energy Storage System');
            await user.click(editBtn);

            expect(screen.queryByRole('switch', { name: /Direct Solar and Battery Export Management/i })).not.toBeInTheDocument();
        });

        it('renders TOU toggle and Enphase notice when enphaseExportSchedules feature flag is enabled', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'enphase',
                hasCredentials: { enphase: true },
                manageTOUSchedules: true,
                release: 'production',
                gridExportSolar: true,
                gridExportBatteries: true,
                features: { enphaseExportSchedules: true },
            });

            await navigateToSettings();

            const editBtn = screen.getByLabelText('Edit Energy Storage System');
            await user.click(editBtn);

            const exportOptSwitch = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
            expect(exportOptSwitch).toBeInTheDocument();
            expect(exportOptSwitch).toBeChecked();
            expect(screen.getByText(/sync your Enphase Electricity Rate/i)).toBeInTheDocument();
        });

        it('is hidden when neither solar nor battery export is enabled, and appears immediately when toggled on', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'tesla',
                hasCredentials: { tesla: true },
                manageTOUSchedules: false,
                release: 'production',
                gridExportSolar: false,
                gridExportBatteries: false,
            });

            await navigateToSettings();

            // Open ESS edit mode from summary card
            const editBtn = screen.getByLabelText('Edit Energy Storage System');
            await user.click(editBtn);

            // Neither export enabled -> should NOT be visible
            expect(screen.queryByRole('switch', { name: /Direct Solar and Battery Export Management/i })).not.toBeInTheDocument();

            // Toggle Export Solar to Grid ON
            const exportSolarSwitch = screen.getByRole('switch', { name: /Export Solar to Grid/i });
            await user.click(exportSolarSwitch);

            // Should immediately pop in without saving, checked by default with TOU notice visible
            const touSwitch = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
            expect(touSwitch).toBeInTheDocument();
            expect(touSwitch).toBeChecked();
            expect(screen.getByText(/Notice: This feature requires RateRudder to manage your battery's Time-Of-Use/i)).toBeInTheDocument();

            // Toggle Export Solar to Grid OFF
            await user.click(exportSolarSwitch);

            // Should disappear
            expect(screen.queryByRole('switch', { name: /Direct Solar and Battery Export Management/i })).not.toBeInTheDocument();

            // Toggle Export Battery to Grid ON
            const exportBatterySwitch = screen.getByRole('switch', { name: /Export Battery to Grid/i });
            await user.click(exportBatterySwitch);

            // Should pop in again, checked by default with TOU notice visible
            const touSwitchAfter = screen.getByRole('switch', { name: /Direct Solar and Battery Export Management/i });
            expect(touSwitchAfter).toBeInTheDocument();
            expect(touSwitchAfter).toBeChecked();
            expect(screen.getByText(/Notice: This feature requires RateRudder to manage your battery's Time-Of-Use/i)).toBeInTheDocument();
        });

        it('automatically expands ESS section and highlights TOU toggle when visiting /settings?tou=1', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'tesla',
                hasCredentials: { tesla: true },
                manageTOUSchedules: false,
                release: 'production',
                gridExportSolar: true,
            });
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

            const scrollIntoViewMock = vi.fn();
            window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

            window.history.pushState(null, '', '/settings?tou=1');
            render(<App />);

            await screen.findByRole('heading', { name: /^Settings$/i });

            // ESS section should automatically be expanded without clicking Edit
            const touSwitch = await screen.findByRole('switch', { name: /Direct Solar and Battery Export Management/i });
            expect(touSwitch).toBeInTheDocument();

            // TOU section container should have the highlighted-section class and be scrolled into view
            const touSection = screen.getByTestId('tou-management-section');
            expect(touSection).toHaveClass('highlighted-section');
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
        });

        it('automatically expands and highlights ESS section when visiting /settings?ess=1', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'tesla',
                hasCredentials: { tesla: true },
                release: 'production',
            });
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });

            const scrollIntoViewMock = vi.fn();
            window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

            window.history.pushState(null, '', '/settings?ess=1');
            render(<App />);

            await screen.findByRole('heading', { name: /^Settings$/i });

            // ESS section should automatically be expanded without clicking Edit
            const essSelect = await screen.findByRole('combobox', { name: /System Type/i });
            expect(essSelect).toBeInTheDocument();

            // ESS section container should have the highlighted-section class and be scrolled into view
            const essSection = screen.getByTestId('ess-section');
            expect(essSection).toHaveClass('highlighted-section');
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
        });

        it('automatically expands and highlights utility section on /settings?tariff=1 and persists default 2026 on save', async () => {
            (api.fetchUtilities as any).mockResolvedValue([
                {
                    id: 'sdge',
                    name: 'San Diego Gas & Electric (SDG&E)',
                    rates: [
                        {
                            id: 'sdge_tou_dr1',
                            name: 'Schedule TOU-DR1',
                            options: [
                                {
                                    field: 'generationRate',
                                    name: 'Generation Rate',
                                    type: 'select',
                                    choices: [
                                        { value: 'sdge', name: 'SDG&E Bundled' },
                                        { value: 'sdcp_power_on', name: 'SDCP PowerOn' },
                                    ],
                                    default: 'sdge',
                                },
                                {
                                    field: 'netMeteringScheme',
                                    name: 'Net Metering / Export Scheme',
                                    type: 'select',
                                    choices: [
                                        { value: 'net', name: 'NEM 1.0' },
                                        { value: 'nem2', name: 'NEM 2.0' },
                                        { value: 'sbp', name: 'Solar Billing Plan (NEM 3.0)' },
                                    ],
                                    default: 'sbp',
                                },
                                {
                                    field: 'applicationYear',
                                    name: 'Interconnection Application Year',
                                    type: 'select',
                                    choices: [
                                        { value: '2023', name: '2023' },
                                        { value: '2024', name: '2024' },
                                        { value: '2025', name: '2025' },
                                        { value: '2026', name: '2026' },
                                        { value: '2027', name: '2027+' },
                                    ],
                                    default: '2026',
                                    dependsOn: {
                                        netMeteringScheme: ['sbp'],
                                    },
                                },
                            ],
                        },
                    ],
                },
            ] as any);
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                utilityProvider: 'sdge',
                utilityRate: 'sdge_tou_dr1',
                utilityRateOptions: {
                    generationRate: 'sdcp_power_on',
                    netMeteringScheme: 'sbp',
                },
                ess: 'tesla',
                hasCredentials: { tesla: true },
                release: 'production',
            });
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });
            (updateSettings as any).mockResolvedValue(undefined);

            const scrollIntoViewMock = vi.fn();
            window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

            window.history.pushState(null, '', '/settings?tariff=1');
            render(<App />);

            await screen.findByRole('heading', { name: /^Settings$/i });

            // Utility section should automatically be expanded without clicking Change
            const appYearTrigger = await waitFor(() => {
                const el = document.getElementById('opt-applicationYear');
                expect(el).toBeInTheDocument();
                return el!;
            });

            // Utility section container should have the highlighted-section class and be scrolled into view
            const utilitySection = screen.getByTestId('utility-section');
            expect(utilitySection).toHaveClass('highlighted-section');
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
            expect(appYearTrigger).toHaveTextContent('2026');

            // Clicking Save Settings without changing the dropdown should persist applicationYear: "2026"
            fireEvent.click(screen.getByText('Save Settings'));

            await waitFor(() => {
                expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
                expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                    utilityProvider: 'sdge',
                    utilityRate: 'sdge_tou_dr1',
                    utilityRateOptions: expect.objectContaining({
                        generationRate: 'sdcp_power_on',
                        netMeteringScheme: 'sbp',
                        applicationYear: '2026',
                    }),
                }), expect.any(String), undefined);
            });
        });

        it('replaces legacy APS netMeteringScheme "rcp" with default "rcp_2026" on display and save via ?tariff=1', async () => {
            (api.fetchUtilities as any).mockResolvedValue([
                {
                    id: 'aps',
                    name: 'Arizona Public Service (APS)',
                    rates: [
                        {
                            id: 'aps_tou_e',
                            name: 'Schedule TOU-E (Time-of-Use 4pm-7pm Weekdays)',
                            options: [
                                {
                                    field: 'netMeteringScheme',
                                    name: 'Net Metering / Export Scheme',
                                    type: 'select',
                                    choices: [
                                        { value: 'rcp_2026', name: 'Rate Rider RCP Tranche 2026: Sep 2026 - Aug 2027 ($0.05554/kWh)' },
                                        { value: 'rcp_2025', name: 'Rate Rider RCP Tranche 2025: Sep 2025 - Aug 2026 ($0.06171/kWh)' },
                                        { value: 'net', name: 'Net Energy Metering (Legacy EPR-6 1:1 Retail Credits)' },
                                    ],
                                    default: 'rcp_2026',
                                },
                            ],
                        },
                    ],
                },
            ] as any);
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                utilityProvider: 'aps',
                utilityRate: 'aps_tou_e',
                utilityRateOptions: {
                    netMeteringScheme: 'rcp',
                },
                ess: 'tesla',
                hasCredentials: { tesla: true },
                release: 'production',
            });
            (fetchAuthStatus as any).mockResolvedValue({ ...defaultAuthStatus, loggedIn: true });
            (updateSettings as any).mockResolvedValue(undefined);

            const scrollIntoViewMock = vi.fn();
            window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

            window.history.pushState(null, '', '/settings?tariff=1');
            render(<App />);

            await screen.findByRole('heading', { name: /^Settings$/i });

            const schemeTrigger = await waitFor(() => {
                const el = document.getElementById('opt-netMeteringScheme');
                expect(el).toBeInTheDocument();
                return el!;
            });

            expect(screen.getByTestId('utility-section')).toHaveClass('highlighted-section');
            expect(schemeTrigger).toHaveTextContent('Rate Rider RCP Tranche 2026: Sep 2026 - Aug 2027 ($0.05554/kWh)');

            fireEvent.click(screen.getByText('Save Settings'));

            await waitFor(() => {
                expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
                expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
                    utilityProvider: 'aps',
                    utilityRate: 'aps_tou_e',
                    utilityRateOptions: expect.objectContaining({
                        netMeteringScheme: 'rcp_2026',
                    }),
                }), expect.any(String), undefined);
            });
        });
    });

    describe('Unsaved Changes Confirmation', () => {
        it('prompts with window.confirm when navigating away with unsaved changes and blocks navigation if cancelled', async () => {
            await navigateToSettings();

            const input = await screen.findByLabelText(/Minimum Battery %/i);
            fireEvent.change(input, { target: { value: '35' } });
            expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();

            const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValueOnce(false);

            const dashboardLinks = screen.getAllByRole('link', { name: 'Dashboard' });
            fireEvent.click(dashboardLinks[0]);

            expect(confirmSpy).toHaveBeenCalledWith('You have unsaved changes. Are you sure you want to leave?');
            expect(screen.getByRole('heading', { name: /^Settings$/i })).toBeInTheDocument();
            expect(screen.getByLabelText(/Minimum Battery %/i)).toHaveValue(35);

            // Confirming allows leaving
            confirmSpy.mockReturnValueOnce(true);
            fireEvent.click(dashboardLinks[0]);

            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
            confirmSpy.mockRestore();
        });

        it('does not prompt with window.confirm when navigating away without unsaved changes', async () => {
            await navigateToSettings();

            const confirmSpy = vi.spyOn(window, 'confirm');

            const dashboardLinks = screen.getAllByRole('link', { name: 'Dashboard' });
            fireEvent.click(dashboardLinks[0]);

            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
            expect(confirmSpy).not.toHaveBeenCalled();
            confirmSpy.mockRestore();
        });
    });

    describe('Connected Apps', () => {
        it('is hidden and skips fetching apps when connectedApps feature flag is disabled', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'staging',
                features: { connectedApps: false },
            });
            (api.fetchApps as any).mockResolvedValue([]);

            await navigateToSettings();
            expect(screen.queryByTestId('connected-apps-section')).not.toBeInTheDocument();
            expect(api.fetchApps).not.toHaveBeenCalled();
        });

        it('is visible when connectedApps feature flag is enabled and displays existing apps', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                release: 'production',
                features: { connectedApps: true },
            });
            (api.fetchApps as any).mockResolvedValue([
                { id: 'app-1', siteID: 'site1', type: 2, name: 'Claude', clientDomain: 'claude.ai', tsCreated: '2026-01-01T00:00:00Z' },
            ]);

            await navigateToSettings();
            const section = await screen.findByTestId('connected-apps-section');
            expect(section).toBeInTheDocument();
            expect(within(section).getByTestId('open-connect-ai-btn')).toBeInTheDocument();
            expect(screen.getByText('Claude')).toBeInTheDocument();
            expect(screen.getByText('claude.ai')).toBeInTheDocument();
            expect(screen.getByText('OAuth')).toBeInTheDocument();

            await user.click(within(section).getByTestId('manual-apps-trigger'));
            expect(screen.getByTestId('mcp-server-url')).toHaveTextContent('/mcp');
        });

        it('opens Connect AI dialog and adds a newly created Gemini key to connected apps', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                features: { connectedApps: true },
            });
            (api.fetchApps as any).mockResolvedValue([]);
            (api.createApp as any).mockResolvedValue({
                token: 'rr_gemini_token_456',
                app: { id: 'app-gem', siteID: 'site1', type: 1, name: 'Gemini', tsCreated: '2026-01-02T00:00:00Z' },
            });

            await navigateToSettings();
            const section = await screen.findByTestId('connected-apps-section');
            await user.click(within(section).getByTestId('open-connect-ai-btn'));

            const dialog = await screen.findByTestId('connect-ai-dialog');
            await user.click(within(dialog).getByTestId('connect-ai-option-gemini'));
            await user.click(within(dialog).getByTestId('create-gemini-key-btn'));

            await waitFor(() => {
                expect(api.createApp).toHaveBeenCalledWith('site1', 'Gemini');
                expect(within(dialog).getByTestId('connect-ai-gemini-prompt')).toHaveTextContent('/api/agent/summary?token=rr_gemini_token_456');
                expect(within(section).getByTestId('app-row-app-gem')).toBeInTheDocument();
            });
        });

        it('supports creating an API key manually and revoking an app when connectedApps feature flag is enabled', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                features: { connectedApps: true },
            });
            (api.fetchApps as any).mockResolvedValue([]);
            (api.createApp as any).mockResolvedValue({
                token: 'rr_test_token_123',
                app: { id: 'app-new', siteID: 'site1', type: 1, name: 'My Agent', tsCreated: '2026-01-02T00:00:00Z' },
            });
            (api.revokeApp as any).mockResolvedValue(undefined);

            await navigateToSettings();

            const section = await screen.findByTestId('connected-apps-section');
            expect(section).toBeInTheDocument();

            await user.click(within(section).getByTestId('manual-apps-trigger'));

            const nameInput = within(section).getByLabelText(/New API Key Name/i);
            await user.type(nameInput, 'My Agent');

            const createBtn = within(section).getByRole('button', { name: /Create API Key/i });
            await user.click(createBtn);

            await waitFor(() => {
                expect(api.createApp).toHaveBeenCalledWith('site1', 'My Agent');
                expect(screen.getByTestId('created-app-token')).toHaveTextContent('rr_test_token_123');
                expect(screen.getByTestId('created-app-summary-url')).toHaveTextContent('/api/agent/summary?token=rr_test_token_123');
                expect(screen.getByText('My Agent')).toBeInTheDocument();
                expect(screen.getByText('API Key')).toBeInTheDocument();
            });

            const revokeBtn = within(section).getByRole('button', { name: /Revoke My Agent/i });
            await user.click(revokeBtn);

            await waitFor(() => {
                expect(api.revokeApp).toHaveBeenCalledWith('site1', 'app-new');
                expect(screen.queryByText('My Agent')).not.toBeInTheDocument();
            });
        });
    });

    describe('Minimum Difference Degradation Warnings', () => {
        it('shows Default placeholder when unset (0) and shows profile-aware degradation warnings without mentioning specific floors', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                optimizationProfile: 'balanced',
                gridExportBatteries: true,
                minArbitrageDifferenceDollarsPerKWH: 0,
                minBatteryExportDifferenceDollarsPerKWH: 0,
            });

            await navigateToSettings();

            const advancedBtn = await screen.findByText('Show Advanced Settings');
            fireEvent.click(advancedBtn);

            const arbInput = await screen.findByLabelText(/Minimum Arbitrage Profit/i);
            const exportInput = await screen.findByLabelText(/Battery Export Margin/i);

            expect(arbInput).toHaveAttribute('placeholder', 'Default');
            expect(exportInput).toHaveAttribute('placeholder', 'Default');
            expect(arbInput).toHaveValue(null);
            expect(exportInput).toHaveValue(null);

            expect(screen.queryByTestId('min-arbitrage-warning')).not.toBeInTheDocument();
            expect(screen.queryByTestId('min-battery-export-warning')).not.toBeInTheDocument();

            // Balanced profile floors are $0.02 arbitrage and $0.03 battery export, so values at or above those do not warn
            await user.type(arbInput, '0.02');
            expect(arbInput).toHaveValue(0.02);
            expect(screen.queryByTestId('min-arbitrage-warning')).not.toBeInTheDocument();

            await user.type(exportInput, '0.03');
            expect(exportInput).toHaveValue(0.03);
            expect(screen.queryByTestId('min-battery-export-warning')).not.toBeInTheDocument();

            // Values below Balanced floors show generic degradation warnings without citing dollar thresholds
            fireEvent.change(arbInput, { target: { value: '0.01' } });
            fireEvent.change(exportInput, { target: { value: '0.02' } });
            expect(screen.getByTestId('min-arbitrage-warning')).toHaveTextContent(/battery degradation/i);
            expect(screen.getByTestId('min-arbitrage-warning')).not.toHaveTextContent(/\$/);
            expect(screen.getByTestId('min-battery-export-warning')).toHaveTextContent(/battery degradation/i);
            expect(screen.getByTestId('min-battery-export-warning')).not.toHaveTextContent(/\$/);

            // Clearing the inputs resets to Default (0) without showing warnings
            fireEvent.change(arbInput, { target: { value: '' } });
            fireEvent.change(exportInput, { target: { value: '' } });
            expect(arbInput).toHaveValue(null);
            expect(exportInput).toHaveValue(null);
            expect(screen.queryByTestId('min-arbitrage-warning')).not.toBeInTheDocument();
            expect(screen.queryByTestId('min-battery-export-warning')).not.toBeInTheDocument();
        });
    });

    describe('ESS Auth Failure and Cooldown Warnings', () => {
        it('shows count-free paused auth warning and Auth Failing badge when consecutiveFailures >= 40, and count-based warning for 3..39', async () => {
            (api.fetchSettings as any).mockResolvedValueOnce({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: { franklin: true },
                essAuthStatus: {
                    consecutiveFailures: 40,
                    lastAttempt: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
                },
            });

            await navigateToSettings();

            const pausedWarning = await screen.findByTestId('ess-auth-warning');
            expect(pausedWarning).toHaveTextContent(/Battery authentication is paused\./i);
            expect(pausedWarning).not.toHaveTextContent(/40/);
            expect(pausedWarning).toHaveTextContent(/locking out your account/i);
            expect(screen.getByText('Auth Failing')).toBeInTheDocument();
            expect(screen.queryByTestId('ess-cooldown-warning')).not.toBeInTheDocument();
        });

        it('shows exact failure count when consecutiveFailures is between 3 and 39', async () => {
            (api.fetchSettings as any).mockResolvedValueOnce({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: { franklin: true },
                essAuthStatus: {
                    consecutiveFailures: 5,
                    lastAttempt: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
                },
            });

            await navigateToSettings();

            const authWarning = await screen.findByTestId('ess-auth-warning');
            expect(authWarning).toHaveTextContent(/Battery authentication has failed 5 times in a row\./i);
            expect(screen.getByText('Auth Failing')).toBeInTheDocument();
        });

        it('shows Tesla permissions warning and Auth Failing badge when consecutiveSetFailures is 3..39 and count-free paused copy at >= 40', async () => {
            (api.fetchSettings as any).mockResolvedValueOnce({
                ...defaultSettings,
                ess: 'tesla',
                hasCredentials: { tesla: true },
                essAuthStatus: {
                    consecutiveFailures: 0,
                    consecutiveSetFailures: 10,
                    lastAttempt: new Date().toISOString(),
                },
            });

            await navigateToSettings();

            const authWarning = await screen.findByTestId('ess-auth-warning');
            expect(authWarning).toHaveTextContent(/Applying battery mode changes has failed 10 times in a row/i);
            expect(authWarning).toHaveTextContent(/make sure all permissions \(including Energy Commands\) are checked/i);
            expect(screen.getByText('Auth Failing')).toBeInTheDocument();
        });

        it('shows count-free paused mode update warning when consecutiveSetFailures >= 40 for Tesla', async () => {
            (api.fetchSettings as any).mockResolvedValueOnce({
                ...defaultSettings,
                ess: 'tesla',
                hasCredentials: { tesla: true },
                essAuthStatus: {
                    consecutiveFailures: 0,
                    consecutiveSetFailures: 40,
                    lastAttempt: new Date().toISOString(),
                },
            });

            await navigateToSettings();

            const teslaWarning = await screen.findByTestId('ess-auth-warning');
            expect(teslaWarning).toHaveTextContent(/Applying battery mode changes is paused\./i);
            expect(teslaWarning).not.toHaveTextContent(/40/);
            expect(teslaWarning).toHaveTextContent(/make sure all permissions \(including Energy Commands\) are checked/i);
        });

        it('shows count-free paused mode update warning when consecutiveSetFailures >= 40 for non-Tesla', async () => {
            (api.fetchSettings as any).mockResolvedValueOnce({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: { franklin: true },
                essAuthStatus: {
                    consecutiveFailures: 0,
                    consecutiveSetFailures: 40,
                    lastAttempt: new Date().toISOString(),
                },
            });

            await navigateToSettings();

            const franklinWarning = await screen.findByTestId('ess-auth-warning');
            expect(franklinWarning).toHaveTextContent(/Applying battery mode changes is paused\./i);
            expect(franklinWarning).not.toHaveTextContent(/40/);
            expect(franklinWarning).toHaveTextContent(/check your battery manufacturer app/i);
        });

        it('shows active interactive cooldown warning when consecutiveUserFailures >= 2 within 15 minutes, and hides it when older than 15 minutes', async () => {
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: { franklin: true },
                essAuthStatus: {
                    consecutiveFailures: 2,
                    lastAttempt: new Date(Date.now() - 5000).toISOString(),
                    consecutiveUserFailures: 2,
                    lastUserAttempt: new Date(Date.now() - 5000).toISOString(),
                },
            });

            await navigateToSettings();

            const cooldownWarning = await screen.findByTestId('ess-cooldown-warning');
            expect(cooldownWarning).toHaveTextContent(/Please wait \d+s before trying again/i);
            expect(cooldownWarning).toHaveTextContent(/protect your battery manufacturer account from being locked out/i);
        });

        it('shows retry caution on first interactive failure and cooldown warning on second failure', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: { franklin: true },
                essAuthStatus: {
                    consecutiveFailures: 40,
                    lastAttempt: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
                },
            });
            (api.updateSettings as any).mockRejectedValue(new Error('invalid username or password'));

            await navigateToSettings();

            const editBtn = await screen.findByLabelText('Edit Energy Storage System');
            await user.click(editBtn);

            const emailInput = screen.getByLabelText('Email');
            const passInput = screen.getByLabelText('Password');
            await user.type(emailInput, 'user@example.com');
            await user.type(passInput, 'wrongpass1');

            const saveBtn = screen.getByRole('button', { name: /Save Settings/i });
            await user.click(saveBtn);

            // First interactive failure -> shows retry caution (0s wait)
            const caution = await screen.findByTestId('ess-retry-caution');
            expect(caution).toHaveTextContent(/Double-check your credentials before trying again/i);
            expect(caution).toHaveTextContent(/retry immediately right now/i);

            // Second interactive failure -> shows 30s cooldown warning
            await user.click(saveBtn);
            const cooldown = await screen.findByTestId('ess-cooldown-warning');
            expect(cooldown).toHaveTextContent(/Please wait \d+s before trying again/i);

            // Rate limit error from server includes lockout explanation
            (api.updateSettings as any).mockRejectedValueOnce(new Error('ESS rate limited, try again in 28s'));
            await user.click(saveBtn);
            expect((await screen.findAllByText(/ESS rate limited, try again in 28s\. RateRudder temporarily pauses login attempts/i)).length).toBeGreaterThan(0);

            // Succeeding re-auth on an already-configured ESS clears warnings without opening the initial grid confirmation modal
            (api.updateSettings as any).mockResolvedValueOnce(undefined);
            await user.click(saveBtn);
            await waitFor(() => {
                expect(screen.getByText('Settings saved successfully')).toBeInTheDocument();
            });
            expect(screen.queryByText('Confirm Grid Settings')).not.toBeInTheDocument();
            expect(screen.queryByTestId('ess-auth-warning')).not.toBeInTheDocument();
            expect(screen.queryByTestId('ess-cooldown-warning')).not.toBeInTheDocument();
        });

        it('does not show auth failure or retry warnings for unconfigured ESS or non-ESS save errors', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: { franklin: false },
                essAuthStatus: {
                    consecutiveFailures: 0,
                    lastAttempt: new Date().toISOString(),
                },
            });
            (api.updateSettings as any).mockRejectedValue(new Error('failed to fetch location data for postal code'));

            await navigateToSettings();

            expect(screen.queryByTestId('ess-auth-warning')).not.toBeInTheDocument();

            const emailInput = await screen.findByLabelText('Email');
            const passInput = screen.getByLabelText('Password');
            await user.type(emailInput, 'user@example.com');
            await user.type(passInput, 'validpass');

            const saveBtn = screen.getByRole('button', { name: /Save Settings/i });
            await user.click(saveBtn);

            expect((await screen.findAllByText(/failed to fetch location data for postal code/i)).length).toBeGreaterThan(0);
            expect(screen.queryByTestId('ess-retry-caution')).not.toBeInTheDocument();
            expect(screen.queryByTestId('ess-cooldown-warning')).not.toBeInTheDocument();
            expect(screen.queryByTestId('ess-auth-warning')).not.toBeInTheDocument();
        });
    });

    describe('Unified Draft Guards and Visible Validation', () => {
        it('opens editESS without false dirty state on initial load, then activates unsaved bar and leave guard on credential or OAuth edits, and resets on Discard', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                ess: 'franklin',
                hasCredentials: {},
            });

            await navigateToSettings();

            // Req 2: editESS is open on initial load, but isDirty is false (no unsaved bar, no false leave prompt)
            const emailInput = await screen.findByLabelText('Email');
            expect(emailInput).toBeInTheDocument();
            expect(screen.queryByTestId('unsaved-changes-bar')).not.toBeInTheDocument();

            const confirmSpy = vi.spyOn(window, 'confirm');
            const dashboardLinks = screen.getAllByRole('link', { name: 'Dashboard' });
            fireEvent.click(dashboardLinks[0]);
            await waitFor(() => {
                expect(screen.getByText('Home Usage')).toBeInTheDocument();
            });
            expect(confirmSpy).not.toHaveBeenCalled();

            // Return to Settings and edit ESS credentials (Req 1)
            fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
            const emailInputAgain = await screen.findByLabelText('Email');
            await user.type(emailInputAgain, 'battery@example.com');

            expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();
            // Never write plaintext credentials to storage or history state
            expect(JSON.stringify(window.localStorage)).not.toContain('battery@example.com');
            expect(JSON.stringify(window.sessionStorage)).not.toContain('battery@example.com');
            expect(JSON.stringify(window.history.state)).not.toContain('battery@example.com');

            confirmSpy.mockReturnValueOnce(false);
            fireEvent.click(screen.getAllByRole('link', { name: 'Dashboard' })[0]);
            expect(confirmSpy).toHaveBeenCalledWith('You have unsaved changes. Are you sure you want to leave?');
            expect(screen.getByLabelText('Email')).toHaveValue('battery@example.com');

            // Discard clears essCredentials and hides unsaved bar
            await user.click(screen.getByRole('button', { name: 'Discard' }));
            expect(screen.getByLabelText('Email')).toHaveValue('');
            expect(screen.queryByTestId('unsaved-changes-bar')).not.toBeInTheDocument();

            // Switch to Tesla OAuth and verify handleOAuthLogin activates unsaved bar and Discard resets oauthStatus
            const essSelect = screen.getByLabelText(/ESS Type|System Type/i);
            await user.click(essSelect);
            await user.click(await screen.findByRole('option', { name: 'Tesla' }));

            const windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as any);
            await user.click(screen.getByRole('button', { name: /Login to link account/i }));
            act(() => {
                window.dispatchEvent(new MessageEvent('message', {
                    origin: window.location.origin,
                    data: { type: 'OAUTH_CODE', code: 'oauth-test-code', state: defaultAuthStatus.sites![0].id },
                }));
            });

            await screen.findByText(/Received code! Save Settings below to complete./i);
            expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();

            await user.click(screen.getByRole('button', { name: 'Discard' }));
            expect(screen.queryByTestId('unsaved-changes-bar')).not.toBeInTheDocument();
            expect(screen.queryByText(/Received code!/i)).not.toBeInTheDocument();

            windowOpenSpy.mockRestore();
            confirmSpy.mockRestore();
        });

        it('guards viewSite query changes and browser Back (popstate) navigation when dirty, while skipping prompts during site deletion', async () => {
            const user = userEvent.setup();
            await navigateToSettings();

            const minSocInput = await screen.findByLabelText(/Minimum Battery %/i);
            fireEvent.change(minSocInput, { target: { value: '42' } });
            expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();

            const confirmSpy = vi.spyOn(window, 'confirm');

            // Same-pathname non-viewSite query update does not prompt
            window.history.replaceState(null, '', '/settings?tou=1');
            expect(confirmSpy).not.toHaveBeenCalled();

            // Changing viewSite on /settings prompts and blocks when cancelled
            confirmSpy.mockReturnValueOnce(false);
            window.history.pushState(null, '', '/settings?viewSite=other-site');
            expect(confirmSpy).toHaveBeenCalledTimes(1);
            expect(window.location.search).toBe('?tou=1');
            expect(screen.getByLabelText(/Minimum Battery %/i)).toHaveValue(42);

            // Browser Back (popstate) prompts and restores /settings when cancelled
            confirmSpy.mockReturnValueOnce(false);
            act(() => {
                window.dispatchEvent(new PopStateEvent('popstate'));
            });
            expect(confirmSpy).toHaveBeenCalledTimes(2);
            expect(window.location.pathname).toBe('/settings');
            expect(screen.getByLabelText(/Minimum Battery %/i)).toHaveValue(42);

            // Deleting site skips beforeunload / leave prompts
            const originalLocation = window.location;
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: {
                    ...originalLocation,
                    origin: originalLocation.origin,
                    pathname: '/settings',
                    search: '',
                    hash: '',
                    href: 'http://localhost:3000/settings',
                },
            });
            try {
                await user.click(screen.getByRole('button', { name: 'Delete Site' }));
                const dialog = await screen.findByRole('dialog');
                await user.click(within(dialog).getByRole('button', { name: /^Delete$/i }));

                await waitFor(() => {
                    expect(deleteSite).toHaveBeenCalled();
                });

                const unloadEvent = new Event('beforeunload', { cancelable: true });
                window.dispatchEvent(unloadEvent);
                expect(unloadEvent.defaultPrevented).toBe(false);
            } finally {
                Object.defineProperty(window, 'location', {
                    configurable: true,
                    value: originalLocation,
                });
                confirmSpy.mockRestore();
            }
        });

        it('preserves dirty form drafts and essCredentials when NotificationModal saves or Pause/Resume is toggled', async () => {
            const user = userEvent.setup();
            let serverSettings = {
                ...defaultSettings,
                minBatterySOC: 15,
                pause: false,
                ess: 'franklin',
                hasCredentials: {},
                features: { notifications: true },
                notifications: { gridOutage: false },
            };
            (api.fetchSettings as any).mockImplementation(async () => ({ ...serverSettings }));
            (api.updateSettings as any).mockImplementation(async (next: any) => {
                serverSettings = { ...next };
            });

            await navigateToSettings();

            // Make dirty edits to both form field and ESS credentials
            const minSocInput = await screen.findByLabelText(/Minimum Battery %/i);
            await user.clear(minSocInput);
            await user.type(minSocInput, '55');
            const emailInput = await screen.findByLabelText('Email');
            await user.type(emailInput, 'draft-owner@example.com');
            expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();

            // Req 6: Clicking Pause sends ONLY persisted settings with pause: true (minBatterySOC: 15, not 55)
            const pauseBtn = screen.getByRole('button', { name: 'Pause' });
            await user.click(pauseBtn);

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalledTimes(1);
            });
            expect(api.updateSettings).toHaveBeenLastCalledWith(
                expect.objectContaining({
                    pause: true,
                    minBatterySOC: 15,
                }),
                expect.any(String)
            );
            // Local drafts remain intact and dirty
            expect(screen.getByLabelText(/Minimum Battery %/i)).toHaveValue(55);
            expect(screen.getByLabelText('Email')).toHaveValue('draft-owner@example.com');
            expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();

            // Req 5: Saving NotificationModal reloads parentSettings with updated notifications without overwriting drafts
            const updatedNotifications = {
                users: {
                    'user-1': {
                        priceSpikes: true,
                    },
                },
            };
            (api.updateNotificationSettings as any).mockImplementation(async () => {
                serverSettings = {
                    ...serverSettings,
                    notifications: updatedNotifications as any,
                };
            });
            await user.click(screen.getByTestId('header-bell-btn'));
            const modalSaveBtn = await screen.findByRole('button', { name: /Save Preferences/i });
            await waitFor(() => {
                expect(modalSaveBtn).not.toBeDisabled();
            });
            await user.click(modalSaveBtn);

            await waitFor(() => {
                expect(api.updateNotificationSettings).toHaveBeenCalledTimes(1);
                expect(screen.queryByRole('button', { name: /Save Preferences/i })).not.toBeInTheDocument();
            });
            expect(screen.getByLabelText(/Minimum Battery %/i)).toHaveValue(55);
            expect(screen.getByLabelText('Email')).toHaveValue('draft-owner@example.com');
            expect(screen.getByTestId('unsaved-changes-bar')).toBeInTheDocument();

            // Fill password and save settings -> merged notifications and pause: true are preserved alongside draft edits
            const passInput = screen.getByLabelText('Password');
            await user.type(passInput, 'secret-pass');
            await user.click(screen.getByRole('button', { name: 'Save Changes' }));

            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenLastCalledWith(
                    expect.objectContaining({
                        minBatterySOC: 55,
                        pause: true,
                        notifications: updatedNotifications,
                    }),
                    expect.any(String),
                    {
                        franklin: {
                            username: 'draft-owner@example.com',
                            password: 'secret-pass',
                        },
                    }
                );
            });
        });

        it('keeps Battery open on Done with 24-hour coverage gap, reopens collapsed invalid sections on submit, scrolls to .error-message, and mirrors error in .unsaved-changes-bar', async () => {
            const user = userEvent.setup();
            const scrollSpy = vi.fn();
            const origScrollIntoView = window.HTMLElement.prototype.scrollIntoView;
            window.HTMLElement.prototype.scrollIntoView = scrollSpy;

            try {
                // Load with collapsed custom schedule containing a gap (00:00-12:00 only)
                (api.fetchSettings as any).mockResolvedValue({
                    ...defaultSettings,
                    minBatterySOC: 25,
                    minBatterySOCPeriods: [
                        {
                            hours: [{ hourStart: 0, minuteStart: 0, hourEnd: 12, minuteEnd: 0 }],
                            minBatterySOC: 25,
                        },
                    ],
                });

                await navigateToSettings();

                // Collapsed Battery summary displays inline Invalid Schedule badge
                expect(screen.getByTestId('battery-summary-error-badge')).toHaveTextContent('Invalid Schedule');

                // Make form dirty so docked bar is rendered, while Battery remains collapsed
                const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
                await user.clear(zipInput);
                await user.type(zipInput, '60602');
                const bar = screen.getByTestId('unsaved-changes-bar');

                // Submit via Save Changes -> reopens Battery, scrolls to .error-message, and mirrors error in docked bar
                scrollSpy.mockClear();
                await user.click(within(bar).getByRole('button', { name: 'Save Changes' }));

                expect(await screen.findByTestId('variable-reserve-section')).toBeInTheDocument();
                expect(within(bar).getByTestId('unsaved-bar-error')).toHaveTextContent(
                    /Reserve SOC periods must cover all 24 hours without gaps or overlaps/i
                );
                expect(scrollSpy).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });

                // Clicking Done in Battery while coverage is still invalid keeps Battery open
                const batterySection = screen.getByTestId('battery-section');
                await user.click(within(batterySection).getByRole('button', { name: 'Done' }));
                expect(screen.getByTestId('variable-reserve-section')).toBeInTheDocument();
            } finally {
                window.HTMLElement.prototype.scrollIntoView = origScrollIntoView;
            }
        });

        it('marks ChecklistBanner items complete only from persisted settings (requiring both utilityProvider and utilityRate) after saving', async () => {
            const user = userEvent.setup();
            (api.fetchSettings as any).mockResolvedValue({
                ...defaultSettings,
                countryCode: '',
                postalCode: '',
                utilityProvider: 'comed',
                utilityRate: '',
                ess: '',
                hasCredentials: {},
            });

            await navigateToSettings();

            const banner = await screen.findByTestId('checklist-banner');
            const locationItem = within(banner).getByText('Set Location').closest('.checklist-item')!;
            const utilityItem = within(banner).getByText('Choose Utility').closest('.checklist-item')!;
            const essItem = within(banner).getByText('Connect Battery (ESS)').closest('.checklist-item')!;

            // Initially all three are pending (utilityProvider without utilityRate is still pending)
            expect(locationItem).toHaveClass('pending');
            expect(utilityItem).toHaveClass('pending');
            expect(essItem).toHaveClass('pending');

            // Fill Location and Utility Rate locally WITHOUT saving -> checklist items must remain pending
            const countrySelect = screen.getByRole('combobox', { name: /Country/i });
            await user.click(countrySelect);
            await user.click(await screen.findByRole('option', { name: /United States/i }));
            const zipInput = screen.getByLabelText(/Zip\/Postal Code/i);
            await user.type(zipInput, '60601');

            const rateSelect = screen.getByRole('combobox', { name: /Rate\/Plan/i });
            await user.click(rateSelect);
            await user.click(await screen.findByRole('option', { name: /Hourly Pricing Program \(BESH\)/i }));

            expect(locationItem).toHaveClass('pending');
            expect(utilityItem).toHaveClass('pending');

            // Save -> Location and Utility now turn checked, while ESS remains pending
            await user.click(screen.getByRole('button', { name: 'Save Changes' }));
            await waitFor(() => {
                expect(api.updateSettings).toHaveBeenCalled();
            });

            await waitFor(() => {
                expect(within(screen.getByTestId('checklist-banner')).getByText('Set Location').closest('.checklist-item')).toHaveClass('checked');
                expect(within(screen.getByTestId('checklist-banner')).getByText('Choose Utility').closest('.checklist-item')).toHaveClass('checked');
                expect(within(screen.getByTestId('checklist-banner')).getByText('Connect Battery (ESS)').closest('.checklist-item')).toHaveClass('pending');
            });
        });
    });
});

