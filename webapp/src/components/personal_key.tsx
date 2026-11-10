import React, {useEffect, useState} from 'react';

import {loadConfig} from '../actions';
import {meetingsVersion} from '../bus';
import {forgetAllMeetingRights} from '../meeting_rights';
import {api} from '../client';
import {useT} from '../i18n';

import {CheckIcon, CrossIcon, InfoIcon} from './icons';
import {InfoTip} from './info_tip';
import {useConfig, useVisualStyle} from './use_style';

interface KeyStatus {
    allowed: boolean;
    connected: boolean;
    name?: string;
}

const OFFER_HIDDEN = 'konturtalk.personal_key_offer_hidden';

function offerHidden(): boolean {
    try {
        return localStorage.getItem(OFFER_HIDDEN) === '1';
    } catch {
        return false;
    }
}

function hideOffer() {
    try {
        localStorage.setItem(OFFER_HIDDEN, '1');
    } catch {
        // Without storage the line simply comes back next time.
    }
}

const ABOUT_KEY = 'С личным ключом Толка плагин действует от вашего имени и с вашими правами в Толке: создаёт комнаты и встречи, управляет ими, отвечает на приглашения и показывает ваши записи.';

function KeyForm({token, busy, error, onToken, onSubmit, onCancel, cancelLabel = 'Отмена'}: {
    token: string;
    busy: boolean;
    error: string;
    onToken: (value: string) => void;
    onSubmit: (event: React.FormEvent) => void;
    onCancel?: () => void;
    cancelLabel?: string;
}) {
    const t = useT();
    return (
        <form
            className='ktk-key__form'
            onSubmit={onSubmit}
        >
            <input
                className='ktk-input'
                type='password'
                autoComplete='off'
                spellCheck={false}
                placeholder={t('Личный ключ Толка')}
                aria-label={t('Личный ключ Толка')}
                value={token}
                onChange={(e) => onToken(e.target.value)}
            />
            {error && <div className='ktk-key__error'>{error}</div>}
            <div className='ktk-key__hint'>
                {t('Где взять: в Толке нажмите на аватар → Настройки → API ключи → Выпустить ключ. Ключ хранится в зашифрованном виде.')}
            </div>
            <div className='ktk-key__actions'>
                <button
                    type='submit'
                    className='ktk-btn ktk-btn--primary'
                    disabled={busy}
                >
                    {t(busy ? 'Проверяем…' : 'Подключить')}
                </button>
                {onCancel && (
                    <button
                        type='button'
                        className='ktk-btn ktk-btn--secondary'
                        onClick={onCancel}
                    >
                        {t(cancelLabel)}
                    </button>
                )}
            </div>
        </form>
    );
}

/**
 * Connects the user's personal Talk key without a slash command. With it,
 * meetings are planned under the user's own name and invitations can be
 * answered from Mattermost. The key is sent once and never shown again.
 *
 * In settings it is a full panel. On a first visit the sidebar shows the full
 * panel too, with "Later" folding it into one quiet line whose explanation
 * sits behind an "i" mark.
 */
export function PersonalKeyPanel({inline = false, onLater}: {inline?: boolean; onLater?: () => void}) {
    const t = useT();
    const config = useConfig();
    const [status, setStatus] = useState<KeyStatus | null>(null);
    const [open, setOpen] = useState(!inline);
    const [hidden, setHidden] = useState(inline && offerHidden());
    const [token, setToken] = useState('');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');

    useEffect(() => {
        api<KeyStatus>('GET', '/personal-key').then(setStatus).catch(() => setStatus(null));
    }, [config?.personal_key]);

    if (!status?.allowed) {
        return null;
    }

    const changed = async (next: KeyStatus) => {
        setStatus(next);
        setToken('');
        setError('');
        await loadConfig();
        forgetAllMeetingRights();
        meetingsVersion.set(meetingsVersion.get() + 1);
    };

    const connect = async (event: React.FormEvent) => {
        event.preventDefault();
        if (!token.trim()) {
            setError(t('Вставьте ключ.'));
            return;
        }
        setBusy(true);
        try {
            await changed(await api<KeyStatus>('POST', '/personal-key', {token}));
        } catch (e) {
            setError((e as Error).message);
        } finally {
            setBusy(false);
        }
    };

    const disconnect = async () => {
        setBusy(true);
        try {
            await changed(await api<KeyStatus>('DELETE', '/personal-key'));
            setOpen(!inline);
        } catch (e) {
            setError((e as Error).message);
        } finally {
            setBusy(false);
        }
    };

    const form = (
        <KeyForm
            token={token}
            busy={busy}
            error={error}
            onToken={(value) => {
                setToken(value);
                setError('');
            }}
            onSubmit={connect}
            onCancel={inline ? () => {
                setOpen(false);
                setError('');
            } : onLater}
            cancelLabel={inline ? 'Отмена' : 'Позже'}
        />
    );

    if (inline) {
        if (status.connected) {
            return (
                <div className='ktk-keyline-wrap'>
                    <div
                        className='ktk-keyline'
                        role='status'
                    >
                        <InfoTip text={t(ABOUT_KEY)}/>
                        <span className='ktk-keyline__text'>
                            {status.name ? <>{t('От имени')} <b>{status.name}</b></> : t('Личный ключ Толка подключён')}
                        </span>
                        <button
                            type='button'
                            className='ktk-link'
                            disabled={busy}
                            onClick={disconnect}
                        >
                            {t('Отключить')}
                        </button>
                    </div>
                    {error && <div className='ktk-key__error'>{error}</div>}
                </div>
            );
        }
        if (hidden && !open) {
            return null;
        }
        return (
            <div className='ktk-keyline-wrap'>
                <div className='ktk-keyline ktk-keyline--offer'>
                    <InfoTip text={t(ABOUT_KEY)}/>
                    <span className='ktk-keyline__text'>
                        {t('Планирование, управление встречами и записи доступны с личным ключом')}
                        {!open && (
                            <>
                                {' · '}
                                <button
                                    type='button'
                                    className='ktk-link'
                                    onClick={() => setOpen(true)}
                                >
                                    {t('Подключить ключ')}
                                </button>
                            </>
                        )}
                    </span>
                    {!open && (
                        <button
                            type='button'
                            className='ktk-keyline__close'
                            aria-label={t('Скрыть')}
                            title={t('Скрыть')}
                            onClick={() => {
                                hideOffer();
                                setHidden(true);
                            }}
                        >
                            <CrossIcon size={12}/>
                        </button>
                    )}
                </div>
                {open && form}
            </div>
        );
    }

    if (status.connected) {
        return (
            <div
                className='ktk-notice ktk-key'
                role='status'
            >
                <CheckIcon size={16}/>
                <div className='ktk-key__body'>
                    <div>
                        {t('Личный ключ подключён')}
                        {status.name ? ` · ${status.name}` : ''}
                    </div>
                    {error && <div className='ktk-key__error'>{error}</div>}
                </div>
                <button
                    type='button'
                    className='ktk-link'
                    disabled={busy}
                    onClick={disconnect}
                >
                    {t('Отключить')}
                </button>
            </div>
        );
    }

    return (
        <div className='ktk-notice ktk-key'>
            <InfoIcon size={16}/>
            <div className='ktk-key__body'>
                <div>{t('Подключите личный ключ Толка, чтобы планировать встречи, управлять ими, открывать свою комнату и смотреть записи. В Толке всё это будет выполняться от вашего имени.')}</div>
                {open && form}
            </div>
        </div>
    );
}

/** The same panel inside Mattermost's settings, which has no plugin styles of its own. */
export function PersonalKeySetting() {
    const style = useVisualStyle();
    return (
        <div
            className='ktk-root'
            data-ktk-style={style}
        >
            <PersonalKeyPanel/>
        </div>
    );
}
