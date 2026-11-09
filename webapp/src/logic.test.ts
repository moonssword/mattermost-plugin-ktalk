import {canJoin, chipKind, cleanRecentStatuses, colorIndex, csrfFromCookie, effectiveStyle, filterTranscript, formatClock, incomingAnchor, initials, isSpaceLink, isTrustedCard, isWebUrl, PERSON_COLORS, repeatText, secondsBetween, speakerColors} from './logic';
import type {MeetingView} from './types';

const base = {status: 'waiting'} as MeetingView;

describe('formatClock', () => {
    it('formats minutes and hours', () => {
        expect(formatClock(65)).toBe('01:05');
        expect(formatClock(3725)).toBe('1:02:05');
        expect(formatClock(-5)).toBe('00:00');
    });
});

describe('secondsBetween', () => {
    it('handles open and closed ranges', () => {
        expect(secondsBetween(null)).toBe(0);
        expect(secondsBetween('2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z')).toBe(60);
        expect(secondsBetween('2026-01-01T00:00:00Z', null, Date.parse('2026-01-01T00:00:30Z'))).toBe(30);
    });
});

describe('canJoin', () => {
    const start = '2026-11-08T10:00:00Z';
    const scheduled = {...base, status: 'scheduled', schedule: {start, end: start}} as MeetingView;
    it('opens a scheduled meeting ten minutes early', () => {
        expect(canJoin(scheduled, Date.parse('2026-11-08T09:49:00Z'))).toBe(false);
        expect(canJoin(scheduled, Date.parse('2026-11-08T09:51:00Z'))).toBe(true);
    });
    it('closes finished meetings', () => {
        expect(canJoin({...base, status: 'ended'}, 0)).toBe(false);
        expect(canJoin({...base, status: 'cancelled'}, 0)).toBe(false);
        expect(canJoin(base, 0)).toBe(true);
    });
});

describe('isSpaceLink', () => {
    const space = 'https://x.ktalk.ru';
    it('matches rooms and recordings of the space only', () => {
        expect(isSpaceLink('https://x.ktalk.ru/abc123', space)).toBe(true);
        expect(isSpaceLink('https://x.ktalk.ru/recordings/k1', space)).toBe(true);
        expect(isSpaceLink('https://other.ktalk.ru/abc123', space)).toBe(false);
        expect(isSpaceLink('https://x.ktalk.ru/content/artifacts', space)).toBe(false);
        expect(isSpaceLink('not a url', space)).toBe(false);
        expect(isSpaceLink(undefined, space)).toBe(false);
    });
});

describe('filterTranscript', () => {
    const lines = [{speaker: 'Анна', text: 'Привет'}, {speaker: 'Иван', text: 'План на неделю'}];
    it('searches text and speaker, ignoring case', () => {
        expect(filterTranscript(lines, 'ПЛАН')).toHaveLength(1);
        expect(filterTranscript(lines, 'анна')).toHaveLength(1);
        expect(filterTranscript(lines, ' ')).toHaveLength(2);
    });
});

describe('csrfFromCookie', () => {
    it('finds the token among cookies', () => {
        expect(csrfFromCookie('a=1; MMCSRF=tok; b=2')).toBe('tok');
        expect(csrfFromCookie('a=1')).toBe('');
    });
});

describe('isWebUrl', () => {
    it('allows only web addresses', () => {
        expect(isWebUrl('https://x.ktalk.ru/abc')).toBe(true);
        expect(isWebUrl('http://x.ru')).toBe(true);
        expect(isWebUrl('javascript:alert(1)')).toBe(false);
        expect(isWebUrl('JaVaScRiPt:alert(1)')).toBe(false);
        expect(isWebUrl('data:text/html,x')).toBe(false);
        expect(isWebUrl('')).toBe(false);
        expect(isWebUrl(undefined)).toBe(false);
    });
});

describe('effectiveStyle', () => {
    it('prefers the user, then the administrator, then the default', () => {
        expect(effectiveStyle('cards', 'compact')).toBe('cards');
        expect(effectiveStyle('default', 'compact')).toBe('compact');
        expect(effectiveStyle('live', 'cards')).toBe('cards');
        expect(effectiveStyle(undefined, 'compact')).toBe('compact');
        expect(effectiveStyle('nonsense', 'nonsense')).toBe('current');
        expect(effectiveStyle(undefined, undefined)).toBe('current');
    });
});

describe('initials', () => {
    it('takes first and last words', () => {
        expect(initials('Илья Петрович Буланов')).toBe('ИБ');
        expect(initials('гость')).toBe('Г');
        expect(initials('  ')).toBe('?');
    });
});

describe('colorIndex', () => {
    it('is stable and in range', () => {
        expect(colorIndex('Анна', 6)).toBe(colorIndex('Анна', 6));
        for (const name of ['a', 'Борис', 'Гость плагина']) {
            expect(colorIndex(name, 6)).toBeGreaterThanOrEqual(0);
            expect(colorIndex(name, 6)).toBeLessThan(6);
        }
    });
});

describe('chipKind', () => {
    it('groups statuses', () => {
        expect(chipKind('accepted')).toBe('ok');
        expect(chipKind('rejected')).toBe('no');
        expect(chipKind('calling')).toBe('wait');
        expect(chipKind('not_in_talk')).toBe('');
    });
});

describe('isTrustedCard', () => {
    it('accepts only the bot', () => {
        expect(isTrustedCard('bot', 'bot')).toBe(true);
        expect(isTrustedCard('someone', 'bot')).toBe(false);
        expect(isTrustedCard('bot', undefined)).toBe(false);
        expect(isTrustedCard(undefined, undefined)).toBe(false);
    });
});

describe('cleanRecentStatuses', () => {
    it('drops only the plugin status', () => {
        const ours = {emoji: 'video_camera', text: 'На встрече в Контур.Толке', duration: 'dont_clear'};
        const mine = {emoji: 'palm_tree', text: 'В отпуске', duration: 'today'};
        expect(cleanRecentStatuses(JSON.stringify([ours, mine]))).toBe(JSON.stringify([mine]));
        expect(cleanRecentStatuses(JSON.stringify([mine]))).toBeNull();
        expect(cleanRecentStatuses(JSON.stringify([{...ours, emoji: 'konturtalk', duration: ''}, mine]))).toBe(JSON.stringify([mine]));
        expect(cleanRecentStatuses('not json')).toBeNull();
        expect(cleanRecentStatuses(undefined)).toBeNull();
    });
});

describe('incomingAnchor', () => {
    const center = {top: 85, left: 269, right: 1355, bottom: 895};
    const header = {top: 85, left: 269, right: 1355, bottom: 141};

    it('sits under the channel header inside the center column', () => {
        expect(incomingAnchor(1400, center, header)).toEqual({top: 157, right: 61, width: 320});
    });

    it('follows the column when the right panel takes space', () => {
        const narrowed = {...center, right: 955};
        expect(incomingAnchor(1400, narrowed, header)).toEqual({top: 157, right: 461, width: 320});
    });

    it('shrinks to the column and gives up when it is too narrow', () => {
        expect(incomingAnchor(1000, {...center, right: 569}, header)?.width).toBe(268);
        expect(incomingAnchor(600, {...center, right: 500}, header)).toBeNull();
        expect(incomingAnchor(1400, null, header)).toBeNull();
    });

    it('stays clear of a right panel that floats over the column', () => {
        const rhs = {top: 85, left: 655, right: 1055, bottom: 795};
        expect(incomingAnchor(1100, {...center, right: 751}, header, rhs)).toEqual({top: 157, right: 461, width: 320});
    });

    it('uses the column top when there is no channel header', () => {
        expect(incomingAnchor(1400, center, null)?.top).toBe(101);
    });
});

describe('repeatText', () => {
    it('names known repeats and falls back for the rest', () => {
        expect(repeatText('weekly')).toBe('Еженедельно');
        expect(repeatText(undefined)).toBe('Повторяется');
        expect(repeatText('yearly')).toBe('Повторяется');
    });
});

describe('speakerColors', () => {
    it('gives speakers distinct colours in order of appearance', () => {
        const colors = speakerColors(['Иван', 'Мария', 'Иван', 'Вера']);
        expect(colors.get('Иван')).toBe(PERSON_COLORS[0]);
        expect(colors.get('Мария')).toBe(PERSON_COLORS[1]);
        expect(colors.get('Вера')).toBe(PERSON_COLORS[2]);
    });
});
