import React from 'react';
import type {Store} from 'redux';

import {discussPost, getConfig, loadConfig, runCommand, startMeeting} from './actions';
import {addIncoming, meetingsVersion, removeIncoming, type IncomingCall} from './bus';
import CallMenuRoot, {CallDropdownSublabel, HeaderCallButton, callButtonAction} from './components/call_menu';
import IncomingCalls from './components/incoming_call';
import {PersonalKeySetting} from './components/personal_key';
import {AdminStatusPanel, LinkEmbed, PopoverCall, PopoverStatus, Toast} from './components/extras';
import {CameraIcon} from './components/icons';
import MeetingPost from './components/meeting_post';
import RecordingPost from './components/recording_post';
import {rhsActions} from './components/rhs_handle';
import Sidebar from './components/rhs';
import {injectStyles} from './components/styles';
import {tFromState} from './i18n';
import {cleanRecentStatuses, csrfFromCookie, isSpaceLink} from './logic';
import {PLUGIN_ID} from './manifest';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Registry = any;

class Plugin {
    private registry!: Registry;
    private store!: Store;
    private uiIds: string[] = [];
    private settingsAdded = false;

    public async initialize(registry: Registry, store: Store) {
        this.registry = registry;
        this.store = store;
        injectStyles();

        // These stay for everyone. Cards decide on their own what to show, and
        // administrators need the console to turn dev mode off again.
        registry.registerPostTypeComponent('custom_konturtalk_meeting', MeetingPost);
        registry.registerPostTypeComponent('custom_konturtalk_rec', RecordingPost);
        registry.registerRootComponent(Toast);
        registry.registerRootComponent(IncomingCalls);
        registry.registerRootComponent(CallMenuRoot);
        registry.registerAdminConsoleCustomSetting('ConnectionStatus', AdminStatusPanel, {showTitle: true});

        const refresh = async () => {
            await loadConfig();
            this.applyAccess();
        };
        registry.registerWebSocketEventHandler(`custom_${PLUGIN_ID}_config_changed`, refresh);
        registry.registerWebSocketEventHandler(`custom_${PLUGIN_ID}_incoming_call`, (msg: {data: IncomingCall}) => {
            if (getConfig()?.enabled) {
                addIncoming(msg.data);
            }
        });
        registry.registerWebSocketEventHandler(`custom_${PLUGIN_ID}_call_dismissed`, (msg: {data: {meeting_id: string}}) => {
            removeIncoming(msg.data.meeting_id);
        });
        registry.registerWebSocketEventHandler(`custom_${PLUGIN_ID}_meeting_updated`, () => {
            meetingsVersion.set(meetingsVersion.get() + 1);
        });
        registry.registerReconnectHandler(() => {
            refresh();
            meetingsVersion.set(meetingsVersion.get() + 1);
        });

        // Mattermost starts plugins on the login page too, and signing in
        // does not reload the page. So the interface waits for a user and
        // loads again whenever the signed-in user changes.
        const currentUser = () => (store.getState() as {entities: {users: {currentUserId?: string}}}).entities.users.currentUserId || '';
        let userId = currentUser();
        const signedIn = async () => {
            await refresh();
            repairRecentStatuses(store);
            ensureStatusEmoji(store);
        };
        store.subscribe(() => {
            const next = currentUser();
            if (next !== userId) {
                userId = next;
                if (next) {
                    signedIn();
                }
            }
        });
        if (userId) {
            await signedIn();
        }
    }

    /** Dev mode can change while the page is open, so the interface follows it. */
    private applyAccess() {
        const enabled = Boolean(getConfig()?.enabled);
        if (enabled && this.uiIds.length === 0) {
            this.registerInterface();
        } else if (!enabled && this.uiIds.length > 0) {
            this.uiIds.forEach((id) => this.registry.unregisterComponent(id));
            this.uiIds = [];
        }
    }

    private registerInterface() {
        const {registry, store} = this;
        const t = (text: string) => tFromState(store.getState(), text);
        const keep = (id: unknown) => {
            if (typeof id === 'string') {
                this.uiIds.push(id);
            }
        };

        const rhs = registry.registerRightHandSidebarComponent(Sidebar, t('Контур.Толк'));
        keep(rhs.id);
        rhsActions.show = rhs.showRHSPlugin;

        // The App Bar shows this button automatically when it is enabled.
        keep(registry.registerChannelHeaderButtonAction(
            <CameraIcon/>,
            () => store.dispatch(rhs.toggleRHSPlugin),
            t('Контур.Толк'),
            t('Контур.Толк'),
        ));
        keep(registry.registerCallButtonAction(
            <HeaderCallButton/>,
            // Shown when another plugin, such as Calls, also has a call button:
            // the markup and classes match Mattermost's own menu items.
            <button className='style--none call-button-dropdown'>
                <CameraIcon size={16}/>
                <div>
                    <span className='call-button-label'>{t('Встреча в Контур.Толке')}</span>
                    <span className='call-button-dropdown-sublabel'><CallDropdownSublabel/></span>
                </div>
            </button>,
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            (channel: any) => callButtonAction(store, channel?.id),
            <CameraIcon size={16}/>,
            t('Контур.Толк'),
        ));
        // The mark lets the stylesheet find the copy Mattermost draws twice.
        keep(registry.registerChannelIntroButtonAction(
            <CameraIcon
                size={16}
                className='ktk-intro-mark'
            />,
            () => startMeeting(store),
            t('Начать встречу'),
        ));
        keep(registry.registerChannelHeaderMenuAction(t('Запланировать встречу'), () => runCommand(store, '/talk schedule')));
        keep(registry.registerPostDropdownMenuAction(
            t('Обсудить в Толке'),
            (postId: string) => discussPost(store, postId),
            (postId: string) => {
                // A meeting card is already a meeting.
                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                const type: string = (store.getState() as any).entities.posts.posts[postId]?.type || '';
                return !type.startsWith(`custom_${PLUGIN_ID}`);
            },
        ));
        keep(registry.registerPopoverUserAttributesComponent(PopoverStatus));
        keep(registry.registerPopoverUserActionsComponent(PopoverCall));
        keep(registry.registerPostWillRenderEmbedComponent(
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            (embed: any) => isSpaceLink(embed?.url, getConfig()?.space_url || ''),
            LinkEmbed,
            false,
        ));

        if (!this.settingsAdded) {
            this.settingsAdded = true; // The registry offers no way to remove these.
            this.registerUserSettings(t);
        }
    }

    private registerUserSettings(t: (text: string) => string) {
        const option = (value: string, text: string) => ({value, text});
        const config = getConfig();
        this.registry.registerUserSettings?.({
            id: PLUGIN_ID,
            uiName: t('Контур.Толк'),
            sections: [
                {
                    title: 'Вид карточек и панели',
                    settings: [{
                        name: 'style',
                        type: 'radio',
                        default: 'default',
                        options: [
                            option('default', 'По умолчанию (выбирает администратор)'),
                            option('current', 'Системный'),
                            option('cards', 'Карточки'),
                            option('compact', 'Компактный'),
                        ],
                    }],
                },
                {
                    title: 'Показывать в статусе Mattermost, что я на встрече',
                    settings: [{
                        name: 'meeting_status',
                        type: 'radio',
                        default: 'true',
                        options: [option('true', 'Да'), option('false', 'Нет')],
                    }],
                },
                {
                    title: 'Как открывать встречу',
                    settings: [{
                        name: 'open_mode',
                        type: 'radio',
                        default: 'tab',
                        options: [option('tab', 'В новой вкладке'), option('window', 'В отдельном окне')],
                    }],
                },
                {
                    title: 'Имена комнат',
                    settings: [{
                        name: 'naming',
                        type: 'radio',
                        default: config?.naming || 'words',
                        options: [
                            option('words', 'Случайные слова'),
                            option('uuid', 'UUID'),
                            option('context', 'Команда и канал'),
                            option('ask', 'Спрашивать при создании'),
                        ],
                    }],
                },
                {
                    title: 'Сообщать в личные сообщения о готовности записи',
                    settings: [{
                        name: 'notify_recording',
                        type: 'radio',
                        default: 'false',
                        options: [option('true', 'Да'), option('false', 'Нет')],
                    }],
                },
                {
                    title: 'Принимать звонки из Mattermost',
                    settings: [{
                        name: 'accept_calls',
                        type: 'radio',
                        default: 'true',
                        options: [option('true', 'Да'), option('false', 'Нет')],
                    }],
                },
                // Shown only when the administrator allows personal keys.
                ...(config?.personal_keys ? [{
                    title: 'Личный ключ Толка',
                    settings: [{
                        name: 'personal_key',
                        type: 'custom',
                        component: PersonalKeySetting,
                    }],
                }] : []),
            ],
        });
    }
}

// Fixes the recent statuses version 0.4.0 left behind. Without this the
// Mattermost status dialog of an affected user stays broken.
function repairRecentStatuses(store: Store) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const state = store.getState() as any;
    const userId = state.entities.users.currentUserId;
    const pref = state.entities.preferences?.myPreferences?.['custom_status--recent_custom_statuses'];
    const cleaned = cleanRecentStatuses(pref?.value);
    if (!userId || cleaned === null) {
        return;
    }
    const value = {user_id: userId, category: 'custom_status', name: 'recent_custom_statuses', value: cleaned};
    store.dispatch({type: 'RECEIVED_PREFERENCES', data: [value]});
    fetch(`${window.basename || ''}/api/v4/users/${userId}/preferences`, {
        method: 'PUT',
        headers: {'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-Token': csrfFromCookie(document.cookie)},
        body: JSON.stringify([value]),
    }).catch(() => undefined);
}

// Registers the :konturtalk: emoji the meeting status uses. The plugin server
// cannot create emoji, so a system administrator's client does it once.
async function ensureStatusEmoji(store: Store) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const state = store.getState() as any;
    const me = state.entities.users.profiles[state.entities.users.currentUserId];
    const customEmoji = state.entities.general?.config?.EnableCustomEmoji === 'true';
    if (!me || !(me.roles || '').includes('system_admin') || !customEmoji) {
        return;
    }
    const base = window.basename || '';
    const headers = {'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-Token': csrfFromCookie(document.cookie)};
    try {
        const existing = await fetch(`${base}/api/v4/emoji/name/${PLUGIN_ID}`, {headers});
        if (existing.ok) {
            return;
        }
        const picture = await fetch(`${base}/plugins/${PLUGIN_ID}/status-emoji.png`);
        if (!picture.ok) {
            return;
        }
        const form = new FormData();
        form.append('image', await picture.blob(), 'konturtalk.png');
        form.append('emoji', JSON.stringify({name: PLUGIN_ID, creator_id: me.id}));
        await fetch(`${base}/api/v4/emoji`, {method: 'POST', headers, body: form});
    } catch {
        // The standard camera emoji stays in use.
    }
}

window.registerPlugin(PLUGIN_ID, new Plugin());

export default React;
