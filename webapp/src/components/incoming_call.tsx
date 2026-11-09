import React, {useEffect, useState, useSyncExternalStore} from 'react';
import {useSelector} from 'react-redux';

import {openExternal} from '../actions';
import {incomingCalls, removeIncoming, type IncomingCall} from '../bus';
import {api} from '../client';
import {useT} from '../i18n';
import {incomingAnchor, isWebUrl, type Anchor, type Box} from '../logic';
import {startRinging, stopRinging} from '../ringer';

import {CameraIcon, CrossIcon, PhoneIcon} from './icons';
import {useVisualStyle} from './use_style';

function avatarUrl(userId: string): string {
    return `${window.basename || ''}/api/v4/users/${encodeURIComponent(userId)}/image`;
}

/** Tells the server how the callee answered. Failure only costs the card update. */
function answer(call: IncomingCall, accept: boolean) {
    removeIncoming(call.meeting_id);
    api('POST', `/meetings/${call.meeting_id}/${accept ? 'answer' : 'decline'}`).catch(() => undefined);
}

function IncomingCallCard({call, silent}: {call: IncomingCall; silent: boolean}) {
    const t = useT();
    // In a group the meeting title says which group is calling.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const group = useSelector((state: any) => state.entities.channels.channels?.[call.channel_id]?.type === 'G');

    useEffect(() => {
        const left = new Date(call.expires_at).getTime() - Date.now();
        const expiry = setTimeout(() => removeIncoming(call.meeting_id), Math.max(0, left));
        if (!silent) {
            startRinging();
        }
        let note: Notification | null = null;
        if (document.hidden && 'Notification' in window && Notification.permission === 'granted') {
            note = new Notification(`${call.caller_name}: ${t('входящий звонок')}`, {body: call.title, tag: `konturtalk-${call.meeting_id}`, requireInteraction: true});
            note.onclick = () => window.focus();
        }
        return () => {
            clearTimeout(expiry);
            if (!silent) {
                stopRinging();
            }
            note?.close();
        };
    // The call object is stable for the life of the card.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [call.meeting_id, silent]);

    return (
        <div
            className='ktk-incoming'
            data-state='ringing'
            role='alertdialog'
            aria-label={`${call.caller_name}: ${t('входящий звонок')}`}
        >
            <div className='ktk-incoming__head'>
                <span className={'ktk-incoming__avatar' + (silent ? '' : ' ktk-incoming__avatar--ringing')}>
                    <img
                        src={avatarUrl(call.caller_id)}
                        alt=''
                    />
                </span>
                <div className='ktk-card__body'>
                    <div className='ktk-incoming__name'>{call.caller_name}</div>
                    <div className='ktk-card__status'>
                        <span className='ktk-status'>
                            <PhoneIcon size={12}/>
                            {t('Входящий звонок')}
                        </span>
                        <span className='ktk-incoming__app'>{t('Контур.Толк')}</span>
                    </div>
                </div>
            </div>
            {group && <div className='ktk-incoming__title'>{call.title}</div>}
            <div className='ktk-actions'>
                <button
                    className='ktk-btn ktk-btn--danger ktk-incoming__btn'
                    onClick={() => answer(call, false)}
                >
                    <CrossIcon/>
                    {t('Отклонить')}
                </button>
                {isWebUrl(call.url) && (
                    <button
                        className='ktk-btn ktk-btn--primary ktk-incoming__btn'
                        onClick={() => {
                            openExternal(call.url);
                            answer(call, true);
                        }}
                    >
                        <CameraIcon size={14}/>
                        {t('Присоединиться')}
                    </button>
                )}
            </div>
        </div>
    );
}

function boxOf(selector: string): Box | null {
    const element = document.querySelector(selector);
    if (!element) {
        return null;
    }
    const {top, right, bottom, left} = element.getBoundingClientRect();
    return bottom > top ? {top, right, bottom, left} : null;
}

function measureAnchor(): Anchor | null {
    const center = boxOf('#app-content');
    const header = boxOf('#app-content .channel-header') || boxOf('#app-content header');
    return incomingAnchor(window.innerWidth, center, header, boxOf('#sidebar-right'));
}

/**
 * Keeps the card under the channel header and inside the center column.
 * Banners, the right panel and window resizes move that column, so the
 * position is measured again while a call is ringing.
 */
function useIncomingAnchor(active: boolean): Anchor | null {
    const [anchor, setAnchor] = useState<Anchor | null>(null);
    useEffect(() => {
        if (!active) {
            return undefined;
        }
        const update = () => setAnchor((previous) => {
            const next = measureAnchor();
            const same = previous && next && previous.top === next.top && previous.right === next.right && previous.width === next.width;
            return same ? previous : next;
        });
        update();
        window.addEventListener('resize', update);
        const timer = setInterval(update, 500);
        return () => {
            window.removeEventListener('resize', update);
            clearInterval(timer);
        };
    }, [active]);
    return anchor;
}

/** Incoming calls, stacked under the channel header. Do not disturb mutes the sound. */
const IncomingCalls = () => {
    const calls = useSyncExternalStore(incomingCalls.subscribe, incomingCalls.get);
    const style = useVisualStyle();
    const anchor = useIncomingAnchor(calls.length > 0);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const dnd = useSelector((state: any) => state.entities.users.statuses?.[state.entities.users.currentUserId] === 'dnd');
    if (calls.length === 0) {
        return null;
    }
    return (
        <div
            className='ktk-root ktk-incoming-stack'
            data-ktk-style={style}
            style={anchor ? {top: anchor.top, right: anchor.right, width: anchor.width} : undefined}
        >
            {calls.map((call) => (
                <IncomingCallCard
                    key={call.meeting_id}
                    call={call}
                    silent={dnd}
                />
            ))}
        </div>
    );
};

export default IncomingCalls;
