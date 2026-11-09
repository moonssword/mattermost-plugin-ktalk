import {csrfFromCookie} from './logic';
import {PLUGIN_ID} from './manifest';

/** An error from the plugin API. A code marks situations the user can fix. */
export class ApiError extends Error {
    constructor(message: string, readonly code: string) {
        super(message);
    }
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
    const response = await fetch(base() + path, {
        method,
        headers,
        credentials: 'include',
        body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
        throw new ApiError(data.error || data.message || `HTTP ${response.status}`, data.code || '');
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
