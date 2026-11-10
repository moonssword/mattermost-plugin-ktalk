import React, {useState} from 'react';
import {useSelector, useStore} from 'react-redux';

import {useT} from '../i18n';
import {PLUGIN_ID} from '../manifest';
import {isToggle, preferenceKey, savePreference, SETTING_GROUPS, settingSections, type SettingSection} from '../settings';

import {PersonalKeyPanel} from './personal_key';
import {useConfig} from './use_style';

/**
 * The user's settings inside the panel. They are the same preferences as in
 * Mattermost's settings window, so a choice made here shows up there too.
 * Each choice is saved at once; there is no Save button to forget. Choices
 * among several values are lists, yes-or-no ones are switches.
 */
export default function SettingsPanel() {
    const t = useT();
    const store = useStore();
    const config = useConfig();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const preferences = useSelector((state: any) => state.entities?.preferences?.myPreferences || {});
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const version: string = useSelector((state: any) => state.plugins?.plugins?.[PLUGIN_ID]?.version || '');
    const [error, setError] = useState('');
    const [saving, setSaving] = useState('');
    const sections = settingSections(config);

    const choose = async (name: string, value: string) => {
        setSaving(name);
        setError('');
        try {
            await savePreference(store, name, value);
        } catch {
            setError(t('Не удалось сохранить настройку. Попробуйте ещё раз.'));
        } finally {
            setSaving('');
        }
    };

    const row = (section: SettingSection) => {
        const current = preferences[preferenceKey(section.name)]?.value || section.default;
        const id = `ktk-set-${section.name}`;
        if (isToggle(section)) {
            const on = current === 'true';
            return (
                <div
                    key={section.name}
                    className='ktk-set__row'
                >
                    <span
                        id={id}
                        className='ktk-set__label'
                    >
                        {t(section.label)}
                    </span>
                    <button
                        type='button'
                        role='switch'
                        className='ktk-switch'
                        aria-checked={on}
                        aria-labelledby={id}
                        disabled={saving === section.name}
                        onClick={() => choose(section.name, on ? 'false' : 'true')}
                    >
                        <span className='ktk-switch__knob'/>
                    </button>
                </div>
            );
        }
        return (
            <div
                key={section.name}
                className='ktk-set__row'
            >
                <label
                    htmlFor={id}
                    className='ktk-set__label'
                >
                    {t(section.label)}
                </label>
                <select
                    id={id}
                    className='ktk-select'
                    value={current}
                    disabled={saving === section.name}
                    onChange={(e) => choose(section.name, e.target.value)}
                >
                    {section.options.map((item) => (
                        <option
                            key={item.value}
                            value={item.value}
                        >
                            {t(item.text)}
                        </option>
                    ))}
                </select>
            </div>
        );
    };

    return (
        <div className='ktk-settings'>
            <section className='ktk-set'>
                <h3 className='ktk-set__title'>{t('Личный ключ Толка')}</h3>
                <PersonalKeyPanel/>
            </section>
            {SETTING_GROUPS.map((group) => (
                <section
                    key={group.id}
                    className='ktk-set'
                >
                    <h3 className='ktk-set__title'>{t(group.title)}</h3>
                    {sections.filter((section) => section.group === group.id).map(row)}
                </section>
            ))}
            {error && (
                <div
                    className='ktk-key__error'
                    role='alert'
                >
                    {error}
                </div>
            )}
            <div className='ktk-set__foot'>
                {t('Эти же настройки есть в меню Mattermost: Настройки → Контур.Толк.')}
                {version && <div>{`${t('Версия плагина')} ${version}`}</div>}
            </div>
        </div>
    );
}
