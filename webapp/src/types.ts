export type MeetingStatus = 'scheduled' | 'ringing' | 'waiting' | 'live' | 'ended' | 'cancelled';

export interface Callee {
    user_id: string;
    name: string;
    status: string;
}

export interface Attendee {
    name: string;
    status: string;
    key?: string;
}

export interface MeetingView {
    id: string;
    room_name: string;
    url: string;
    title: string;
    channel_id: string;
    channel_name?: string;
    post_id: string;
    creator_id: string;
    creator_name: string;
    kind: string;
    status: MeetingStatus;
    link_only?: boolean;
    created_at: string;
    started_at?: string | null;
    ended_at?: string | null;
    users_online: number;
    online?: string[] | null;
    pin?: string;
    guests_until?: string | null;
    lobby?: boolean;
    sip?: {sipNumber?: string; sipUri?: string; phoneNumbers?: string[]} | null;
    recording?: boolean;
    expired?: boolean;
    callees?: Callee[];
    recordings?: Array<{key: string; title: string; url: string; duration: number}>;
    results?: {url?: string; attended?: string[]; missed?: string[]; polls?: number; notes?: number; boards?: number} | null;
    schedule?: {
        start: string;
        end: string;
        description?: string;
        repeat?: string;
        repeat_text?: string;
        recurring?: boolean;
        auto_record?: boolean;
        attendees?: Attendee[] | null;
        invited?: string[];
        answers?: Record<string, string>;
        keys?: Record<string, string>;
    };
}

export type VisualStyle = 'current' | 'cards' | 'compact';

export interface PluginConfig {
    enabled: boolean;
    test_mode: boolean;
    style: VisualStyle;
    bot_user_id: string;
    configured: boolean;
    space_url: string;
    link_only: boolean;
    naming: string;
    open_mode: string;
    calls: boolean;
    personal_keys: boolean;
    personal_key: boolean;
    join_opens_min: number;
    recording_access?: string;
}

export interface UpcomingEvent {
    id: string;
    subject: string;
    start: string;
    end: string;
    url: string;
    online: number;
    external?: string;
    recurring?: boolean;
    repeat?: string;
    repeat_text?: string;
    meeting_id?: string;
    channel_id?: string;
}

export interface RecordingItem {
    key: string;
    title: string;
    url: string;
    created_date: string;
    duration: number;
    participants: number;
}

export interface RecordingArtifacts {
    title: string;
    url: string;
    summary: {status: string; text: string};
    protocol: {status: string; text: string};
    transcript: {status: string; lines: Array<{speaker: string; start: number; text: string}>};
}

declare global {
    interface Window {
        registerPlugin(id: string, plugin: unknown): void;
        basename?: string;
        PostUtils?: {
            formatText(text: string, options?: unknown): string;
            messageHtmlToComponent(html: string, options?: object): React.ReactNode;
        };
    }
}
