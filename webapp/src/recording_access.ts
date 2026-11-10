import {useEffect, useState} from 'react';

import {api} from './client';

const TTL_MS = 60000;

// Answers per recording key, shared by every card on the page, so a thread
// that shows one recording several times asks the server once.
const answers = new Map<string, {allowed: boolean; at: number}>();
const pending = new Map<string, Promise<void>>();

function fresh(key: string): boolean {
    const answer = answers.get(key);
    return Boolean(answer && Date.now() - answer.at < TTL_MS);
}

async function ask(keys: string[]): Promise<void> {
    const missing = keys.filter((key) => !fresh(key) && !pending.has(key));
    if (missing.length > 0) {
        const request = api<{access: Record<string, boolean>}>('POST', '/recordings/access', {keys: missing}).then((result) => {
            const at = Date.now();
            for (const key of missing) {
                answers.set(key, {allowed: Boolean(result.access?.[key]), at});
            }
        }).catch(() => {
            // No answer means no buttons; the next render asks again.
        }).finally(() => {
            missing.forEach((key) => pending.delete(key));
        });
        missing.forEach((key) => pending.set(key, request));
    }
    await Promise.all(keys.map((key) => pending.get(key)).filter(Boolean));
}

/**
 * The recordings among keys that the user may open. Talk decides, through the
 * server; until it has answered, none are shown, so a button never appears
 * to someone who cannot use it.
 */
export function useRecordingAccess(keys: string[]): Set<string> {
    const id = keys.join(',');
    const known = () => new Set(keys.filter((key) => fresh(key) && answers.get(key)!.allowed));
    const [allowed, setAllowed] = useState<Set<string>>(known);
    useEffect(() => {
        if (keys.length === 0) {
            return undefined;
        }
        let cancelled = false;
        ask(keys).then(() => {
            if (!cancelled) {
                setAllowed(known());
            }
        });
        return () => {
            cancelled = true;
        };
    // The keys are compared by their joined form.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id]);
    return allowed;
}
