import React, {useEffect, useRef, useSyncExternalStore} from 'react';
import {useSelector, useStore} from 'react-redux';
import type {Store} from 'redux';

import {getConfig, startMeeting} from '../actions';
import {callMenu} from '../bus';
import {useT} from '../i18n';

import {CameraIcon, LinkIcon, PhoneIcon} from './icons';
import {useConfig, useVisualStyle} from './use_style';

const MENU_WIDTH = 300;

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function channelType(state: any, channelId: string): string {
    return state.entities.channels.channels[channelId]?.type || '';
}

function isChat(type: string): boolean {
    return type === 'D' || type === 'G';
}

/** A direct or group chat rings only when the administrator allows calls. */
function ringsIn(type: string, config = getConfig()): boolean {
    return isChat(type) && Boolean(config?.calls);
}

/**
 * The menu opens under the call button of the channel header. Mattermost
 * draws either this plugin's button or, with Calls installed, a merged menu
 * whose items vanish once chosen, so the header button is the stable anchor.
 */
function anchorElement(): HTMLElement | null {
    return document.querySelector<HTMLElement>('#channel-header .call-button') ||
        (document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null);
}

/**
 * The header call button. In a channel it posts a meeting card, which is a
 * link for everyone. In a direct or group chat it asks first: ring the others
 * or only post the link.
 */
export function callButtonAction(store: Store, channelId?: string) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const state = store.getState() as any;
    const channel = channelId || state.entities.channels.currentChannelId;
    const type = channelType(state, channel);
    if (!ringsIn(type)) {
        startMeeting(store, channel, '', true);
        return;
    }
    const anchor = anchorElement();
    const rect = anchor?.getBoundingClientRect();
    const left = rect ? Math.max(8, Math.min(rect.right - MENU_WIDTH, window.innerWidth - MENU_WIDTH - 8)) : (window.innerWidth - MENU_WIDTH) / 2;
    callMenu.set({channelId: channel, group: type === 'G', top: rect ? rect.bottom + 4 : 64, left, anchor});
}

/** Labels of the header button follow the channel it is drawn in. */
/**
 * Where the header button is drawn and whether it rings there. The plugin
 * settings arrive after Mattermost draws the header, so they are followed
 * too, or the label would keep what it showed before they loaded.
 */
export function useCallPlace(): {chat: boolean; group: boolean; rings: boolean} {
    const config = useConfig();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const type = useSelector((state: any) => channelType(state, state.entities.channels.currentChannelId));
    return {chat: isChat(type), group: type === 'G', rings: ringsIn(type, config)};
}

export const HeaderCallButton = () => {
    const t = useT();
    const {rings} = useCallPlace();
    const label = t(rings ? 'Позвонить или отправить ссылку' : 'Отправить ссылку на встречу');
    return (
        <button
            className='btn btn-sm btn-tertiary call-button'
            aria-label={label}
            aria-haspopup={rings ? 'menu' : undefined}
            title={label}
        >
            <CameraIcon size={16}/>
        </button>
    );
};

export const CallDropdownSublabel = () => {
    const t = useT();
    const {chat, rings} = useCallPlace();
    if (rings) {
        return <>{t('Звонок или ссылка')}</>;
    }
    return <>{t(chat ? 'Ссылка в этом чате' : 'Ссылка в этом канале')}</>;
};

/** The two choices of a direct or group chat, in Mattermost's menu style. */
const CallMenuRoot = () => {
    const menu = useSyncExternalStore(callMenu.subscribe, callMenu.get);
    const store = useStore();
    const t = useT();
    const style = useVisualStyle();
    const ref = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!menu) {
            return undefined;
        }
        ref.current?.querySelector<HTMLButtonElement>('button')?.focus();
        const close = (restoreFocus: boolean) => {
            callMenu.set(null);
            if (restoreFocus) {
                menu.anchor?.focus();
            }
        };
        const onPointer = (e: PointerEvent) => {
            if (!ref.current?.contains(e.target as Node)) {
                close(false);
            }
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.preventDefault();
                close(true);
                return;
            }
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                e.preventDefault();
                const items = Array.from(ref.current?.querySelectorAll<HTMLButtonElement>('button') || []);
                const index = items.indexOf(document.activeElement as HTMLButtonElement);
                const next = e.key === 'ArrowDown' ? index + 1 : index - 1;
                items[(next + items.length) % items.length]?.focus();
            }
        };
        const onResize = () => close(false);
        document.addEventListener('pointerdown', onPointer, true);
        document.addEventListener('keydown', onKey, true);
        window.addEventListener('resize', onResize);
        return () => {
            document.removeEventListener('pointerdown', onPointer, true);
            document.removeEventListener('keydown', onKey, true);
            window.removeEventListener('resize', onResize);
        };
    }, [menu]);

    if (!menu) {
        return null;
    }
    const choose = (linkOnly: boolean) => {
        callMenu.set(null);
        // A link sent to a chat opens nothing; a call opens the room to wait in.
        startMeeting(store as Store, menu.channelId, '', linkOnly, !linkOnly);
    };
    return (
        <div
            ref={ref}
            className='ktk-root ktk-callmenu'
            data-ktk-style={style}
            role='menu'
            aria-label={t('Встреча в Контур.Толке')}
            style={{top: menu.top, left: menu.left, width: MENU_WIDTH}}
        >
            <button
                role='menuitem'
                className='ktk-callmenu__item'
                onClick={() => choose(false)}
            >
                <PhoneIcon
                    size={18}
                    weight={1.6}
                />
                <span>
                    <span className='ktk-callmenu__label'>{t('Позвонить')}</span>
                    <span className='ktk-callmenu__hint'>{t(menu.group ? 'Участникам чата придёт входящий звонок' : 'Собеседнику придёт входящий звонок')}</span>
                </span>
            </button>
            <button
                role='menuitem'
                className='ktk-callmenu__item'
                onClick={() => choose(true)}
            >
                <LinkIcon
                    size={18}
                    weight={1.6}
                />
                <span>
                    <span className='ktk-callmenu__label'>{t('Отправить ссылку')}</span>
                    <span className='ktk-callmenu__hint'>{t('Карточка встречи в чате, без звонка')}</span>
                </span>
            </button>
        </div>
    );
};

export default CallMenuRoot;
