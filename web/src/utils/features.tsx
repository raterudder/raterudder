import React, { createContext, useContext } from 'react';

const FeaturesContext = createContext<Record<string, boolean>>({});

export const FeaturesProvider: React.FC<{
    features?: Record<string, boolean>;
    children: React.ReactNode;
}> = ({ features, children }) => (
    <FeaturesContext.Provider value={features ?? {}}>
        {children}
    </FeaturesContext.Provider>
);

// eslint-disable-next-line react-refresh/only-export-components
export const useFeatures = (): Record<string, boolean> => useContext(FeaturesContext);

// eslint-disable-next-line react-refresh/only-export-components
export const useFeatureFlag = (name: string): boolean => {
    const features = useContext(FeaturesContext);
    return Boolean(features[name]);
};
