import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import userEvent from '@testing-library/user-event';
import Dashboard from './Dashboard';
import { Router } from 'wouter';
import * as api from '../api';
import { setupDefaultApiMocks } from '../test/apiMocks';

const { fetchActions, fetchSavings, fetchSettings, ActionReason } = api;

vi.mock('../api');

const renderWithRouter = (component: React.ReactNode) => {
    return render(
        <Router>
            {component}
        </Router>
    );
};

describe('Dashboard', () => {
    beforeEach(() => {
        window.history.replaceState({}, '', '/');
        vi.resetAllMocks();
        setupDefaultApiMocks(api);
    });

    it('renders loading state initially', () => {
        (fetchActions as any).mockReturnValueOnce(new Promise(() => {}));
        renderWithRouter(<Dashboard />);
        expect(screen.getByText('Loading day...')).toBeInTheDocument();
    });

    it('renders actions with reason-based text', async () => {
        const actions = [{
            reason: 'alwaysChargeBelowThreshold',
            description: 'This is a legacy description',
            timestamp: new Date().toISOString(),
            batteryMode: 2, // ChargeAny
            solarMode: 0,
            currentPrice: { dollarsPerKWH: 0.04, tsStart: '', tsEnd: '' },
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show reason-based text, not description
            expect(screen.getByText(/Current price.*\$ 0\.040/)).toBeInTheDocument();
            // Legacy description should NOT be shown
            expect(screen.queryByText('This is a legacy description')).not.toBeInTheDocument();
        });
    });

    it('falls back to description when reason is empty (legacy actions)', async () => {
        const actions = [{
            description: 'This is a test',
            timestamp: new Date().toISOString(),
            batteryMode: 1,
            solarMode: 1,
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            const standbyElements = screen.getAllByText('Hold Battery');
            expect(standbyElements.length).toBeGreaterThan(0);
            expect(screen.getByText('This is a test')).toBeInTheDocument();
        });
    });

    it('renders no actions message when empty', async () => {
        (fetchActions as any).mockResolvedValue([]);
        renderWithRouter(<Dashboard />);
        await waitFor(() => {
            expect(screen.getByText('No actions recorded for this day.')).toBeInTheDocument();
        });
    });

    it('navigates to previous day', async () => {
         const user = userEvent.setup();
         (fetchActions as any).mockResolvedValue([]);
         renderWithRouter(<Dashboard />);

         await waitFor(() => {
             expect(screen.getByText(/Prev/)).toBeInTheDocument();
             expect(screen.getByText(/Prev/)).not.toBeDisabled();
         });

         const prevButton = screen.getByText(/Prev/);
         await user.click(prevButton);

         await waitFor(() => {
             const calls = (fetchActions as any).mock.calls;
             if (calls.length < 2) throw new Error('fetchActions not called twice');
             const lastCall = calls[calls.length - 1];
             const startArg = lastCall[0] as Date;
             const now = new Date();
             const expectedDate = new Date(now);
             expectedDate.setDate(expectedDate.getDate() - 1);
             expect(startArg.getDate()).toBe(expectedDate.getDate());
         });
    });

    it('renders battery SOC in footer', async () => {
        const actions = [{
            reason: ActionReason.AlwaysChargeBelowThreshold,
            description: 'SOC test',
            timestamp: new Date().toISOString(),
            batteryMode: 1,
            solarMode: 1,
            currentPrice: { dollarsPerKWH: 0.05, tsStart: '2026-02-20T19:00:00Z', tsEnd: '' },
            systemStatus: {
                batterySOC: 42.5,
                alarms: [],
                storms: [],
            }
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getAllByText(/Battery/).length).toBeGreaterThan(0);
            expect(screen.getAllByText(/42.5%/).length).toBeGreaterThan(0);
        });
    });

    it('renders dry run badge', async () => {
        const actions = [{
            reason: ActionReason.AlwaysChargeBelowThreshold,
            description: 'Dry run test',
            timestamp: new Date().toISOString(),
            batteryMode: 1, // Standby
            solarMode: 1, // NoExport
            dryRun: true,
            currentPrice: { dollarsPerKWH: 0.01, tsStart: '', tsEnd: '' },
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText('Dry Run')).toBeInTheDocument();
            expect(screen.getByText('Dry Run')).toHaveClass('tag', 'dry-run');
        });
    });

    it('hides no change badges', async () => {
        const actions = [{
            description: 'Mixed modes test',
            timestamp: new Date().toISOString(),
            batteryMode: 0, // NoChange
            solarMode: 1, // NoExport
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Solar mode should be visible
            expect(screen.getByText('Use & No Export')).toBeInTheDocument();
            // Battery mode (NoChange) should NOT be visible as a badge/tag
            const badges = screen.queryAllByText((content, element) => {
                return element !== null && element.classList.contains('tag') && content === 'No Change';
            });
            expect(badges.length).toBe(0);
        });
    });

    it('groups consecutive fault actions into summary', async () => {
        const actions = [
            {
                description: 'Fault 1',
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                fault: true,
                systemStatus: {
                    alarms: [{ name: 'GridFault' }]
                },
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
            },
            {
                description: 'Fault 2',
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                fault: true,
                systemStatus: {
                    alarms: [{ name: 'InverterFault' }]
                },
                currentPrice: { dollarsPerKWH: 0.20, tsStart: '', tsEnd: '' }
            }
        ];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "System Fault" title/header
            expect(screen.getByRole('heading', { name: /System Fault/ })).toBeInTheDocument();
            // Should show alarm names - the order depends on Set iteration, usually insertion order
            // Since we add GridFault then InverterFault, it should be "GridFault, InverterFault"
            // However, regex is safer if order is not guaranteed, but usually it is for Sets of strings added in order.
            expect(screen.getByText(/Alarms: GridFault, InverterFault/)).toBeInTheDocument();
            // Should show count
            expect(screen.getByText('(2x)')).toBeInTheDocument();
        });
    });

    it('groups consecutive no change actions into summary', async () => {
        const actions = [
            {
                description: 'No change 1',
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0, // NoChange
                solarMode: 0, // NoChange
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
            },
            {
                description: 'No change 2',
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.20, tsStart: '', tsEnd: '' }
            }
        ];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "No Change" title/header
            expect(screen.getByRole('heading', { name: /No Change/ })).toBeInTheDocument();
            // Should show the latest action's description/reason
            expect(screen.getByText('No change 2')).toBeInTheDocument();
            expect(screen.queryByText('No change 1')).not.toBeInTheDocument();
            // Should show average price: (0.10 + 0.20) / 2 = 0.15
            expect(screen.getByText(/Avg Price:/)).toBeInTheDocument();
            expect(screen.getByText(/\$ 0.150\/kWh/)).toBeInTheDocument();
            // Should show range: 0.10 - 0.20
            expect(screen.getByText(/Range: \$ 0.100 - \$ 0.200/)).toBeInTheDocument();
            // Should show count in title
            expect(screen.getByText('(2x)')).toBeInTheDocument();
        });
    });

    it('renders daily savings summary', async () => {
        (fetchActions as any).mockResolvedValue([]);
        (fetchSavings as any).mockResolvedValue({
            batterySavings: 5.50,
            solarSavings: 5.00,
            cost: 2.00,
            credit: 1.00,
            avoidedCost: 6.00,
            chargingCost: 0.50,
            solarGenerated: 20,
            gridImported: 10,
            gridExported: 5,
            homeUsed: 25,
            batteryUsed: 11,
        });

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText('Net Savings Today')).toBeInTheDocument();
            // Net savings: 5.50 (battery) + 5.00 (solar) + 1.00 (credit) = 11.50
            expect(screen.getByText(/\$ 11\.50/)).toBeInTheDocument();
            expect(screen.getByText('Solar')).toBeInTheDocument();
            expect(screen.getByText('+ $ 5.00')).toBeInTheDocument();
            expect(screen.getByText('Battery')).toBeInTheDocument();
            expect(screen.getByText('+ $ 5.50')).toBeInTheDocument();
            expect(screen.getByText('Export')).toBeInTheDocument();
            expect(screen.getByText('+ $ 1.00')).toBeInTheDocument();
        });

        await waitFor(() => {
            expect(screen.getByText('Home Usage')).toBeInTheDocument();
            expect(screen.getByText(/25\.0/)).toBeInTheDocument();
            expect(screen.getByText('Solar Gen')).toBeInTheDocument();
            expect(screen.getByText(/20\.0/)).toBeInTheDocument();
            expect(screen.getByText('Battery Use')).toBeInTheDocument();
            expect(screen.getByText(/11\.0/)).toBeInTheDocument();
            expect(screen.getByText(/Grid \(In\/Out\)/)).toBeInTheDocument();
            expect(screen.getByText('10.0')).toBeInTheDocument();
            expect(screen.getByText('5.0')).toBeInTheDocument();
            expect(screen.getAllByText(/kWh/).length).toBeGreaterThanOrEqual(3);
            expect(screen.getByText('Total Credit')).toBeInTheDocument();
            expect(screen.getAllByText('$ 1.00').length).toBeGreaterThanOrEqual(1);
            expect(screen.getByText('Total Cost')).toBeInTheDocument();
            expect(screen.getByText(/\$ 2\.00/)).toBeInTheDocument();
        });
    });

    it('renders negative savings correctly', async () => {
        (fetchActions as any).mockResolvedValue([]);
        (fetchSavings as any).mockResolvedValue({
            batterySavings: -2.50,
            solarSavings: 1.00,
            cost: 10.00,
            credit: -0.50, // Negative credit due to negative price
            avoidedCost: 0.50,
            chargingCost: 3.00,
            solarGenerated: 5,
            gridImported: 20,
            gridExported: 2,
            homeUsed: 23,
            batteryUsed: 5
        });

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Net savings: 1.00 (solar) - 2.50 (battery) - 0.50 (credit) = -2.00
            expect(screen.getByText('Net Savings Today')).toBeInTheDocument();
            const netValue = screen.getByText(/- \$ 2\.00/);
            expect(netValue).toBeInTheDocument();
            expect(netValue).toHaveClass('negative');

            expect(screen.getByText('Solar')).toBeInTheDocument();
            expect(screen.getByText('+ $ 1.00')).toBeInTheDocument();

            expect(screen.getByText('Battery')).toBeInTheDocument();
            const batteryValue = screen.getByText('- $ 2.50');
            expect(batteryValue).toBeInTheDocument();
            expect(batteryValue).toHaveClass('negative');

            expect(screen.getByText('Export')).toBeInTheDocument();
            const exportValues = screen.getAllByText('- $ 0.50');
            expect(exportValues.length).toBe(2);
            expect(exportValues[0]).toHaveClass('negative');
        });
    });


    it('shows banner when ESS credentials are missing', async () => {
        (fetchActions as any).mockResolvedValue([]);
        (fetchSavings as any).mockResolvedValue(null);
        (fetchSettings as any).mockResolvedValue({
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: false },
            utilityProvider: 'comed_besh'
        });

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText(/Energy Storage System is not connected/i)).toBeInTheDocument();
            expect(screen.getByText(/Configure it in Settings/i)).toBeInTheDocument();
        });
    });

    it('does not show banner when ESS credentials are present', async () => {
        (fetchActions as any).mockResolvedValue([]);
        (fetchSavings as any).mockResolvedValue(null);
        (fetchSettings as any).mockResolvedValue({
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: true },
            utilityProvider: 'comed_besh'
        });

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Need to wait for loading to finish
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        expect(screen.queryByText(/Energy Storage System is not connected/i)).not.toBeInTheDocument();
    });

    it('shows banner when Utility Provider is missing', async () => {
        (fetchActions as any).mockResolvedValue([]);
        (fetchSavings as any).mockResolvedValue(null);
        (fetchSettings as any).mockResolvedValue({
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: true },
            utilityProvider: ''
        });

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByText(/Utility Provider is not configured/i)).toBeInTheDocument();
            expect(screen.getByText(/Configure it in Settings/i)).toBeInTheDocument();
        });
    });

    it('does not show banner when Utility Provider is present', async () => {
        (fetchActions as any).mockResolvedValue([]);
        (fetchSavings as any).mockResolvedValue(null);
        (fetchSettings as any).mockResolvedValue({
            minBatterySOC: 10,
            ess: 'franklin',
            hasCredentials: { franklin: true },
            utilityProvider: 'comed_besh'
        });

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        expect(screen.queryByText(/Utility Provider is not configured/i)).not.toBeInTheDocument();

    });

    it('renders manual emergency mode correctly', async () => {
        const actions = [{
            reason: 'emergencyMode',
            description: 'Emergency Mode Active',
            timestamp: new Date().toISOString(),
            batteryMode: 0,
            solarMode: 0,
            fault: true,
            systemStatus: {
                alarms: [],
                storm: []
            },
            currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Emergency Mode/ })).toBeInTheDocument();
            expect(screen.getByText('System manually put into emergency mode. Skipping automation.')).toBeInTheDocument();
        });
    });

    it('renders storm protection mode correctly with times', async () => {
        const stormStart = new Date('2023-01-01T12:00:00');
        const stormEnd = new Date('2023-01-01T15:00:00');
        const actions = [{
            reason: 'emergencyMode',
            description: 'Storm Prep',
            timestamp: new Date('2023-01-01T10:00:00').toISOString(),
            batteryMode: 0,
            solarMode: 0,
            fault: true,
            systemStatus: {
                alarms: [],
                storms: [{
                    description: 'Thunderstorm',
                    tsStart: stormStart.toISOString(),
                    tsEnd: stormEnd.toISOString(),
                }]
            },
            currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' }
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Storm Hedge Mode/ })).toBeInTheDocument();
            expect(screen.getByText('Franklin is charging the battery to prepare for the storm.')).toBeInTheDocument();
            expect(screen.getByText(/Storm Duration: 12:00 PM - 3:00 PM/)).toBeInTheDocument();
        });
    });

    it('hides price footer in summary when no price data is available', async () => {
        const actions = [{
            reason: 'emergencyMode',
            description: 'Emergency Mode Active',
            timestamp: new Date().toISOString(),
            batteryMode: 0,
            solarMode: 0,
            fault: true,
            systemStatus: {
                alarms: [],
                storms: []
            },
            // currentPrice is undefined
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /Emergency Mode/ })).toBeInTheDocument();
            // Price label should not be present
            expect(screen.queryByText('Avg Price:')).not.toBeInTheDocument();
        });
    });

    it('hides CurrentStatus on previous dates', async () => {
        const user = userEvent.setup();
        const now = new Date();
        const yesterday = new Date(now);
        yesterday.setDate(yesterday.getDate() - 1);

        (fetchActions as any).mockResolvedValue([]);
        renderWithRouter(<Dashboard />);

        // Wait for initial load
        await waitFor(() => {
            expect(screen.queryByText('Loading day...')).not.toBeInTheDocument();
        });

        // Click Prev button to go to yesterday
        const prevButton = screen.getByText(/Prev/);
        await user.click(prevButton);

        // Verification of navigation happened
        await waitFor(() => {
            const hasDateParam = window.location.search.includes('date=');
            expect(hasDateParam).toBe(true);
        });

        // Mock actions for yesterday
        const actions = [{
            timestamp: yesterday.toISOString(),
            batteryMode: 0,
            solarMode: 0,
            targetBatteryMode: 2,
            systemStatus: {
                batterySOC: 50,
                batteryKW: -5.0,
            },
            currentPrice: { dollarsPerKWH: 0.05, tsStart: '', tsEnd: '' },
        }];
        (fetchActions as any).mockResolvedValue(actions);

        await waitFor(() => {
            // Should NOT show CurrentStatus card on a non-today date
            expect(document.querySelector('.current-status-card')).not.toBeInTheDocument();
        });
    });

    it('renders CurrentStatus using targetBatteryMode when batteryMode is NoChange', async () => {
        const now = new Date();
        const actions = [{
            timestamp: now.toISOString(),
            batteryMode: 0, // NoChange
            solarMode: 0,
            targetBatteryMode: 2, // ChargeAny
            systemStatus: {
                batterySOC: 50,
                batteryKW: -5.0, // Charging
            },
            currentPrice: { dollarsPerKWH: 0.05, tsStart: '', tsEnd: '' },
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "System Charging" because targetBatteryMode is ChargeAny
            expect(screen.getByText('System Charging')).toBeInTheDocument();

            // Should show the specific label in the CurrentStatus component
            const statusCard = document.querySelector('.current-status-card');
            expect(statusCard).toBeInTheDocument();
            expect(statusCard?.textContent).toContain('Charge From Solar+Grid');
        });
    });

    it('shows pills for targetBatteryMode even when batteryMode is NoChange', async () => {
        const actions = [{
            description: 'Target mode test',
            timestamp: new Date().toISOString(),
            batteryMode: 0, // NoChange
            solarMode: 0,
            targetBatteryMode: -1, // Load
            targetSolarMode: 1, // NoExport
        }];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // Should show "Use Battery" and "No Export" pills
            const useBatteryPills = screen.getAllByText('Use Battery');
            expect(useBatteryPills.some(p => p.classList.contains('tag'))).toBe(true);

            const noExportPills = screen.getAllByText('Use & No Export');
            expect(noExportPills.some(p => p.classList.contains('tag'))).toBe(true);
        });
    });

    it('shows target mode pills in summaries', async () => {
        const actions = [
            {
                description: 'No change 1',
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                targetBatteryMode: 1, // Standby
                targetSolarMode: 1, // NoExport
            },
            {
                description: 'No change 2',
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                targetBatteryMode: 1, // Standby
                targetSolarMode: 1, // NoExport
            }
        ];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /No Change/ })).toBeInTheDocument();
            // Should show pills in the summary item
            const holdBatteryPills = screen.getAllByText('Hold Battery');
            expect(holdBatteryPills.some(p => p.classList.contains('tag'))).toBe(true);

            const noExportPills = screen.getAllByText('Use & No Export');
            expect(noExportPills.some(p => p.classList.contains('tag'))).toBe(true);
        });
    });

    it('renders action summaries with price range, latest battery charge, and info tags', async () => {
        const actions = [
            {
                timestamp: new Date('2023-01-01T10:00:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '2023-01-01T10:00:00Z', tsEnd: '' },
                systemStatus: { batterySOC: 40 },
                deficitAt: '2023-01-01T15:00:00Z',
            },
            {
                timestamp: new Date('2023-01-01T10:30:00').toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.20, tsStart: '2023-01-01T10:30:00Z', tsEnd: '' },
                systemStatus: { batterySOC: 45 },
                deficitAt: '2023-01-01T16:00:00Z',
                capacityAt: '2023-01-01T18:00:00Z',
            }
        ];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /No Change/ })).toBeInTheDocument();

            // Should show average price and range
            expect(screen.getByText(/Avg Price:/)).toBeInTheDocument();
            expect(screen.getByText(/\$ 0\.150\/kWh/)).toBeInTheDocument();
            expect(screen.getByText(/Range: \$ 0\.100 - \$ 0\.200/)).toBeInTheDocument();

            // Should show average SOC and range
            expect(screen.getByText(/Battery:/)).toBeInTheDocument();
            expect(screen.getByText(/42\.5%/)).toBeInTheDocument();
            expect(screen.getByText(/Range: 40% - 45%/)).toBeInTheDocument();

            // Should show Deficit and Capacity tags from the LATEST action
            // Using a more flexible regex for time as it depends on local timezone
            expect(screen.getByText(/Deficit:/)).toBeInTheDocument();
            expect(screen.getByText(/Capacity:/)).toBeInTheDocument();

            // Check that some element contains the Deficit/Capacity text with some time format
            const tags = document.querySelectorAll('.tag-info');
            const tagTexts = Array.from(tags).map(t => t.textContent);
            expect(tagTexts.some(t => t?.includes('Deficit:'))).toBe(true);
            expect(tagTexts.some(t => t?.includes('Capacity:'))).toBe(true);
        });
    });

    it('hides paused actions from the dashboard timeline', async () => {
        const now = new Date();
        const actions = [
            {
                reason: 'sufficientBattery',
                description: 'Normal action',
                timestamp: new Date(now.getTime() - 60000).toISOString(),
                batteryMode: -1, // Load
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.10, tsStart: '', tsEnd: '' },
                paused: false,
            },
            {
                description: 'Automation is paused',
                timestamp: now.toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.12, tsStart: '', tsEnd: '' },
                paused: true,
            }
        ];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // The paused action description should NOT appear in the action list
            expect(screen.queryByText('Automation is paused')).not.toBeInTheDocument();
        });
    });

    it('shows paused indicator in CurrentStatus when last action is paused', async () => {
        const now = new Date();
        const actions = [
            {
                description: 'Automation is paused',
                timestamp: now.toISOString(),
                batteryMode: 0,
                solarMode: 0,
                currentPrice: { dollarsPerKWH: 0.12, tsStart: '', tsEnd: '' },
                systemStatus: {
                    batterySOC: 65,
                    batteryKW: 0,
                },
                paused: true,
            }
        ];
        (fetchActions as any).mockResolvedValue(actions);

        renderWithRouter(<Dashboard />);

        await waitFor(() => {
            // CurrentStatus card should show "System Paused"
            expect(screen.getByText('System Paused')).toBeInTheDocument();
            expect(screen.getByText('Automation is currently paused')).toBeInTheDocument();

            // The status card should have the "paused" CSS class
            const statusCard = document.querySelector('.current-status-card');
            expect(statusCard).toBeInTheDocument();
            expect(statusCard?.classList.contains('paused')).toBe(true);

            // Should still show battery SOC and price
            expect(screen.getAllByText(/65\.0%/).length).toBeGreaterThan(0);
        });
    });


    it('renders only savings in overview mode (siteID=ALL)', async () => {
        (fetchActions as any).mockResolvedValue([{ description: 'Should not show' }]);
        (fetchSavings as any).mockResolvedValue({
            batterySavings: 10,
            solarSavings: 20,
            cost: 5,
            credit: 2,
            avoidedCost: 12,
            chargingCost: 2,
            solarGenerated: 30,
            gridImported: 15,
            gridExported: 5,
            homeUsed: 40,
            batteryUsed: 10,
        });
        (fetchSettings as any).mockResolvedValue({ utilityProvider: 'test' });

        renderWithRouter(<Dashboard siteID="ALL" />);

        await waitFor(() => {
            expect(screen.getByText('Net Savings Today')).toBeInTheDocument();
            expect(screen.getByText(/\$ 32\.00/)).toBeInTheDocument(); // 10+20+2
        });

        // Should NOT show current status or actions
        expect(document.querySelector('.current-status-card')).not.toBeInTheDocument();
        expect(screen.queryByText('Should not show')).not.toBeInTheDocument();
        expect(document.querySelector('.action-list')).not.toBeInTheDocument();
    });
});
