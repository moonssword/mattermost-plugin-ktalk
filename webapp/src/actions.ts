import type {Store} from 'redux';

import {configStore, meetingsVersion, rhsRecording, showToast} from './bus';
import {api, executeCommand} from './client';
import {tFromState} from './i18n';
import {isWebUrl} from './logic';
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

/**
 * Browsers block window.open outside of a click, and the meeting address is
 * only known after a request. So the window opens first and gets its address
 * when the answer arrives.
 */
function reserveWindow(): Window | null {
    const features = getConfig()?.open_mode === 'window' ? 'width=1100,height=760' : undefined;
    return window.open('about:blank', '_blank', features);
}

function finishWindow(target: Window | null, url: string) {
    if (target) {
        target.opener = null;
        target.location.href = url;
    } else {
        window.open(url, '_blank', 'noopener');
    }
}

export function openExternal(url: string) {
    if (isWebUrl(url)) {
        finishWindow(reserveWindow(), url);
    }
}

async function withWindow(store: Store, work: () => Promise<{url: string}>) {
    const target = reserveWindow();
    try {
        const {url} = await work();
        if (!isWebUrl(url)) {
            throw new Error(tFromState(store.getState(), 'Не удалось выполнить действие.'));
        }
        finishWindow(target, url);
    } catch (error) {
        target?.close();
        showToast((error as Error).message || tFromState(store.getState(), 'Не удалось выполнить действие.'));
    }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function currentContext(store: Store): {channelId: string; teamId: string} {
    const state = store.getState();
    return {channelId: state.entities.channels.currentChannelId, teamId: state.entities.teams.currentTeamId};
}

/** Starts a meeting. With the "ask" scheme the server shows a dialog instead. */
export async function startMeeting(store: Store, channelId?: string, rootId = '') {
    const context = currentContext(store);
    const channel = channelId || context.channelId;
    if (getConfig()?.naming === 'ask') {
        await runCommand(store, '/talk start', channel, rootId);
        return;
    }
    await withWindow(store, () => api<{url: string; meeting: MeetingView}>('POST', '/meetings', {channel_id: channel, root_id: rootId}));
}

export async function discussPost(store: Store, postId: string) {
    await withWindow(store, () => api<{url: string}>('POST', '/meetings', {post_id: postId}));
}

export async function callUser(store: Store, userId: string) {
    await withWindow(store, () => api<{url: string}>('POST', '/calls', {user_id: userId}));
}

export async function openPersonalRoom(store: Store) {
    await withWindow(store, () => api<{url: string}>('GET', '/personal-room'));
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

export async function meetingAction(id: string, action: string, done?: string) {
    try {
        await api('POST', `/meetings/${id}/${action}`);
        meetingsVersion.set(meetingsVersion.get() + 1);
        if (done) {
            showToast(done);
        }
    } catch (error) {
        showToast((error as Error).message);
    }
}

export function showRecording(store: Store, toggle: unknown, key: string, section: string) {
    rhsRecording.set({key, section});
    store.dispatch(toggle as never);
}
