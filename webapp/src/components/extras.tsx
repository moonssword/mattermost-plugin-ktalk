import React, {useEffect, useState, useSyncExternalStore} from 'react';
import {useStore} from 'react-redux';

import {callUser, getConfig, openExternal} from '../actions';
import {toastMessage} from '../bus';
import {api} from '../client';
import {useT} from '../i18n';
import {formatClock} from '../logic';

import {CameraIcon, CheckIcon, CrossIcon, ExternalIcon, PhoneIcon} from './icons';
import {useVisualStyle} from './use_style';

export const Toast = () => {
    const message = useSyncExternalStore(toastMessage.subscribe, toastMessage.get);
    if (!message) {
        return null;
    }
    return (
        <div
            className='ktk-toast'
            role='alert'
        >
            {message}
        </div>
    );
};

function useTalkStatus(userId?: string): string {
    const [status, setStatus] = useState('');
    useEffect(() => {
        let cancelled = false;
        if (userId && !getConfig()?.link_only) {
            api<{status: string}>('GET', `/users/${userId}/status`).then((result) => {
                if (!cancelled) {
                    setStatus(result.status);
                }
            }).catch(() => undefined);
        }
        return () => {
            cancelled = true;
        };
    }, [userId]);
    return status;
}

const STATUS_TEXT: Record<string, string> = {inMeeting: 'на встрече', doNotDisturb: 'не беспокоить', online: 'в сети'};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const PopoverStatus = ({user}: {user?: any}) => {
    const t = useT();
    const status = useTalkStatus(user?.id);
    const text = STATUS_TEXT[status];
    if (!text) {
        return null;
    }
    return <div className='ktk-popover'>{`${t('В Толке')}: ${t(text)}`}</div>;
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const PopoverCall = ({user, hide}: {user?: any; hide?: () => void}) => {
    const t = useT();
    const store = useStore();
    const style = useVisualStyle();
    const currentUserId = (store.getState() as {entities: {users: {currentUserId: string}}}).entities.users.currentUserId;
    if (!user || user.is_bot || user.id === currentUserId || !getConfig()?.configured) {
        return null;
    }
    return (
        <div
            className='ktk-root ktk-popover-action'
            data-ktk-style={style}
        >
            <button
                className='ktk-btn ktk-btn--secondary'
                onClick={() => {
                    hide?.();
                    callUser(store, user.id);
                }}
            >
                <PhoneIcon/>
                {t('Позвонить в Толке')}
            </button>
        </div>
    );
};

interface Preview {
    type: string;
    url: string;
    title?: string;
    users_online?: number;
    status?: string;
    duration?: number;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const LinkEmbed = ({embed}: {embed?: any}) => {
    const t = useT();
    const style = useVisualStyle();
    const url: string | undefined = embed?.url;
    const [preview, setPreview] = useState<Preview | null>(null);
    useEffect(() => {
        if (url) {
            api<Preview>('GET', `/preview?url=${encodeURIComponent(url)}`).then(setPreview).catch(() => undefined);
        }
    }, [url]);
    if (!url) {
        return null;
    }
    const isRecording = preview?.type === 'recording';
    const meta = [
        preview?.status === 'live' && `${t('В комнате')}: ${preview.users_online}`,
        isRecording && preview?.duration ? formatClock(preview.duration) : '',
    ].filter(Boolean).join(' · ');
    return (
        <div
            className='ktk-root'
            data-ktk-style={style}
        >
        <div className='ktk-card'>
            <div className='ktk-card__head'>
                <div className='ktk-card__icon'><CameraIcon/></div>
                <div className='ktk-card__body'>
                    <div className='ktk-card__title'>{preview?.title || t(isRecording ? 'Запись в Контур.Толке' : 'Встреча в Контур.Толке')}</div>
                    {meta && <div className='ktk-card__status'>{meta}</div>}
                </div>
            </div>
            <div className='ktk-actions'>
                <button
                    className='ktk-btn ktk-btn--primary'
                    onClick={() => openExternal(url)}
                >
                    {isRecording ? <ExternalIcon/> : <CameraIcon size={14}/>}
                    {t(isRecording ? 'Открыть в Толке' : 'Присоединиться')}
                </button>
            </div>
        </div>
        </div>
    );
};

interface AdminStatus {
    configured: boolean;
    space_url: string;
    has_key: boolean;
    webhooks_enabled: boolean;
    webhook_target: string;
    webhook?: {activated: boolean; last_event: string};
    test_mode?: {users: string[]; unknown: string[]; admins?: boolean};
    key_error?: string;
    expires_at?: string;
    tariff_expires_at?: string;
    online?: {usersCount: number; conferencesCount: number};
    features?: Array<{scope: string; feature: string; ok: boolean}>;
}

/** Who can use the plugin in test mode, as the System Console says it. */
function testModeText(users: string[], admins: boolean): string {
    if (users.length === 0 && !admins) {
        return 'Режим тестирования включён, но тестировщиков нет: плагином сейчас не пользуется никто. Добавьте логины в «Тестировщики» или откройте доступ системным администраторам.';
    }
    const who = [admins ? 'системные администраторы' : '', users.join(', ')].filter(Boolean).join(' и ');
    return `Режим тестирования включён. Плагином пользуются ${who}.`;
}

// System Console text stays in Russian: administrators read it alongside the
// Russian setting labels from plugin.json.
export const AdminStatusPanel = () => {
    const [status, setStatus] = useState<AdminStatus | null>(null);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);

    const load = async () => {
        setBusy(true);
        try {
            setStatus(await api<AdminStatus>('GET', '/admin/status'));
            setError('');
        } catch (e) {
            setError((e as Error).message);
        }
        setBusy(false);
    };
    useEffect(() => {
        load();
    }, []);

    const webhook = async (method: string) => {
        setBusy(true);
        try {
            await api(method, '/admin/webhook');
            setError('');
            await new Promise((resolve) => setTimeout(resolve, 2000));
        } catch (e) {
            setError((e as Error).message);
        }
        await load();
    };

    const date = (value?: string) => (value ? new Date(value).toLocaleDateString('ru') : '');
    return (
        <div className='ktk-root'>
            {error && <div className='ktk-error'>{error}</div>}
            {status && !status.configured && <div>{'Укажите адрес пространства ниже и сохраните настройки.'}</div>}
            {status?.configured && !status.has_key && <div>{'Режим ссылок: API-ключ не задан. Встречи создаются, но управление, статусы и записи недоступны.'}</div>}
            {status?.key_error && <div className='ktk-error'>{status.key_error}</div>}
            {status?.test_mode && (
                <div>
                    <div className={status.test_mode.users.length === 0 && !status.test_mode.admins ? 'ktk-missing' : undefined}>
                        {testModeText(status.test_mode.users, Boolean(status.test_mode.admins))}
                    </div>
                    {status.test_mode.unknown.length > 0 && <div className='ktk-missing'>{`Таких пользователей нет: ${status.test_mode.unknown.join(', ')}.`}</div>}
                </div>
            )}
            {status?.expires_at && <div>{`API-ключ действует до ${date(status.expires_at)}.`}</div>}
            {status?.tariff_expires_at && <div>{`Тариф действует до ${date(status.tariff_expires_at)}.`}</div>}
            {status?.online && <div>{`Сейчас идёт встреч: ${status.online.conferencesCount}, участников: ${status.online.usersCount}.`}</div>}
            {status?.features && (
                <table className='ktk-table'>
                    <tbody>
                        {status.features.map((item) => (
                            <tr key={item.scope}>
                                <td className={item.ok ? 'ktk-ok' : 'ktk-missing'}>{item.ok ? <CheckIcon size={16}/> : <CrossIcon size={16}/>}</td>
                                <td>{item.feature}</td>
                                <td><code>{item.scope}</code></td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}
            {status?.webhooks_enabled && (
                <div>
                    <div>
                        {'Вебхук: '}
                        {status.webhook ? (status.webhook.activated ? 'активен' : 'ждёт активации') : 'не зарегистрирован'}
                        {` (${status.webhook_target})`}
                    </div>
                    <div className='ktk-actions'>
                        <button
                            type='button'
                            className='ktk-btn ktk-btn--secondary'
                            disabled={busy}
                            onClick={() => webhook('POST')}
                        >
                            {'Зарегистрировать вебхук'}
                        </button>
                        {status.webhook && (
                            <button
                                type='button'
                                className='ktk-btn ktk-btn--danger'
                                disabled={busy}
                                onClick={() => webhook('DELETE')}
                            >
                                {'Удалить вебхук'}
                            </button>
                        )}
                    </div>
                </div>
            )}
            <div className='ktk-actions'>
                <button
                    type='button'
                    className='ktk-btn ktk-btn--secondary'
                    disabled={busy}
                    onClick={load}
                >
                    {'Проверить подключение'}
                </button>
            </div>
        </div>
    );
};
