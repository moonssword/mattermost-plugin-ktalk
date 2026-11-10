import type {Store} from 'redux';

import {csrfFromCookie} from './logic';
import {PLUGIN_ID} from './manifest';
import type {PluginConfig} from './types';

export interface SettingOption {
    value: string;
    text: string;
}

export interface SettingSection {
    /** The question as Mattermost's settings window shows it. */
    title: string;
    /** A short label for the panel's own page. */
    label: string;
    /** Where the panel groups it. */
    group: SettingGroup;
    name: string;
    default: string;
    options: SettingOption[];
}

export type SettingGroup = 'look' | 'meetings' | 'notices';

/** The panel's groups, in order, with their headings. */
export const SETTING_GROUPS: Array<{id: SettingGroup; title: string}> = [
    {id: 'look', title: 'Внешний вид'},
    {id: 'meetings', title: 'Встречи'},
    {id: 'notices', title: 'Звонки и уведомления'},
];

/** A yes or no choice, shown in the panel as a switch. */
export function isToggle(section: SettingSection): boolean {
    return section.options.length === 2 && section.options.every((item) => item.value === 'true' || item.value === 'false');
}

const option = (value: string, text: string): SettingOption => ({value, text});

/**
 * The user's settings. Mattermost's settings window and the panel's own page
 * are both built from this list and store the same preferences, so the two
 * places never disagree.
 */
export function settingSections(config: PluginConfig | null): SettingSection[] {
    return [
        {
            title: 'Вид карточек и панели',
            label: 'Стиль',
            group: 'look',
            name: 'style',
            default: 'default',
            options: [
                option('default', 'По умолчанию (выбирает администратор)'),
                option('current', 'Системный'),
                option('cards', 'Карточки'),
                option('compact', 'Компактный'),
            ],
        },
        {
            title: 'Показывать в статусе Mattermost, что я на встрече',
            label: 'Статус «На встрече» в Mattermost',
            group: 'meetings',
            name: 'meeting_status',
            default: 'true',
            options: [option('true', 'Да'), option('false', 'Нет')],
        },
        {
            title: 'Как открывать встречу',
            label: 'Открывать встречу',
            group: 'meetings',
            name: 'open_mode',
            default: 'tab',
            options: [option('tab', 'В новой вкладке'), option('window', 'В отдельном окне')],
        },
        {
            title: 'Имена комнат',
            label: 'Имена комнат',
            group: 'meetings',
            name: 'naming',
            default: config?.naming || 'words',
            options: [
                option('words', 'Случайные слова'),
                option('uuid', 'UUID'),
                option('context', 'Команда и канал'),
                option('ask', 'Спрашивать при создании'),
            ],
        },
        {
            title: 'Сообщать в личные сообщения о готовности записи',
            label: 'Сообщать в ЛС о готовой записи',
            group: 'notices',
            name: 'notify_recording',
            default: 'false',
            options: [option('true', 'Да'), option('false', 'Нет')],
        },
        {
            title: 'Принимать звонки из Mattermost',
            label: 'Принимать звонки из Mattermost',
            group: 'notices',
            name: 'accept_calls',
            default: 'true',
            options: [option('true', 'Да'), option('false', 'Нет')],
        },
    ];
}

export const PREFERENCE_CATEGORY = `pp_${PLUGIN_ID}`;

/** Where Mattermost keeps a preference in its store. */
export function preferenceKey(name: string): string {
    return `${PREFERENCE_CATEGORY}--${name}`;
}

/**
 * Saves one setting as Mattermost's settings window does, and puts it into
 * the store at once, so the panel and the cards follow without a reload.
 */
export async function savePreference(store: Store, name: string, value: string): Promise<void> {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const state = store.getState() as any;
    const userId = state.entities.users.currentUserId;
    const preference = {user_id: userId, category: PREFERENCE_CATEGORY, name, value};
    const response = await fetch(`${window.basename || ''}/api/v4/users/${userId}/preferences`, {
        method: 'PUT',
        headers: {'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-Token': csrfFromCookie(document.cookie)},
        body: JSON.stringify([preference]),
    });
    if (!response.ok) {
        throw new Error('preference not saved');
    }
    store.dispatch({type: 'RECEIVED_PREFERENCES', data: [preference]});
}
