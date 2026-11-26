import React from 'react';
import { Select } from '@base-ui/react/select';
import type { UserSite } from '../api';

export interface HeaderSiteSelectProps {
    sites: UserSite[];
    selectedSiteID: string;
    onSelectSite: (siteID: string) => void;
    defaultOpen?: boolean;
    onOpenChange?: (open: boolean) => void;
}

const HeaderSiteSelect: React.FC<HeaderSiteSelectProps> = ({
    sites,
    selectedSiteID,
    onSelectSite,
    defaultOpen = false,
    onOpenChange,
}) => {
    return (
        <Select.Root
            value={selectedSiteID}
            defaultOpen={defaultOpen}
            onOpenChange={(open) => onOpenChange?.(open)}
            items={{
                ...Object.fromEntries(sites.map((site) => [site.id, site.name || site.id])),
                ALL: 'Overview',
            }}
            onValueChange={(value) => {
                // Base UI's SelectPositioner can emit null when a previously selected item
                // (e.g. an admin ?viewSite= override) is removed from `sites` in the same render
                // as the controlled `value` updates; ignore non-string/empty values so we don't clear siteID.
                if (typeof value === 'string' && value) {
                    onSelectSite(value);
                }
            }}
        >
            <Select.Trigger className="site-selector-header" aria-label="Select Site">
                <Select.Value />
                <Select.Icon style={{ display: 'flex', alignItems: 'center' }}>
                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                        <path d="M2.5 4.5L6 8L9.5 4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                </Select.Icon>
            </Select.Trigger>
            <Select.Portal>
                <Select.Positioner className="select-positioner">
                    <Select.Popup className="select-popup">
                        <Select.Item className="select-item" value="ALL">
                            <Select.ItemText>Overview</Select.ItemText>
                        </Select.Item>
                        {sites.map((site) => (
                            <Select.Item key={site.id} className="select-item" value={site.id}>
                                <Select.ItemText>{site.name || site.id}</Select.ItemText>
                            </Select.Item>
                        ))}
                    </Select.Popup>
                </Select.Positioner>
            </Select.Portal>
        </Select.Root>
    );
};

export default HeaderSiteSelect;
