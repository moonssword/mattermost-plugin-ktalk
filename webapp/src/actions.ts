import type {Store} from 'redux';

import {configStore, keyRequest, meetingsVersion, rhsRecording, showToast} from './bus';
import {api, ApiError, executeCommand} from './client';
import {rhsActions} from './components/rhs_handle';
import {tFromState} from './i18n';
import {isDesktopApp, isWebUrl} from './logic';
import type {MeetingView, PluginConfig} from './types';

export function getConfig(): PluginConfig | null {
    return configStore.get();
}

export async function loadConfig(): Promise<PluginConfig | null> {
    try {
        configStore.set(await api<PluginConfig>('GET', '/config'));
    } catch {
        // Keep what is known: a network blip should not hide the interface.
    }
    return configStore.get();
}

function inDesktopApp(): boolean {
    return isDesktopApp((window as unknown as {desktop?: unknown}).desktop, navigator.userAgent);
}

/**
 * Opens a meeting or recording in the device's browser. The desktop app hands
 * a web address opened this way to the system browser; anything else it keeps
 * in a window of its own, without the camera and microphone the room needs.
 */
function openInBrowser(url: string) {
    window.open(url, '_blank', 'noopener,noreferrer');
}

/**
 * Browsers block window.open outside of a click, and the meeting address is
 * only known after a request. So in a browser the window opens first and gets
 * its address when the answer arrives. The desktop app has no such blocker,
 * and would keep that blank window to itself, so there the address is opened
 * once it is known.
 */
function reserveWindow(): Window | null {
    if (inDesktopApp()) {
        return null;
    }
    const features = getConfig()?.open_mode === 'window' ? 'width=1100,height=760' : undefined;
    return window.open('about:blank', '_blank', features);
}

function finishWindow(target: Window | null, url: string) {
    if (target) {
        target.opener = null;
        target.location.href = url;
    } else {
        openInBrowser(url);
    }
}

export function openExternal(url: string) {
    if (!isWebUrl(url)) {
        return;
    }
    if (inDesktopApp()) {
        openInBrowser(url);
        return;
    }
    finishWindow(reserveWindow(), url);
}

const PERSONAL_KEY_REQUIRED = 'personal_key_required';

/** The store the plugin was started with, for actions that open the sidebar. */
let appStore: Store | null = null;

export function bindStore(store: Store) {
    appStore = store;
}

/**
 * Asks for the personal key in the sidebar, then offers to go on with the
 * action. Returns false when the error is something else.
 */
function askForKey(error: unknown, label: string, proceed: () => void): boolean {
    if ((error as ApiError)?.code !== PERSONAL_KEY_REQUIRED || !appStore) {
        return false;
    }
    requestKey(appStore, label, proceed);
    return true;
}

/** Opens the sidebar on the personal key form for an action. */
export function requestKey(store: Store, label: string, proceed: () => void) {
    rhsRecording.set(null);
    keyRequest.set({label, proceed});
    if (rhsActions.show) {
        store.dispatch(rhsActions.show as never);
    }
}

async function withWindow(store: Store, work: () => Promise<{url: string}>, label = '', again?: () => void) {
    const target = reserveWindow();
    try {
        const {url} = await work();
        if (!isWebUrl(url)) {
            throw new Error(tFromState(store.getState(), 'Не удалось выполнить действие.'));
        }
        finishWindow(target, url);
    } catch (error) {
        target?.close();
        if (again && askForKey(error, label, again)) {
            return;
        }
        showToast((error as Error).message || tFromState(store.getState(), 'Не удалось выполнить действие.'));
    }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function currentContext(store: Store): {channelId: string; teamId: string} {
    const state = store.getState();
    return {channelId: state.entities.channels.currentChannelId, teamId: state.entities.teams.currentTeamId};
}

/**
 * Starts a meeting. In a direct or group chat it rings the others unless
 * linkOnly is set. With the "ask" scheme the server shows a dialog instead.
 */
export async function startMeeting(store: Store, channelId?: string, rootId = '', linkOnly = false) {
    const context = currentContext(store);
    const channel = channelId || context.channelId;
    if (getConfig()?.naming === 'ask') {
        await runCommand(store, linkOnly ? '/talk link' : '/talk start', channel, rootId);
        return;
    }
    await withWindow(
        store,
        () => api<{url: string; meeting: MeetingView}>('POST', '/meetings', {channel_id: channel, root_id: rootId, link_only: linkOnly}),
        tFromState(store.getState(), linkOnly ? 'Начать встречу' : 'Позвонить'),
        () => startMeeting(store, channel, rootId, linkOnly),
    );
}

export async function discussPost(store: Store, postId: string) {
    await withWindow(store, () => api<{url: string}>('POST', '/meetings', {post_id: postId}), tFromState(store.getState(), 'Обсудить в Толке'), () => discussPost(store, postId));
}

export async function callUser(store: Store, userId: string) {
    await withWindow(store, () => api<{url: string}>('POST', '/calls', {user_id: userId}), tFromState(store.getState(), 'Позвонить'), () => callUser(store, userId));
}

export async function openPersonalRoom(store: Store) {
    await withWindow(store, () => api<{url: string}>('GET', '/personal-room'), tFromState(store.getState(), 'Моя комната'), () => openPersonalRoom(store));
}

/**
 * Runs a slash command for a button. Mattermost opens a dialog only when its
 * trigger matches the one in the store, which its own command box saves after
 * the response. A plain request does not, so the dialog arrived and was
 * silently dropped. Saving the arguments and the trigger the same way opens
 * it, whichever comes first: the response or the dialog over the socket.
 */
export async function runCommand(store: Store, command: string, channelId?: string, rootId = '') {
    const context = currentContext(store);
    const channel = channelId || context.channelId;
    try {
        const response = await executeCommand(command, channel, context.teamId, rootId);
        if (response.trigger_id) {
            store.dispatch({type: 'RECEIVED_DIALOG_ARGUMENTS', data: {channel_id: channel}});
            store.dispatch({type: 'RECEIVED_DIALOG_TRIGGER_ID', data: response.trigger_id});
        }
        if (response.text) {
            showToast(response.text);
        }
    } catch (error) {
        showToast((error as Error).message);
    }
}

export async function meetingAction(id: string, action: string, done?: string, body?: unknown) {
    try {
        await api('POST', `/meetings/${id}/${action}`, body);
        meetingsVersion.set(meetingsVersion.get() + 1);
        if (done) {
            showToast(done);
        }
    } catch (error) {
        if (askForKey(error, tFromState(appStore?.getState(), 'Управлять встречей'), () => meetingAction(id, action, done, body))) {
            return;
        }
        showToast((error as Error).message);
    }
}

export function showRecording(store: Store, toggle: unknown, key: string, section: string) {
    rhsRecording.set({key, section});
    store.dispatch(toggle as never);
}
