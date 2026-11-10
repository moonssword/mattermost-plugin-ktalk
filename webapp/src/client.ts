import {csrfFromCookie} from './logic';
import {PLUGIN_ID} from './manifest';

/** An error from the plugin API. A code marks situations the user can fix. */
export class ApiError extends Error {
    constructor(message: string, readonly code: string) {
        super(message);
    }
}

/** Marks a request that never reached the plugin: offline, a restart, a proxy. */
export const NETWORK_ERROR = 'network';

export function isNetworkError(error: unknown): boolean {
    return (error as ApiError)?.code === NETWORK_ERROR;
}

function base(): string {
    return window.basename || '';
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {
        'X-Requested-With': 'XMLHttpRequest',
        'X-CSRF-Token': csrfFromCookie(document.cookie),
    };
    if (body !== undefined) {
        headers['Content-Type'] = 'application/json';
    }
    let response: Response;
    try {
        response = await fetch(base() + path, {
            method,
            headers,
            credentials: 'include',
            body: body === undefined ? undefined : JSON.stringify(body),
        });
    } catch {
        // The browser says only "Failed to fetch": the server restarted, the
        // connection dropped or the computer woke from sleep.
        throw new ApiError('No connection to the server.', NETWORK_ERROR);
    }
    const data = await response.json().catch(() => null);
    if (!response.ok && data === null && response.status >= 502 && response.status <= 504) {
        // A proxy answered for a server that is restarting.
        throw new ApiError(`HTTP ${response.status}`, NETWORK_ERROR);
    }
    if (!response.ok) {
        throw new ApiError(data?.error || data?.message || `HTTP ${response.status}`, data?.code || '');
    }
    return data as T;
}

export function api<T>(method: string, path: string, body?: unknown): Promise<T> {
    return request<T>(method, `/plugins/${PLUGIN_ID}/api/v1${path}`, body);
}

/** Runs a slash command, which is the only way to get a trigger for a dialog. */
export type CommandResponse = {trigger_id?: string; text?: string};

export function executeCommand(command: string, channelId: string, teamId: string, rootId = ''): Promise<CommandResponse> {
    return request<CommandResponse>('POST', '/api/v4/commands/execute', {command, channel_id: channelId, team_id: teamId, root_id: rootId});
}
