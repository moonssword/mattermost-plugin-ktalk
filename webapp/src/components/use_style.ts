import {useSelector} from 'react-redux';
import {useSyncExternalStore} from 'react';

import {configStore} from '../bus';
import {effectiveStyle} from '../logic';
import {PLUGIN_ID} from '../manifest';
import type {PluginConfig, VisualStyle} from '../types';

export function useConfig(): PluginConfig | null {
    return useSyncExternalStore(configStore.subscribe, configStore.get);
}

/** Reads the user's choice from preferences, so a change applies right away. */
export function useVisualStyle(): VisualStyle {
    const config = useConfig();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const preference = useSelector((state: any) => state.entities?.preferences?.myPreferences?.[`pp_${PLUGIN_ID}--style`]?.value);
    return effectiveStyle(preference, config?.style);
}
