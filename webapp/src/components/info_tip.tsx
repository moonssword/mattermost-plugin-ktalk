import React, {useEffect, useRef, useState} from 'react';

import {useT} from '../i18n';

import {InfoIcon} from './icons';

let nextId = 0;

/**
 * An "i" mark that explains a terse line. The text shows on hover and keyboard
 * focus, and a click pins it open, which is what touch screens rely on.
 */
export function InfoTip({text}: {text: string}) {
    const t = useT();
    const [open, setOpen] = useState(false);
    const [id] = useState(() => `ktk-tip-${++nextId}`);
    const root = useRef<HTMLSpanElement>(null);

    useEffect(() => {
        if (!open) {
            return undefined;
        }
        const outside = (event: PointerEvent) => {
            if (!root.current?.contains(event.target as Node)) {
                setOpen(false);
            }
        };
        const escape = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                setOpen(false);
            }
        };
        document.addEventListener('pointerdown', outside);
        document.addEventListener('keydown', escape);
        return () => {
            document.removeEventListener('pointerdown', outside);
            document.removeEventListener('keydown', escape);
        };
    }, [open]);

    return (
        <span
            ref={root}
            className='ktk-tip'
            data-open={open}
        >
            <button
                type='button'
                className='ktk-tip__btn'
                aria-label={t('Подробнее')}
                aria-describedby={id}
                aria-expanded={open}
                onClick={() => setOpen((value) => !value)}
            >
                <InfoIcon size={14}/>
            </button>
            <span
                id={id}
                role='tooltip'
                className='ktk-tip__bubble'
            >
                {text}
            </span>
        </span>
    );
}
