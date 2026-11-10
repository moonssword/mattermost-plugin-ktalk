import {useEffect, useState} from 'react';

import {api} from './client';

/** What one user may do with a meeting, as Talk sees it. */
export interface MeetingRights {
    /** A moderator of the room: record, end, PIN, guests, waiting room. */
    room: boolean;
    /** The organizer of the event: change or cancel it. */
    schedule: boolean;
    /** The person ringing: stop it. */
    ring: boolean;
    /** Invited in Talk, other than the organizer: may answer. */
    attendee: boolean;
    /** The user's key in Talk, to find their own row among attendees. */
    key: string;
    /** The user has connected a personal key; rights need it. */
    personal_key: boolean;
}

export const NO_RIGHTS: MeetingRights = {room: false, schedule: false, ring: false, attendee: false, key: '', personal_key: false};

const TTL_MS = 30000;
const answers = new Map<string, {rights: MeetingRights; at: number}>();
const pending = new Map<string, Promise<void>>();

/**
 * The user's rights for a meeting in its current state. Until the server has
 * answered, none are given, so a control never shows to someone who cannot
 * use it. The server checks every action again.
 */
export function useMeetingRights(meetingId: string | undefined, status: string | undefined, enabled: boolean, personalKey = false): MeetingRights {
    // Rights depend on the personal key, so connecting one asks again.
    const id = meetingId && enabled ? `${meetingId}:${status || ''}:${personalKey ? 'key' : ''}` : '';
    const known = () => {
        const answer = id ? answers.get(id) : undefined;
        return answer && Date.now() - answer.at < TTL_MS ? answer.rights : null;
    };
    const [rights, setRights] = useState<MeetingRights>(() => known() || NO_RIGHTS);
    useEffect(() => {
        if (!id || !meetingId) {
            setRights(NO_RIGHTS);
            return undefined;
        }
        let cancelled = false;
        const cached = known();
        if (cached) {
            setRights(cached);
            return undefined;
        }
        if (!pending.has(id)) {
            pending.set(id, api<MeetingRights>('GET', `/meetings/${meetingId}/rights`).then((result) => {
                answers.set(id, {rights: {...NO_RIGHTS, ...result}, at: Date.now()});
            }).catch(() => undefined).finally(() => pending.delete(id)));
        }
        pending.get(id)!.then(() => {
            if (!cancelled) {
                setRights(known() || NO_RIGHTS);
            }
        });
        return () => {
            cancelled = true;
        };
    // known reads the cache for the current id only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id]);
    return rights;
}

/** Forgets every answer, after the personal key is connected or removed. */
export function forgetAllMeetingRights() {
    answers.clear();
}

/** Forgets the rights of a meeting, after an answer or a change. */
export function forgetMeetingRights(meetingId: string) {
    for (const id of answers.keys()) {
        if (id.startsWith(`${meetingId}:`)) {
            answers.delete(id);
        }
    }
}
