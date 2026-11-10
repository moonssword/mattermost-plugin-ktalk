import React, {useEffect, useLayoutEffect, useRef, useState} from 'react';
import {createPortal} from 'react-dom';

import {useVisualStyle} from './use_style';

export interface DropdownItem {
    key: string;
    label: string;
    hint?: string;
    icon?: React.ReactNode;
    /** Marks the preselected choice, as a radio item. */
    selected?: boolean;
    onSelect: () => void;
}

interface Props {
    /** The button's own content: an icon and a label. */
    children: React.ReactNode;
    className: string;
    /** Names the menu for screen readers. */
    label: string;
    /** A heading above the items, such as the question the choice answers. */
    title?: string;
    items: DropdownItem[];
}

const WIDTH = 300;
const GAP = 4;

/**
 * A button with a menu. The menu is drawn over the page rather than inside
 * the card, which clips its content in some styles, and closes on a choice,
 * a click elsewhere, Escape, scrolling or resizing.
 */
const Dropdown = ({children, className, label, title, items}: Props) => {
    const style = useVisualStyle();
    const button = useRef<HTMLButtonElement>(null);
    const menu = useRef<HTMLDivElement>(null);
    const [place, setPlace] = useState<{top: number; left: number} | null>(null);
    const open = place !== null;

    const close = (restoreFocus: boolean) => {
        setPlace(null);
        if (restoreFocus) {
            button.current?.focus();
        }
    };

    const toggle = () => {
        if (open || !button.current) {
            setPlace(null);
            return;
        }
        const rect = button.current.getBoundingClientRect();
        const left = Math.max(8, Math.min(rect.left, window.innerWidth - WIDTH - 8));
        setPlace({top: rect.bottom + GAP, left});
    };

    // Opens upwards when there is no room below.
    useLayoutEffect(() => {
        if (!place || !menu.current || !button.current) {
            return;
        }
        const height = menu.current.offsetHeight;
        const rect = button.current.getBoundingClientRect();
        if (place.top + height > window.innerHeight - 8 && rect.top - height - GAP > 8) {
            setPlace({top: rect.top - height - GAP, left: place.left});
        }
    }, [place]);

    useEffect(() => {
        if (!open) {
            return undefined;
        }
        const checked = menu.current?.querySelector<HTMLButtonElement>('[aria-checked="true"]');
        (checked || menu.current?.querySelector<HTMLButtonElement>('button'))?.focus();
        const onPointer = (e: PointerEvent) => {
            const target = e.target as Node;
            if (!menu.current?.contains(target) && !button.current?.contains(target)) {
                close(false);
            }
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.preventDefault();
                e.stopPropagation();
                close(true);
                return;
            }
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                e.preventDefault();
                const list = Array.from(menu.current?.querySelectorAll<HTMLButtonElement>('button') || []);
                const index = list.indexOf(document.activeElement as HTMLButtonElement);
                const next = e.key === 'ArrowDown' ? index + 1 : index - 1;
                list[(next + list.length) % list.length]?.focus();
            }
        };
        const onMove = (e: Event) => {
            if (!menu.current?.contains(e.target as Node)) {
                close(false);
            }
        };
        document.addEventListener('pointerdown', onPointer, true);
        document.addEventListener('keydown', onKey, true);
        window.addEventListener('scroll', onMove, true);
        window.addEventListener('resize', onMove);
        return () => {
            document.removeEventListener('pointerdown', onPointer, true);
            document.removeEventListener('keydown', onKey, true);
            window.removeEventListener('scroll', onMove, true);
            window.removeEventListener('resize', onMove);
        };
    }, [open]);

    const radio = items.some((item) => item.selected !== undefined);
    return (
        <>
            <button
                ref={button}
                className={className}
                aria-haspopup='menu'
                aria-expanded={open}
                onClick={toggle}
            >
                {children}
                <svg
                    className='ktk-caret'
                    width='10'
                    height='10'
                    viewBox='0 0 24 24'
                    fill='none'
                    stroke='currentColor'
                    strokeWidth='2.4'
                    strokeLinecap='round'
                    strokeLinejoin='round'
                    aria-hidden='true'
                >
                    <path d={open ? 'M6 15l6-6 6 6' : 'M6 9l6 6 6-6'}/>
                </svg>
            </button>
            {place && createPortal(
                <div
                    ref={menu}
                    className='ktk-root ktk-callmenu'
                    data-ktk-style={style}
                    role='menu'
                    aria-label={label}
                    style={{top: place.top, left: place.left, width: WIDTH}}
                >
                    {title && <div className='ktk-callmenu__title'>{title}</div>}
                    {items.map((item) => (
                        <button
                            key={item.key}
                            role={radio ? 'menuitemradio' : 'menuitem'}
                            aria-checked={radio ? Boolean(item.selected) : undefined}
                            className='ktk-callmenu__item'
                            onClick={() => {
                                close(false);
                                item.onSelect();
                            }}
                        >
                            {item.icon}
                            {radio && (
                                <span
                                    className='ktk-callmenu__radio'
                                    aria-hidden='true'
                                />
                            )}
                            <span>
                                <span className='ktk-callmenu__label'>{item.label}</span>
                                {item.hint && <span className='ktk-callmenu__hint'>{item.hint}</span>}
                            </span>
                        </button>
                    ))}
                </div>,
                document.body,
            )}
        </>
    );
};

export default Dropdown;
