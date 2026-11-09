import React, {useEffect, useState} from 'react';
import {useSelector, useStore} from 'react-redux';

import {meetingAction, openExternal, runCommand, showRecording} from '../actions';
import {useLocale, useT} from '../i18n';
import {canJoin, chipKind, formatClock, initials, isTrustedCard, isWebUrl, personColor, repeatText, secondsBetween} from '../logic';
import type {MeetingView} from '../types';

import {CalendarIcon, CameraIcon, CheckIcon, CopyIcon, DocumentIcon, EditIcon, ExternalIcon, PhoneIcon, RecordIcon, StopIcon} from './icons';
import Markdown from './markdown';
import {rhsActions} from './rhs_handle';
import {useConfig, useVisualStyle} from './use_style';

const CALLEE_TEXT: Record<string, string> = {
    calling: 'Звоним',
    accepted: 'Принял(а)',
    declined: 'Отклонил(а)',
    alreadyCalling: 'Уже разговаривает',
    doNotDisturb: 'Не беспокоить',
    rejected: 'Отклонил(а)',
    no_answer: 'Не дозвонились',
    not_in_talk: 'Нет в Толке',
    userNotFound: 'Нет в Толке',
    calls_off: 'Звонки отключены',
    cancelled: 'Отменён',
    error: 'Ошибка',
};

const ATTENDEE_TEXT: Record<string, string> = {
    accepted: 'Принял(а)',
    reject: 'Отклонил(а)',
    unknown: 'Нет ответа',
};


function statusText(meeting: MeetingView): string {
    switch (meeting.status) {
    case 'scheduled': return 'Запланирована';
    case 'ringing': return 'Идёт звонок';
    case 'live': return 'Идёт встреча';
    case 'ended': return meeting.expired ? 'Не состоялась' : 'Завершена';
    case 'cancelled': return 'Отменена';
    default: return 'Ожидание участников';
    }
}

/** CSS state: finished meetings look the same whichever way they finished. */
function cardState(meeting: MeetingView): string {
    return meeting.status === 'cancelled' ? 'ended' : meeting.status;
}

function useNow(active: boolean): number {
    const [now, setNow] = useState(Date.now());
    useEffect(() => {
        if (!active) {
            return undefined;
        }
        const timer = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(timer);
    }, [active]);
    return now;
}

// Moderation buttons are a convenience: the server checks rights again. A plain
// member sees none of them because the server would refuse anyway.
function useCanControl(meeting: MeetingView | undefined): boolean {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    return useSelector((state: any) => {
        if (!meeting) {
            return false;
        }
        const users = state.entities.users;
        const userId = users.currentUserId;
        if (userId === meeting.creator_id) {
            return true;
        }
        if ((users.profiles[userId]?.roles || '').includes('system_admin')) {
            return true;
        }
        return Boolean(state.entities.channels.membersInChannel?.[meeting.channel_id]?.[userId]?.scheme_admin);
    });
}

function CopyButton({url}: {url: string}) {
    const t = useT();
    const [copied, setCopied] = useState(false);
    return (
        <button
            className='ktk-btn ktk-btn--secondary'
            onClick={async () => {
                await navigator.clipboard.writeText(url);
                setCopied(true);
                setTimeout(() => setCopied(false), 2000);
            }}
        >
            {copied ? <CheckIcon/> : <CopyIcon/>}
            {t(copied ? 'Скопировано' : 'Скопировать ссылку')}
        </button>
    );
}

function Row({label, children}: {label: string; children: React.ReactNode}) {
    return (
        <div className='ktk-row'>
            <dt>{label}</dt>
            <dd>{children}</dd>
        </div>
    );
}

function Chip({status, text}: {status: string; text: string}) {
    const kind = chipKind(status);
    return <span className={'ktk-chip' + (kind ? ` ktk-chip--${kind}` : '')}>{text}</span>;
}

function Avatars({names}: {names: string[]}) {
    return (
        <span
            className='ktk-avatars'
            aria-hidden='true'
        >
            {names.slice(0, 5).map((name) => (
                <span
                    key={name}
                    className='ktk-av'
                    style={{background: personColor(name)}}
                >
                    {initials(name)}
                </span>
            ))}
        </span>
    );
}

function StateIcon({meeting}: {meeting: MeetingView}) {
    if (meeting.status === 'scheduled') {
        return <CalendarIcon size={20}/>;
    }
    if (meeting.status === 'ringing') {
        return <PhoneIcon size={20}/>;
    }
    return <CameraIcon size={20}/>;
}

/**
 * The current user's own attendee row. An invited user answers right in the
 * list, where everyone else's answer is shown. Talk takes the answer only with
 * the user's own personal key, so without one the row offers to connect.
 */
function SelfRow({meeting, status}: {meeting: MeetingView; status: string}) {
    const t = useT();
    const store = useStore();
    const config = useConfig();
    const [busy, setBusy] = useState(false);
    const answer = async (accept: boolean) => {
        if ((accept && status === 'accepted') || (!accept && status === 'reject')) {
            return;
        }
        setBusy(true);
        await meetingAction(meeting.id, accept ? 'accept-invite' : 'decline-invite', t(accept ? 'Вы приняли приглашение.' : 'Вы отклонили приглашение.'));
        setBusy(false);
    };
    return (
        <div className='ktk-row ktk-row--self'>
            <dt>{t('Вы')}</dt>
            <dd>
                {config?.personal_key ? (
                    <span
                        className='ktk-answer'
                        role='group'
                        aria-label={t('Ваш ответ')}
                    >
                        <button
                            type='button'
                            className='ktk-answer__btn ktk-answer__btn--ok'
                            aria-pressed={status === 'accepted'}
                            disabled={busy}
                            onClick={() => answer(true)}
                        >
                            {t('Принять')}
                        </button>
                        <button
                            type='button'
                            className='ktk-answer__btn ktk-answer__btn--no'
                            aria-pressed={status === 'reject'}
                            disabled={busy}
                            onClick={() => answer(false)}
                        >
                            {t('Отклонить')}
                        </button>
                    </span>
                ) : (
                    <>
                        <Chip
                            status={status || 'unknown'}
                            text={t(ATTENDEE_TEXT[status] || 'Нет ответа')}
                        />
                        {' '}
                        <button
                            type='button'
                            className='ktk-link'
                            title={t('Чтобы отвечать отсюда, подключите личный ключ Толка')}
                            onClick={() => runCommand(store, '/talk connect', meeting.channel_id)}
                        >
                            {t('Ответить')}
                        </button>
                    </>
                )}
            </dd>
        </div>
    );
}

/** Attendees of a scheduled meeting, with the current user's row answerable. */
function AttendeeRows({meeting}: {meeting: MeetingView}) {
    const t = useT();
    const config = useConfig();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const userId: string = useSelector((state: any) => state.entities.users.currentUserId);
    const schedule = meeting.schedule;
    if (!schedule) {
        return null;
    }
    const attendees = schedule.attendees || [];
    const ownKey = schedule.keys?.[userId] || '';
    const answerable = Boolean(ownKey) && meeting.status === 'scheduled' && (schedule.invited || []).includes(userId) &&
        userId !== meeting.creator_id && Boolean(config?.personal_keys);
    const given = schedule.answers?.[userId] || '';
    const listed = answerable && attendees.some((attendee) => attendee.key === ownKey);
    return (
        <>
            {attendees.map((attendee) => (answerable && attendee.key === ownKey ? (
                <SelfRow
                    key={attendee.name}
                    meeting={meeting}
                    status={given || attendee.status}
                />
            ) : (
                <Row
                    key={attendee.name}
                    label={attendee.name}
                >
                    <Chip
                        status={attendee.status || 'unknown'}
                        text={t(ATTENDEE_TEXT[attendee.status] || 'Нет ответа')}
                    />
                </Row>
            )))}
            {answerable && !listed && (
                <SelfRow
                    meeting={meeting}
                    status={given}
                />
            )}
        </>
    );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const MeetingPost = ({post}: {post: any}) => {
    const t = useT();
    const locale = useLocale();
    const store = useStore();
    const config = useConfig();
    const style = useVisualStyle();
    const trusted = isTrustedCard(post.user_id, config?.bot_user_id);
    const meeting = trusted ? (post.props?.meeting as MeetingView | undefined) : undefined;
    const live = meeting?.status === 'live';
    const scheduled = meeting?.status === 'scheduled';
    const now = useNow(Boolean(live || scheduled));
    const canControl = useCanControl(meeting) && Boolean(config?.enabled);

    if (!meeting) {
        return <Markdown text={post.message || ''}/>;
    }
    const closed = meeting.status === 'ended' || meeting.status === 'cancelled';
    const joinable = canJoin(meeting, now, config?.join_opens_min);
    const dateTime = new Intl.DateTimeFormat(locale, {dateStyle: 'medium', timeStyle: 'short'});
    const results = meeting.results;
    const elapsed = formatClock(secondsBetween(meeting.started_at, null, now));
    const materials = results ? [
        results.notes ? `${t('Заметки')}: ${results.notes}` : '',
        results.polls ? `${t('Опросы')}: ${results.polls}` : '',
        results.boards ? `${t('Доски')}: ${results.boards}` : '',
    ].filter(Boolean).join(' · ') : '';
    const showCallees = meeting.status !== 'ended' && meeting.status !== 'cancelled';

    return (
        <div
            className='ktk-root'
            data-ktk-style={style}
        >
            <div
                className='ktk-card'
                data-state={cardState(meeting)}
            >
                <div className='ktk-card__head'>
                    <div className='ktk-card__icon'><StateIcon meeting={meeting}/></div>
                    <div className='ktk-card__body'>
                        <div className='ktk-card__title'>
                            <Markdown
                                text={meeting.title}
                                singleline={true}
                            />
                        </div>
                        <div className='ktk-card__status'>
                            <span className='ktk-status'>
                                {live && <span className='ktk-dot'/>}
                                {t(statusText(meeting))}
                            </span>
                            {live && <span className='ktk-time'>{elapsed}</span>}
                            {meeting.status === 'ended' && !meeting.expired && meeting.started_at && (
                                <span className='ktk-time'>{formatClock(secondsBetween(meeting.started_at, meeting.ended_at))}</span>
                            )}
                            {meeting.recording && <span className='ktk-rec'>{t('Запись идёт')}</span>}
                        </div>
                    </div>
                </div>

                <dl className='ktk-rows'>
                    {meeting.schedule && (
                        <Row label={t('Когда')}>
                            {dateTime.format(new Date(meeting.schedule.start))}
                            {meeting.schedule.recurring && ` · ${t(repeatText(meeting.schedule.repeat))}`}
                            {meeting.schedule.auto_record && ` · ${t('Автозапись')}`}
                        </Row>
                    )}
                    {meeting.schedule?.description && (
                        <div className='ktk-text desc'>
                            <Markdown text={meeting.schedule.description}/>
                        </div>
                    )}
                    <Row label={t(meeting.schedule ? 'Организатор' : 'Начал(а)')}>{meeting.creator_name}</Row>
                    {live && meeting.users_online > 0 && (
                        <Row label={t('В комнате')}>
                            <Avatars names={meeting.online || []}/>
                            {(meeting.online || []).join(', ') || meeting.users_online}
                        </Row>
                    )}
                    {!closed && meeting.pin && <Row label={t('ПИН-код')}>{meeting.pin}</Row>}
                    {!closed && meeting.lobby && <Row label={t('Вход')}>{t('Через зал ожидания')}</Row>}
                    {!closed && meeting.guests_until && (
                        <Row label={t('Вход без аккаунта до')}>{dateTime.format(new Date(meeting.guests_until))}</Row>
                    )}
                    {!closed && meeting.sip?.sipNumber && (
                        <Row label={t('Дозвон')}>{[meeting.sip.sipNumber, ...(meeting.sip.phoneNumbers || [])].join(', ')}</Row>
                    )}
                    {showCallees && meeting.callees?.map((callee) => (
                        <Row
                            key={callee.user_id}
                            label={callee.name}
                        >
                            <Chip
                                status={callee.status}
                                text={t(CALLEE_TEXT[callee.status] || callee.status)}
                            />
                        </Row>
                    ))}
                    {!closed && <AttendeeRows meeting={meeting}/>}
                    {results?.attended && results.attended.length > 0 && <Row label={t('Были на встрече')}>{results.attended.join(', ')}</Row>}
                    {results?.missed && results.missed.length > 0 && <Row label={t('Не пришли')}>{results.missed.join(', ')}</Row>}
                    {materials && <Row label={t('Материалы')}>{materials}</Row>}
                </dl>


                <div className='ktk-actions'>
                    {!closed && isWebUrl(meeting.url) && (
                        <button
                            className='ktk-btn ktk-btn--primary'
                            disabled={!joinable}
                            onClick={() => openExternal(meeting.url)}
                        >
                            <CameraIcon size={14}/>
                            {t('Присоединиться')}
                        </button>
                    )}
                    {!closed && isWebUrl(meeting.url) && <CopyButton url={meeting.url}/>}
                    {results?.url && isWebUrl(results.url) && (
                        <a
                            className='ktk-btn ktk-btn--secondary'
                            href={results.url}
                            target='_blank'
                            rel='noopener noreferrer'
                        >
                            <ExternalIcon/>
                            {t('Итоги в Толке')}
                        </a>
                    )}
                    {config?.enabled && meeting.recordings?.map((recording, index, all) => (
                        <button
                            key={recording.key}
                            className='ktk-btn ktk-btn--secondary'
                            onClick={() => showRecording(store, rhsActions.show, recording.key, 'summary')}
                        >
                            <DocumentIcon/>
                            {t('Запись')}{all.length > 1 ? ` ${index + 1}` : ''}
                        </button>
                    ))}
                    {canControl && !meeting.link_only && meeting.status === 'ringing' && (
                        <button
                            className='ktk-btn ktk-btn--danger'
                            onClick={() => meetingAction(meeting.id, 'cancel-call')}
                        >
                            <StopIcon/>
                            {t('Отменить звонок')}
                        </button>
                    )}
                    {canControl && live && !meeting.link_only && !meeting.recording && (
                        <button
                            className='ktk-btn ktk-btn--secondary'
                            onClick={() => meetingAction(meeting.id, 'record')}
                        >
                            <RecordIcon/>
                            {t('Начать запись')}
                        </button>
                    )}
                    {canControl && live && !meeting.link_only && (
                        <button
                            className='ktk-btn ktk-btn--danger'
                            onClick={() => {
                                if (window.confirm(t('Завершить встречу для всех участников?'))) {
                                    meetingAction(meeting.id, 'end');
                                }
                            }}
                        >
                            <StopIcon/>
                            {t('Завершить для всех')}
                        </button>
                    )}
                    {canControl && scheduled && (
                        <button
                            className='ktk-btn ktk-btn--secondary'
                            onClick={() => runCommand(store, `/talk schedule edit ${meeting.id}`, meeting.channel_id)}
                        >
                            <EditIcon/>
                            {t('Изменить')}
                        </button>
                    )}
                    {canControl && scheduled && (
                        <button
                            className='ktk-btn ktk-btn--danger'
                            onClick={() => {
                                if (window.confirm(t('Отменить эту встречу? Участники получат уведомление.'))) {
                                    meetingAction(meeting.id, 'cancel-schedule');
                                }
                            }}
                        >
                            {t('Отменить встречу')}
                        </button>
                    )}
                </div>
            </div>
        </div>
    );
};

export default MeetingPost;
