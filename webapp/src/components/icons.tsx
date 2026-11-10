import React from 'react';

// All icons share a 24 unit grid and stay symmetric around its centre. The
// camera is the same shape as the bot avatar and assets/icon.svg.
export const CameraIcon = ({size = 18, className}: {size?: number; className?: string}) => (
    <svg
        className={className}
        width={size}
        height={size}
        viewBox='0 0 24 24'
        fill='currentColor'
        aria-hidden='true'
    >
        <rect
            x='2'
            y='6'
            width='13'
            height='12'
            rx='3'
        />
        <path
            d='M17 10.4 21 8v8l-4-2.4z'
            stroke='currentColor'
            strokeWidth='1.6'
            strokeLinejoin='round'
        />
    </svg>
);

const Line = ({size = 14, weight = 2, children}: {size?: number; weight?: number; children: React.ReactNode}) => (
    <svg
        width={size}
        height={size}
        viewBox='0 0 24 24'
        fill='none'
        stroke='currentColor'
        strokeWidth={weight}
        strokeLinecap='round'
        strokeLinejoin='round'
        aria-hidden='true'
    >
        {children}
    </svg>
);

type IconProps = {size?: number; weight?: number};

export const CalendarIcon = ({size}: IconProps) => (
    <Line size={size}>
        <rect
            x='3'
            y='5'
            width='18'
            height='16'
            rx='3'
        />
        <path d='M3 10h18M8 3v4M16 3v4'/>
    </Line>
);

export const PhoneIcon = ({size, weight}: IconProps) => (
    <Line
        size={size}
        weight={weight}
    >
        <path d='M5 4h4l2 5-2.5 1.5a11 11 0 0 0 5 5L15 13l5 2v4a2 2 0 0 1-2 2A16 16 0 0 1 3 6a2 2 0 0 1 2-2z'/>
    </Line>
);

export const CopyIcon = ({size}: IconProps) => (
    <Line size={size}>
        <rect
            x='9'
            y='9'
            width='12'
            height='12'
            rx='2'
        />
        <path d='M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1'/>
    </Line>
);

export const CheckIcon = ({size}: IconProps) => (
    <Line size={size}>
        <path d='M4 12.5 9.5 18 20 6.5'/>
    </Line>
);

export const InfoIcon = ({size}: IconProps) => (
    <Line size={size}>
        <circle
            cx='12'
            cy='12'
            r='9'
        />
        <path d='M12 11v5M12 8h.01'/>
    </Line>
);

export const CrossIcon = ({size}: IconProps) => (
    <Line size={size}>
        <path d='M6 6l12 12M18 6 6 18'/>
    </Line>
);

export const ExternalIcon = ({size}: IconProps) => (
    <Line size={size}>
        <path d='M14 4h6v6M20 4l-9 9M18 14v4a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4'/>
    </Line>
);

export const RecordIcon = ({size}: IconProps) => (
    <Line size={size}>
        <circle
            cx='12'
            cy='12'
            r='9'
        />
        <circle
            cx='12'
            cy='12'
            r='3.5'
            fill='currentColor'
            stroke='none'
        />
    </Line>
);

export const StopIcon = ({size}: IconProps) => (
    <Line size={size}>
        <rect
            x='5'
            y='5'
            width='14'
            height='14'
            rx='2.5'
        />
    </Line>
);

export const EditIcon = ({size}: IconProps) => (
    <Line size={size}>
        <path d='M4 20h4L19.5 8.5a2.12 2.12 0 0 0-3-3L5 17z'/>
    </Line>
);

export const DocumentIcon = ({size}: IconProps) => (
    <Line size={size}>
        <path d='M6 3h8l5 5v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z'/>
        <path d='M14 3v5h5M8 13h8M8 17h6'/>
    </Line>
);

export const RoomIcon = ({size}: IconProps) => (
    <Line size={size}>
        <path d='M3 12 12 4l9 8'/>
        <path d='M6 10v9a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1v-9'/>
    </Line>
);

export const LinkIcon = ({size, weight}: IconProps) => (
    <Line
        size={size}
        weight={weight}
    >
        <path d='M10.336 13.664a5.06 5.06 0 0 0 6.659 0L18.659 12a4.709 4.709 0 0 0-6.66-6.66m1.665 4.996a5.06 5.06 0 0 0-6.66 0L5.342 12A4.709 4.709 0 0 0 12 18.66'/>
    </Line>
);

export const ChevronLeftIcon = ({size, weight}: IconProps) => (
    <Line
        size={size}
        weight={weight}
    >
        <path d='M15 5l-7 7 7 7'/>
    </Line>
);

/** Something still being prepared. */
export const ClockIcon = ({size, weight}: IconProps) => (
    <Line
        size={size}
        weight={weight}
    >
        <circle
            cx='12'
            cy='12'
            r='9'
        />
        <path d='M12 7v5l3 2'/>
    </Line>
);

/** Something that will not come. */
export const UnavailableIcon = ({size, weight}: IconProps) => (
    <Line
        size={size}
        weight={weight}
    >
        <circle
            cx='12'
            cy='12'
            r='9'
        />
        <path d='M5.6 5.6l12.8 12.8'/>
    </Line>
);
