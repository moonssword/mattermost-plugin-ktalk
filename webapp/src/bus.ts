// Small shared stores for state crossing registered components, which Mattermost
// mounts in unrelated parts of its tree.
import type {PluginConfig} from './types';

type Listener = () => void;

function createStore<T>(initial: T) {
    let value = initial;
    const listeners = new Set<Listener>();
    return {
        get: () => value,
        set: (next: T) => {
            value = next;
            listeners.forEach((listener) => listener());
        },
        subscribe: (listener: Listener) => {
            listeners.add(listener);
            return () => {
                listeners.delete(listener);
            };
        },
    };
}

/** The recording the sidebar should show, with the wanted section. */
export const rhsRecording = createStore<{key: string; section: string} | null>(null);

/** Increases on each meeting update, so lists know to reload. */
export const meetingsVersion = createStore(0);

export const toastMessage = createStore<string>('');

export interface IncomingCall {
    meeting_id: string;
    channel_id: string;
    caller_id: string;
    caller_name: string;
    title: string;
    url: string;
    expires_at: string;
}

/** Calls ringing for this user right now. */
export const incomingCalls = createStore<IncomingCall[]>([]);

export function addIncoming(call: IncomingCall) {
    incomingCalls.set([...incomingCalls.get().filter((item) => item.meeting_id !== call.meeting_id), call]);
}

export function removeIncoming(meetingId: string) {
    incomingCalls.set(incomingCalls.get().filter((item) => item.meeting_id !== meetingId));
}

/** The plugin settings for this user, once loaded. */
export const configStore = createStore<PluginConfig | null>(null);

let toastTimer: ReturnType<typeof setTimeout> | undefined;

export function showToast(message: string) {
    toastMessage.set(message);
    if (toastTimer) {
        clearTimeout(toastTimer);
    }
    toastTimer = setTimeout(() => toastMessage.set(''), 6000);
}
