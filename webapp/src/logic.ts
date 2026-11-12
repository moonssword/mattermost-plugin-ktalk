// Pure helpers, kept free of React so they can be unit tested.
import type {MeetingView, VisualStyle} from './types';

export function formatClock(totalSeconds: number): string {
    const seconds = Math.max(0, Math.floor(totalSeconds));
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    const mm = String(m).padStart(2, '0');
    const ss = String(s).padStart(2, '0');
    return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

export function secondsBetween(start?: string | null, end?: string | null, now: number = Date.now()): number {
    if (!start) {
        return 0;
    }
    const to = end ? new Date(end).getTime() : now;
    return (to - new Date(start).getTime()) / 1000;
}

/** A scheduled meeting can be joined shortly before it starts. */
export function canJoin(meeting: MeetingView, now: number, opensMinutes = 10): boolean {
    if (meeting.status === 'ended' || meeting.status === 'cancelled') {
        return false;
    }
    if (meeting.status === 'scheduled' && meeting.schedule) {
        return now >= new Date(meeting.schedule.start).getTime() - (opensMinutes * 60000);
    }
    return true;
}

export function isSpaceLink(url: string | undefined, spaceURL: string): boolean {
    if (!url || !spaceURL) {
        return false;
    }
    try {
        const parsed = new URL(url);
        const pathname = parsed.pathname.replace(/\/+$/, '');
        return parsed.origin.toLowerCase() === new URL(spaceURL).origin.toLowerCase() &&
            (/^\/[a-z0-9]{3,64}$/i.test(pathname) || /^\/recordings\/[^/]+$/.test(pathname));
    } catch {
        return false;
    }
}

export function filterTranscript<T extends {speaker: string; text: string}>(lines: T[], query: string): T[] {
    const needle = query.trim().toLowerCase();
    if (!needle) {
        return lines;
    }
    return lines.filter((line) => line.text.toLowerCase().includes(needle) || line.speaker.toLowerCase().includes(needle));
}

/** Reads the CSRF token Mattermost keeps in a cookie. */
export function csrfFromCookie(cookie: string): string {
    const match = cookie.match(/(?:^|;\s*)MMCSRF=([^;]+)/);
    return match ? match[1] : '';
}

/**
 * Addresses can come from outside, such as a calendar invitation. A javascript:
 * address opened from this page would run with access to Mattermost.
 */
export function isWebUrl(url: string | undefined): boolean {
    if (!url) {
        return false;
    }
    try {
        const {protocol} = new URL(url);
        return protocol === 'https:' || protocol === 'http:';
    } catch {
        return false;
    }
}

const STYLES: VisualStyle[] = ['current', 'cards', 'compact'];

/** A user's choice wins over the administrator's default. */
export function effectiveStyle(preference: string | undefined, fallback: string | undefined): VisualStyle {
    for (const value of [preference, fallback]) {
        if (STYLES.includes(value as VisualStyle)) {
            return value as VisualStyle;
        }
    }
    return 'current';
}

/** Two letters for an avatar made from a display name. */
export function initials(name: string): string {
    const words = name.trim().split(/\s+/).filter(Boolean);
    if (words.length === 0) {
        return '?';
    }
    const letters = words.length > 1 ? [words[0], words[words.length - 1]] : [words[0]];
    return letters.map((word) => Array.from(word)[0].toUpperCase()).join('');
}

/** Picks a stable color index for a name. */
/** Person colours, dark enough for white initials in light and dark themes. */
export const PERSON_COLORS = ['#1c58d9', '#6a4a9a', '#0f7b6c', '#b4531f', '#a23a6b', '#3f6212'];

/** The colour a person keeps everywhere: avatars on cards, speakers in transcripts. */
export function personColor(name: string): string {
    return PERSON_COLORS[colorIndex(name, PERSON_COLORS.length)];
}

export function colorIndex(name: string, palette: number): number {
    let hash = 0;
    for (const ch of name) {
        hash = ((hash * 31) + (ch.codePointAt(0) || 0)) % 1000003;
    }
    return hash % palette;
}

/** Where a status belongs among chip colors. */
export function chipKind(status: string): 'ok' | 'no' | 'wait' | '' {
    switch (status) {
    case 'accepted':
        return 'ok';
    case 'reject':
    case 'rejected':
    case 'declined':
    case 'error':
    case 'no_answer':
        return 'no';
    case 'calling':
    case 'alreadyCalling':
    case 'doNotDisturb':
    case 'unknown':
        return 'wait';
    default:
        return '';
    }
}

/**
 * Only the plugin's bot may produce a card. Anybody can post a message with this
 * type and made-up data, including a javascript: link, through the REST API.
 */
export function isTrustedCard(postUserId: string | undefined, botUserId: string | undefined): boolean {
    return Boolean(postUserId && botUserId && postUserId === botUserId);
}

const MEETING_STATUS_TEXTS = ['На встрече в Контур.Толке', 'In a Kontur Talk meeting'];

/**
 * Removes the plugin's own status from the user's recent statuses. Version
 * 0.4.0 stored it with a duration Mattermost does not know, which made the
 * status dialog crash; a clean list also hides a status the user never chose.
 * Returns null when nothing needs to change.
 */
export function cleanRecentStatuses(raw: string | undefined): string | null {
    if (!raw) {
        return null;
    }
    let list: Array<{emoji?: string; text?: string}>;
    try {
        list = JSON.parse(raw);
    } catch {
        return null;
    }
    if (!Array.isArray(list)) {
        return null;
    }
    const kept = list.filter((item) => !(['video_camera', 'konturtalk'].includes(item?.emoji || '') && MEETING_STATUS_TEXTS.includes(item?.text || '')));
    return kept.length === list.length ? null : JSON.stringify(kept);
}

export type Box = {top: number; right: number; bottom: number; left: number};
export type Anchor = {top: number; right: number; width: number};

/** Width of the incoming call card and its distance from the column edges. */
export const INCOMING_WIDTH = 320;
export const INCOMING_GAP = 16;
const INCOMING_MIN_WIDTH = 240;

/**
 * Where the incoming call card goes: just under the channel header and inside
 * the right edge of the center column. It never covers the channel header, the
 * right panel or the app bar, whatever banners sit above them. Returns null
 * when the column is missing or too narrow, so the stylesheet default applies.
 */
export function incomingAnchor(viewportWidth: number, center: Box | null, header: Box | null, rhs: Box | null = null): Anchor | null {
    if (!center) {
        return null;
    }
    // On narrow screens the right panel floats over the column instead of
    // squeezing it, so its left edge is where the free space ends.
    const edge = rhs && rhs.left > center.left && rhs.left < center.right ? rhs.left : center.right;
    const width = Math.min(INCOMING_WIDTH, Math.round(edge - center.left - (2 * INCOMING_GAP)));
    if (width < INCOMING_MIN_WIDTH) {
        return null;
    }
    const below = header && header.bottom > center.top ? header.bottom : center.top;
    return {
        top: Math.round(below + INCOMING_GAP),
        right: Math.round(viewportWidth - edge + INCOMING_GAP),
        width,
    };
}

/** How a recurring meeting repeats, or the plain "recurring" when unknown. */
/**
 * How a meeting repeats, for display. The server words it in full in Russian,
 * like Talk's form; other languages get the short period, which translates.
 */
export function repeatLabel(text: string | undefined, repeat: string | undefined, locale: string | undefined, t: (text: string) => string): string {
    if (text && (locale || '').toLowerCase().startsWith('ru')) {
        return text;
    }
    return t(repeatText(repeat));
}

export function repeatText(repeat: string | undefined): string {
    return ({daily: 'Ежедневно', weekly: 'Еженедельно', monthly: 'Ежемесячно'} as Record<string, string>)[repeat || ''] || 'Повторяется';
}

/**
 * Colours for the speakers of one transcript, given in order of appearance,
 * so the first six people never share a colour. Hashing names alone would
 * often give two people the same one.
 */
export function speakerColors(speakers: string[]): Map<string, string> {
    const colors = new Map<string, string>();
    for (const speaker of speakers) {
        if (!colors.has(speaker)) {
            colors.set(speaker, PERSON_COLORS[colors.size % PERSON_COLORS.length]);
        }
    }
    return colors;
}

/** The longest recordings period, in days counted inclusively. */
export const MAX_PERIOD_DAYS = 31;

function dayValue(date: Date): string {
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function parseDay(value: string): Date {
    const [year, month, day] = value.split('-').map(Number);
    return new Date(year, month - 1, day);
}

function addDays(value: string, days: number): string {
    const date = parseDay(value);
    date.setDate(date.getDate() + days);
    return dayValue(date);
}

/** The default period: the last 30 days, today included. */
export function defaultPeriod(today = new Date()): {from: string; to: string} {
    const to = dayValue(today);
    return {from: addDays(to, -(MAX_PERIOD_DAYS - 2)), to};
}

/**
 * Keeps a period of local dates (YYYY-MM-DD) in order, no longer than
 * MAX_PERIOD_DAYS and not in the future. The other end follows the one the
 * user changed, so the edit is never undone.
 */
export function clampPeriod(from: string, to: string, changed: 'from' | 'to', today = new Date()): {from: string; to: string} {
    const last = dayValue(today);
    let start = from > last ? last : from;
    let end = to > last ? last : to;
    if (changed === 'from') {
        if (end < start || addDays(start, MAX_PERIOD_DAYS - 1) < end) {
            end = addDays(start, MAX_PERIOD_DAYS - 1);
            end = end > last ? last : end;
        }
    } else if (end < start || addDays(end, -(MAX_PERIOD_DAYS - 1)) > start) {
        start = addDays(end, -(MAX_PERIOD_DAYS - 1));
    }
    return {from: start, to: end};
}

/** Bounds of a period for the server: the start of the first day to the start of the day after the last. */
export function periodBounds(from: string, to: string): {from: string; to: string} {
    return {from: parseDay(from).toISOString(), to: parseDay(addDays(to, 1)).toISOString()};
}

/**
 * Names of the people in the room. Talk counts every connection, so guests
 * without a name and second devices show as "+N" rather than going unseen.
 */
export function onlineText(names: string[], count: number): string {
    const extra = count - names.length;
    if (names.length === 0) {
        return String(count);
    }
    return extra > 0 ? `${names.join(', ')} +${extra}` : names.join(', ');
}

/**
 * The Mattermost desktop app. Its preload script sets window.desktop, and its
 * user agent names it; either is enough.
 */
export function isDesktopApp(bridge: unknown, userAgent: string): boolean {
    return Boolean(bridge) || /\bMattermost\/\d/.test(userAgent);
}
