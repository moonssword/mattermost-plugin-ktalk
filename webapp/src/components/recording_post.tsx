import React from 'react';
import {useStore} from 'react-redux';

import {showRecording} from '../actions';
import {useT} from '../i18n';
import {isTrustedCard, isWebUrl} from '../logic';
import {useRecordingAccess} from '../recording_access';

import {DocumentIcon, ExternalIcon} from './icons';
import Markdown from './markdown';
import {rhsActions} from './rhs_handle';
import {useConfig, useVisualStyle} from './use_style';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const RecordingPost = ({post}: {post: any}) => {
    const t = useT();
    const store = useStore();
    const config = useConfig();
    const style = useVisualStyle();
    const trusted = isTrustedCard(post.user_id, config?.bot_user_id);
    const key = trusted ? post.props?.recording_key as string | undefined : undefined;
    const link = post.props?.recording_url as string | undefined;
    // The summary, minutes and transcript open only for those Talk allows;
    // the link to Talk stays, and Talk decides there.
    const allowed = useRecordingAccess(key && config?.enabled ? [key] : []).has(key || '');

    return (
        <div
            className='ktk-root'
            data-ktk-style={style}
        >
            <Markdown text={post.message || ''}/>
            {key && (
                <div
                    className='ktk-actions'
                    style={{padding: 0}}
                >
                    {!post.props?.is_summary && link && isWebUrl(link) && (
                        <a
                            className='ktk-btn ktk-btn--primary'
                            href={link}
                            target='_blank'
                            rel='noopener noreferrer'
                        >
                            <ExternalIcon/>
                            {t('Открыть в Толке')}
                        </a>
                    )}
                    {allowed && post.props?.is_summary && (
                        <button
                            className='ktk-btn ktk-btn--primary'
                            onClick={() => showRecording(store, rhsActions.show, key, 'summary')}
                        >
                            <DocumentIcon/>
                            {t('Пересказ')}
                        </button>
                    )}
                    {allowed && (
                        <button
                            className='ktk-btn ktk-btn--secondary'
                            onClick={() => showRecording(store, rhsActions.show, key, 'protocol')}
                        >
                            <DocumentIcon/>
                            {t('Протокол')}
                        </button>
                    )}
                    {allowed && (
                        <button
                            className='ktk-btn ktk-btn--secondary'
                            onClick={() => showRecording(store, rhsActions.show, key, 'transcript')}
                        >
                            <DocumentIcon/>
                            {t('Транскрипт')}
                        </button>
                    )}
                </div>
            )}
        </div>
    );
};

export default RecordingPost;
