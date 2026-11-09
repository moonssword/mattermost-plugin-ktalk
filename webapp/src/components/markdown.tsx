import React, {useMemo} from 'react';

interface Props {
    text: string;
    singleline?: boolean;
    className?: string;
}

// Renders with Mattermost's own message renderer, so text gets the same
// Markdown and sanitizing as chat messages. Mentions stay inert: this text can
// come from Talk or a meeting title. Falls back to plain text without the host.
const Markdown = ({text, singleline, className}: Props) => {
    const rendered = useMemo(() => {
        const utils = window.PostUtils;
        if (!utils || !text) {
            return null;
        }
        const html = utils.formatText(text, {atMentions: false, mentionHighlight: false, markdown: true, singleline: Boolean(singleline)});
        return utils.messageHtmlToComponent(html, {mentions: false, emoji: true, images: false});
    }, [text, singleline]);

    const classes = 'ktk-md' + (className ? ' ' + className : '');
    if (singleline) {
        return <span className={classes}>{rendered ?? text}</span>;
    }
    return <div className={classes}>{rendered ?? text}</div>;
};

export default Markdown;
