import React, {useCallback, useEffect, useRef, useState, useSyncExternalStore} from 'react';
import {useStore} from 'react-redux';

import {openExternal, openPersonalRoom, runCommand, startMeeting} from '../actions';
import {meetingsVersion, rhsRecording} from '../bus';
import {api, ApiError, isNetworkError, NETWORK_ERROR} from '../client';
import {useLocale, useT} from '../i18n';
import {clampPeriod, defaultPeriod, filterTranscript, formatClock, periodBounds, repeatLabel, secondsBetween, speakerColors} from '../logic';
import type {MeetingView, RecordingArtifacts, RecordingItem, UpcomingEvent} from '../types';

import {useCallPlace} from './call_menu';
import Dropdown from './dropdown';
import {CalendarIcon, CameraIcon, ChevronLeftIcon, ClockIcon, DocumentIcon, EditIcon, ExternalIcon, InfoIcon, PhoneIcon, RoomIcon, UnavailableIcon} from './icons';
import Markdown from './markdown';
import {PersonalKeyPanel} from './personal_key';
import {useConfig, useVisualStyle} from './use_style';

const REFRESH_MS = 30000;

interface LoadError {
    message: string;
    code: string;
}

/** Pauses before each retry of a request that did not reach the server. */
const RETRY_MS = [2000, 5000, 15000];

/**
 * Loads a list and keeps it. A request that never reached the server, for
 * example while the plugin restarts, is retried quietly and the last list
 * stays on screen; only a list that never loaded explains the trouble.
 */
function useLoad<T>(path: string, deps: unknown[] = []) {
    const [data, setData] = useState<T | null>(null);
    const [error, setError] = useState<LoadError | null>(null);
    const retry = useRef<ReturnType<typeof setTimeout>>();
    const attempt = useRef(0);
    const loaded = useRef(false);
    const reload = useCallback(async () => {
        clearTimeout(retry.current);
        try {
            setData(await api<T>('GET', path));
            setError(null);
            loaded.current = true;
            attempt.current = 0;
        } catch (e) {
            if (isNetworkError(e) && attempt.current < RETRY_MS.length) {
                retry.current = setTimeout(reload, RETRY_MS[attempt.current]);
                attempt.current++;
                return;
            }
            attempt.current = 0;
            if (isNetworkError(e) && loaded.current) {
                return; // The list on screen stays; the next refresh tries again.
            }
            setError({message: (e as Error).message, code: (e as ApiError).code || ''});
        }
    }, [path]);
    useEffect(() => {
        reload();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [reload, ...deps]);
    useEffect(() => () => clearTimeout(retry.current), []);
    return {data, error, reload};
}

/**
 * Meeting changes as a counter that moves at most once per interval. The
 * calendar asks Talk on every load, and live meetings change every few
 * seconds, so the last change waits for the interval instead of being lost.
 */
function useThrottledVersion(interval: number): number {
    const version = useSyncExternalStore(meetingsVersion.subscribe, meetingsVersion.get);
    const [shown, setShown] = useState(version);
    const [last, setLast] = useState(0);
    useEffect(() => {
        if (version === shown) {
            return undefined;
        }
        const wait = Math.max(0, last + interval - Date.now());
        const timer = setTimeout(() => {
            setShown(version);
            setLast(Date.now());
        }, wait);
        return () => clearTimeout(timer);
    }, [version, shown, last, interval]);
    return shown;
}

function Empty({text, icon}: {text: string; icon?: React.ReactNode}) {
    return (
        <div className={'ktk-empty' + (icon ? ' ktk-empty--icon' : '')}>
            {icon}
            <span>{text}</span>
        </div>
    );
}

/**
 * Grey stand-ins shaped like the content that is on its way: list rows with a
 * button, or lines of text. They fade in after a moment, so a quick answer
 * shows nothing at all. Screen readers still hear that loading is going on.
 */
function Skeleton({kind = 'list', rows = 3}: {kind?: 'list' | 'text'; rows?: number}) {
    const t = useT();
    const widths = ['72%', '58%', '66%', '50%', '62%'];
    return (
        <div
            className='ktk-skel'
            role='status'
            aria-busy='true'
            aria-label={t('Загрузка…')}
        >
            {Array.from({length: rows}, (_, i) => (kind === 'list' ? (
                <div
                    key={i}
                    className='ktk-item ktk-skel__item'
                    aria-hidden='true'
                >
                    <span
                        className='ktk-skel__bar ktk-skel__title'
                        style={{width: widths[i % widths.length]}}
                    />
                    <span className='ktk-skel__bar ktk-skel__meta'/>
                    <span className='ktk-item__actions'>
                        <span className='ktk-skel__bar ktk-skel__button'/>
                    </span>
                </div>
            ) : (
                <span
                    key={i}
                    className='ktk-skel__bar ktk-skel__line'
                    style={{width: i === rows - 1 ? '45%' : '100%'}}
                    aria-hidden='true'
                />
            )))}
        </div>
    );
}

/**
 * A problem the user can do something about, such as an unconfirmed address,
 * reads as advice. Anything else is an error.
 */
function Problem({error}: {error: LoadError}) {
    const t = useT();
    if (error.code === NETWORK_ERROR) {
        return (
            <div
                className='ktk-notice'
                role='status'
            >
                <InfoIcon size={16}/>
                <span>{t('Нет связи с сервером. Повторим автоматически.')}</span>
            </div>
        );
    }
    if (!error.code) {
        return <div className='ktk-error'>{error.message}</div>;
    }
    return (
        <div
            className='ktk-notice'
            role='status'
        >
            <InfoIcon size={16}/>
            <span>{error.message}</span>
        </div>
    );
}

function MeetingItem({meeting}: {meeting: MeetingView}) {
    const t = useT();
    return (
        <div
            className='ktk-item'
            data-state={meeting.status}
        >
            <div className='ktk-item__title'>
                <Markdown
                    text={meeting.title}
                    singleline={true}
                />
            </div>
            <div className='ktk-item__meta'>
                {meeting.channel_name && <span>{meeting.channel_name}</span>}
                {meeting.status === 'live' && <span>{`${t('В комнате')}: ${meeting.users_online}`}</span>}
                {meeting.status === 'live' && <span className='ktk-time'>{formatClock(secondsBetween(meeting.started_at))}</span>}
            </div>
            <div className='ktk-item__actions'>
                <button
                    className='ktk-btn ktk-btn--secondary'
                    aria-label={`${t('Присоединиться')}: ${meeting.title}`}
                    onClick={() => openExternal(meeting.url)}
                >
                    <CameraIcon size={12}/>
                    {t('Войти')}
                </button>
            </div>
        </div>
    );
}

function NowTab({onCount}: {onCount: (count: number) => void}) {
    const t = useT();
    const version = useSyncExternalStore(meetingsVersion.subscribe, meetingsVersion.get);
    const {data, error, reload} = useLoad<MeetingView[]>('/meetings/active', [version]);
    useEffect(() => {
        const timer = setInterval(reload, REFRESH_MS);
        return () => clearInterval(timer);
    }, [reload]);
    useEffect(() => {
        if (data) {
            onCount(data.length);
        }
    }, [data, onCount]);
    if (error) {
        return <Problem error={error}/>;
    }
    if (!data) {
        return <Skeleton/>;
    }
    if (data.length === 0) {
        return <Empty text={t('Нет идущих встреч в ваших каналах.')}/>;
    }
    const live = data.filter((meeting) => meeting.status === 'live');
    const waiting = data.filter((meeting) => meeting.status !== 'live');
    return (
        <>
            {live.length > 0 && <div className='ktk-section-title'>{t('Идут сейчас')}</div>}
            {live.map((meeting) => (
                <MeetingItem
                    key={meeting.id}
                    meeting={meeting}
                />
            ))}
            {waiting.length > 0 && <div className='ktk-section-title'>{t('Ожидают участников')}</div>}
            {waiting.map((meeting) => (
                <MeetingItem
                    key={meeting.id}
                    meeting={meeting}
                />
            ))}
        </>
    );
}

function UpcomingTab({onCount}: {onCount: (count: number) => void}) {
    const t = useT();
    const locale = useLocale();
    const store = useStore();
    const version = useThrottledVersion(10000);
    const config = useConfig();
    const {data, error} = useLoad<UpcomingEvent[]>('/upcoming', [version, config?.personal_key]);
    useEffect(() => {
        if (data) {
            onCount(data.length);
        }
    }, [data, onCount]);
    if (error) {
        // A missing personal key is solved right here, not with a command.
        if (error.code === 'personal_key_required') {
            return <PersonalKeyPanel/>;
        }
        return <Problem error={error}/>;
    }
    if (!data) {
        return <Skeleton/>;
    }
    const keyPanel = <PersonalKeyPanel inline={true}/>;
    if (data.length === 0) {
        return (
            <>
                {keyPanel}
                <Empty text={t('Нет встреч на ближайшую неделю.')}/>
            </>
        );
    }
    const format = new Intl.DateTimeFormat(locale, {weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit'});
    return (
        <>
            {keyPanel}
            {data.map((event) => (
                <div
                    className='ktk-item'
                    data-state='scheduled'
                    key={event.id}
                >
                    <div className='ktk-item__title'>{event.subject}</div>
                    <div className='ktk-item__meta'>
                        <span>{format.format(new Date(event.start))}</span>
                        {event.external && <span>{event.external}</span>}
                        {event.recurring && <span>{repeatLabel(event.repeat_text, event.repeat, locale, t)}</span>}
                    </div>
                    {(event.url || event.meeting_id) && (
                        <div className='ktk-item__actions'>
                            {event.url && (
                                <button
                                    className='ktk-btn ktk-btn--secondary'
                                    aria-label={`${t('Присоединиться')}: ${event.subject}`}
                                    onClick={() => openExternal(event.url)}
                                >
                                    <CameraIcon size={12}/>
                                    {t('Войти')}
                                </button>
                            )}
                            {event.meeting_id && (
                                <button
                                    className='ktk-btn ktk-btn--secondary ktk-btn--icon'
                                    title={t('Изменить')}
                                    aria-label={`${t('Изменить')}: ${event.subject}`}
                                    onClick={() => runCommand(store, `/talk schedule edit ${event.meeting_id}`, event.channel_id)}
                                >
                                    <EditIcon/>
                                </button>
                            )}
                        </div>
                    )}
                </div>
            ))}
        </>
    );
}

function RecordingsTab({onOpen}: {onOpen: (key: string) => void}) {
    const t = useT();
    const locale = useLocale();
    const [query, setQuery] = useState('');
    const [debounced, setDebounced] = useState('');
    const [period, setPeriod] = useState(() => defaultPeriod());
    const [items, setItems] = useState<RecordingItem[] | null>(null);
    const [next, setNext] = useState('');
    const [error, setError] = useState('');

    useEffect(() => {
        const timer = setTimeout(() => setDebounced(query), 300);
        return () => clearTimeout(timer);
    }, [query]);

    const load = useCallback(async (page: string, previous: RecordingItem[], retried = false): Promise<void> => {
        try {
            const params = new URLSearchParams();
            if (debounced) {
                params.set('q', debounced);
            }
            if (page) {
                params.set('page', page);
            }
            const bounds = periodBounds(period.from, period.to);
            params.set('from', bounds.from);
            params.set('to', bounds.to);
            const result = await api<{recordings: RecordingItem[]; next_page: string}>('GET', `/recordings?${params}`);
            setItems([...previous, ...result.recordings]);
            setNext(result.next_page);
            setError('');
        } catch (e) {
            // A dropped connection is retried once quietly before it is shown.
            if (isNetworkError(e) && !retried) {
                setTimeout(() => load(page, previous, true), RETRY_MS[1]);
                return;
            }
            setError(isNetworkError(e) ? t('Нет связи с сервером. Обновите вкладку чуть позже.') : (e as Error).message);
        }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [debounced, period]);

    useEffect(() => {
        setItems(null);
        load('', []);
    }, [load]);

    const today = defaultPeriod().to;
    const changePeriod = (end: 'from' | 'to', value: string) => {
        if (!value) {
            return;
        }
        const next = end === 'from' ? clampPeriod(value, period.to, 'from') : clampPeriod(period.from, value, 'to');
        if (next.from !== period.from || next.to !== period.to) {
            setPeriod(next);
        }
    };

    const format = new Intl.DateTimeFormat(locale, {dateStyle: 'medium', timeStyle: 'short'});
    return (
        <>
            <input
                className='ktk-input'
                placeholder={t('Поиск записей')}
                aria-label={t('Поиск записей')}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
            />
            <fieldset className='ktk-period'>
                <legend className='ktk-sr'>{t('Период, не больше месяца')}</legend>
                <div className='ktk-period__box'>
                    <input
                        type='date'
                        aria-label={t('Начало периода')}
                        value={period.from}
                        max={today}
                        onChange={(e) => changePeriod('from', e.target.value)}
                    />
                    <span
                        className='ktk-period__dash'
                        aria-hidden='true'
                    >{'–'}</span>
                    <input
                        type='date'
                        aria-label={t('Конец периода')}
                        value={period.to}
                        max={today}
                        onChange={(e) => changePeriod('to', e.target.value)}
                    />
                </div>
                <div className='ktk-period__hint'>{t('Период не больше месяца')}</div>
            </fieldset>
            {error && <div className='ktk-error'>{error}</div>}
            {!items && !error && <Skeleton/>}
            {items?.length === 0 && <Empty text={t(debounced ? 'Ничего не найдено за этот период.' : 'За этот период записей нет.')}/>}
            {items?.map((item) => (
                <div
                    className='ktk-item'
                    key={item.key}
                >
                    <div className='ktk-item__title'>{item.title}</div>
                    <div className='ktk-item__meta'>
                        <span>{format.format(new Date(item.created_date))}</span>
                        <span className='ktk-time'>{formatClock(item.duration)}</span>
                        <span>{participantsText(item.participants, t)}</span>
                    </div>
                    <div className='ktk-item__actions'>
                        <button
                            className='ktk-btn ktk-btn--secondary'
                            aria-label={`${t('Пересказ')}: ${item.title}`}
                            onClick={() => onOpen(item.key)}
                        >
                            <DocumentIcon size={12}/>
                            {t('Пересказ')}
                        </button>
                        <a
                            className='ktk-btn ktk-btn--secondary ktk-btn--icon'
                            href={item.url}
                            target='_blank'
                            rel='noopener noreferrer'
                            title={t('Открыть в Толке')}
                            aria-label={`${t('Открыть в Толке')}: ${item.title}`}
                        >
                            <ExternalIcon/>
                        </a>
                    </div>
                </div>
            ))}
            {next && (
                <div className='ktk-more'>
                    <button
                        className='ktk-btn ktk-btn--secondary'
                        onClick={() => load(next, items || [])}
                    >
                        {t('Показать ещё')}
                    </button>
                </div>
            )}
        </>
    );
}

/** "1 участник", "3 участника", "5 участников"; English has its own forms. */
function participantsText(count: number, t: (text: string) => string): string {
    const tens = count % 100;
    const ones = count % 10;
    let word = 'участников';
    if (tens < 11 || tens > 14) {
        if (ones === 1) {
            word = 'участник';
        } else if (ones >= 2 && ones <= 4) {
            word = 'участника';
        }
    }
    return `${count} ${t(word)}`;
}

/** Why a summary, the minutes or the transcript is not there, with its mark. */
function SpeechEmpty({status}: {status: string}) {
    const t = useT();
    const pending = status === 'inProgress' || status === 'recreateInProgress';
    return (
        <Empty
            text={t(pending ? 'Ещё готовится.' : 'Недоступно для этой записи.')}
            icon={pending ? (
                <ClockIcon
                    size={28}
                    weight={1.6}
                />
            ) : (
                <UnavailableIcon
                    size={28}
                    weight={1.6}
                />
            )}
        />
    );
}

function RecordingDetails({recordingKey, section: initial, onBack}: {recordingKey: string; section: string; onBack: () => void}) {
    const t = useT();
    const [section, setSection] = useState(initial);
    const [query, setQuery] = useState('');
    const {data, error} = useLoad<RecordingArtifacts>(`/recordings/${encodeURIComponent(recordingKey)}/artifacts`);

    useEffect(() => setSection(initial), [initial]);

    let body: React.ReactNode = <Skeleton
        kind='text'
        rows={6}
    />;
    if (error) {
        body = <Problem error={error}/>;
    } else if (data && section === 'transcript') {
        const lines = filterTranscript(data.transcript.lines || [], query);
        // From the whole transcript, so a search keeps everyone's colour.
        const colors = speakerColors((data.transcript.lines || []).map((line) => line.speaker));
        body = (
            <>
                <input
                    className='ktk-input'
                    placeholder={t('Поиск по тексту')}
                    aria-label={t('Поиск по тексту')}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                />
                {(data.transcript.lines || []).length === 0 && <SpeechEmpty status={data.transcript.status}/>}
                {(data.transcript.lines || []).length > 0 && lines.length === 0 && <Empty text={t('Ничего не найдено.')}/>}
                {lines.map((line, index) => (
                    <div
                        className='ktk-line'
                        key={`${line.start}-${index}`}
                    >
                        <div className='ktk-line__meta'>
                            <span className='ktk-line__who'>
                                <span
                                    className='ktk-line__dot'
                                    style={{background: colors.get(line.speaker)}}
                                    aria-hidden='true'
                                />
                                {line.speaker}
                            </span>
                            <span className='ktk-line__time'>{formatClock(line.start / 1000)}</span>
                        </div>
                        <div className='ktk-text'>{line.text}</div>
                    </div>
                ))}
            </>
        );
    } else if (data) {
        const summary = section === 'protocol' ? data.protocol : data.summary;
        body = summary.text ? (
            <Markdown
                text={summary.text}
                className='ktk-text body'
            />
        ) : <SpeechEmpty status={summary.status}/>;
    }

    return (
        <>
            <div className='ktk-rhs__actions'>
                <button
                    className='ktk-back'
                    onClick={onBack}
                >
                    <ChevronLeftIcon
                        size={16}
                        weight={1.8}
                    />
                    {t('Назад')}
                </button>
            </div>
            <div className='ktk-rhs__scroll'>
                {data && <div className='ktk-detail-title'>{data.title}</div>}
                <div className='ktk-tabs'>
                    {['summary', 'protocol', 'transcript'].map((name) => (
                        <button
                            key={name}
                            className={'ktk-tab' + (section === name ? ' ktk-tab--active' : '')}
                            onClick={() => setSection(name)}
                        >
                            {t(({summary: 'Пересказ', protocol: 'Протокол', transcript: 'Транскрипт'} as Record<string, string>)[name])}
                        </button>
                    ))}
                </div>
                <div style={{paddingTop: 12}}>{body}</div>
            </div>
        </>
    );
}

const TABS = [
    {id: 'now', label: 'Сейчас'},
    {id: 'upcoming', label: 'Запланированные'},
    {id: 'recordings', label: 'Записи'},
];

const Sidebar = () => {
    const t = useT();
    const store = useStore();
    const style = useVisualStyle();
    const config = useConfig();
    const place = useCallPlace();
    const [tab, setTab] = useState('now');
    const [visited, setVisited] = useState<Record<string, boolean>>({now: true, upcoming: true});
    const [counts, setCounts] = useState<Record<string, number>>({});
    const opened = useSyncExternalStore(rhsRecording.subscribe, rhsRecording.get);
    const countNow = useCallback((count: number) => setCounts((old) => ({...old, now: count})), []);
    const countUpcoming = useCallback((count: number) => setCounts((old) => ({...old, upcoming: count})), []);

    let content: React.ReactNode;
    if (config && !config.configured) {
        content = <Empty text={t('Плагин не настроен: укажите адрес пространства в системной консоли.')}/>;
    } else if (opened) {
        content = (
            <RecordingDetails
                recordingKey={opened.key}
                section={opened.section}
                onBack={() => rhsRecording.set(null)}
            />
        );
    } else {
        content = (
            <>
                {/* A narrow panel keeps the main action labelled and turns the
                    others into icons; a wide one labels all three briefly. */}
                <div className='ktk-rhs__actions ktk-rhs__head'>
                    {/* The main part posts the meeting link to the chat; where
                        calls are possible, the arrow offers to ring instead. */}
                    <div className='ktk-split ktk-head__main'>
                        <button
                            className='ktk-btn ktk-btn--primary ktk-split__main'
                            aria-label={t('Начать встречу')}
                            title={place.rings ? t('Отправить ссылку на встречу в этот чат') : undefined}
                            onClick={() => startMeeting(store, undefined, '', true)}
                        >
                            <CameraIcon size={14}/>
                            <span className='ktk-narrow'>{t('Начать встречу')}</span>
                            <span className='ktk-wide'>{t('Начать')}</span>
                        </button>
                        {place.rings && (
                            <Dropdown
                                className='ktk-btn ktk-btn--primary ktk-split__more'
                                label={t('Другие способы начать встречу')}
                                items={[{
                                    key: 'call',
                                    icon: (
                                        <PhoneIcon
                                            size={18}
                                            weight={1.6}
                                        />
                                    ),
                                    label: t('Позвонить'),
                                    hint: t(place.group ? 'Участникам чата придёт входящий звонок' : 'Собеседнику придёт входящий звонок'),
                                    onSelect: () => startMeeting(store, undefined, '', false),
                                }]}
                            >
                                <span className='ktk-sr'>{t('Другие способы начать встречу')}</span>
                            </Dropdown>
                        )}
                    </div>
                    {!config?.link_only && (
                        <button
                            className='ktk-btn ktk-btn--secondary ktk-head__aux'
                            title={t('Запланировать встречу')}
                            aria-label={t('Запланировать встречу')}
                            onClick={() => runCommand(store, '/talk schedule')}
                        >
                            <CalendarIcon/>
                            <span className='ktk-wide'>{t('Запланировать')}</span>
                        </button>
                    )}
                    <button
                        className='ktk-btn ktk-btn--secondary ktk-head__aux'
                        title={t('Моя комната')}
                        aria-label={t('Моя комната')}
                        onClick={() => openPersonalRoom(store)}
                    >
                        <RoomIcon/>
                        <span className='ktk-wide'>{t('Комната')}</span>
                    </button>
                </div>
                {!config?.link_only && (
                    <div
                        className='ktk-tabs'
                        role='tablist'
                    >
                        {TABS.map((item) => (
                            <button
                                key={item.id}
                                role='tab'
                                aria-selected={tab === item.id}
                                className={'ktk-tab' + (tab === item.id ? ' ktk-tab--active' : '')}
                                onClick={() => {
                                    setTab(item.id);
                                    setVisited((old) => ({...old, [item.id]: true}));
                                }}
                            >
                                {t(item.label)}
                                {counts[item.id] > 0 && <span className='ktk-tab__count'>{counts[item.id]}</span>}
                            </button>
                        ))}
                    </div>
                )}
                {!config?.link_only && (
                    <div className='ktk-rhs__scroll'>
                        <div
                            className='ktk-panel'
                            hidden={tab !== 'now'}
                        >
                            <NowTab onCount={countNow}/>
                        </div>
                        <div
                            className='ktk-panel'
                            hidden={tab !== 'upcoming'}
                        >
                            <UpcomingTab onCount={countUpcoming}/>
                        </div>
                        {visited.recordings && (
                            <div
                                className='ktk-panel'
                                hidden={tab !== 'recordings'}
                            >
                                <RecordingsTab onOpen={(key) => rhsRecording.set({key, section: 'summary'})}/>
                            </div>
                        )}
                    </div>
                )}
            </>
        );
    }

    return (
        <div
            className='ktk-root ktk-rhs'
            data-ktk-style={style}
        >
            {content}
        </div>
    );
};

export default Sidebar;
