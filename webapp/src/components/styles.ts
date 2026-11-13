// Everything is scoped to .ktk-root and built from the theme variables of
// Mattermost, so light, dark and custom themes all work. Only what the plugin
// renders changes: the author's avatar and name around a post stay standard.
//
// Differences from the design mockups, on purpose:
//  - Online green is too light for text, so status text uses a mix with the
//    text color.
//  - No web fonts: text and monospace come from the Mattermost theme and system.
export const CSS = `
.ktk-root{
  --k-bg:var(--center-channel-bg);--k-fg:var(--center-channel-color);--k-fg-rgb:var(--center-channel-color-rgb);
  --k-btn:var(--button-bg);--k-btn-rgb:var(--button-bg-rgb);--k-btn-fg:var(--button-color);
  --k-online:var(--online-indicator);--k-online-rgb:var(--online-indicator-rgb);
  --k-away:var(--away-indicator);--k-away-rgb:var(--away-indicator-rgb);
  --k-err:var(--error-text);--k-err-rgb:var(--error-text-color-rgb);
  --k-link:var(--link-color);
  --k-online-text:color-mix(in srgb,var(--online-indicator) 58%,var(--center-channel-color));
  --k-mono:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  color:var(--k-fg);min-width:0;
}

/* ---- Base and style A, "Системный": neutral, with status readable at a glance ---- */
.ktk-card{position:relative;min-width:0;max-width:560px;margin:4px 0;padding:12px 16px;border:1px solid rgba(var(--k-fg-rgb),.16);border-radius:8px;background:var(--k-bg);box-shadow:0 1px 2px rgba(var(--k-fg-rgb),.05)}
.ktk-card[data-state="live"]{border-color:rgba(var(--k-online-rgb),.5)}
.ktk-card__head{display:flex;gap:12px;align-items:flex-start}
.ktk-card__icon{flex:none;width:36px;height:36px;border-radius:8px;display:flex;align-items:center;justify-content:center;background:rgba(var(--k-btn-rgb),.12);color:var(--k-btn)}
[data-state="live"] .ktk-card__icon{background:rgba(var(--k-online-rgb),.16);color:var(--k-online-text)}
[data-state="ended"] .ktk-card__icon{background:rgba(var(--k-fg-rgb),.08);color:rgba(var(--k-fg-rgb),.56)}
[data-state="ringing"] .ktk-card__icon{background:rgba(var(--k-away-rgb),.2);color:var(--k-fg)}
.ktk-card__body{min-width:0;flex:1}
.ktk-card__title{font-weight:600;font-size:14px;line-height:20px;overflow-wrap:anywhere}
.ktk-card__status{display:flex;flex-wrap:wrap;gap:4px 8px;align-items:center;margin-top:2px;font-size:12px;line-height:16px;color:rgba(var(--k-fg-rgb),.72)}
.ktk-status{display:inline-flex;align-items:center;gap:6px;font-weight:600}
[data-state="live"] .ktk-status{color:var(--k-online-text)}
.ktk-dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--k-online)}
.ktk-time{font-variant-numeric:tabular-nums}
.ktk-rec{display:inline-flex;align-items:center;gap:5px;font-weight:600;color:var(--k-err)}
.ktk-rec::before{content:"";width:8px;height:8px;border-radius:50%;background:currentColor;animation:ktk-rec-pulse 2s ease-in-out infinite}
@keyframes ktk-rec-pulse{0%,100%{opacity:1;box-shadow:0 0 0 0 rgba(var(--k-err-rgb),.45)}50%{opacity:.35;box-shadow:0 0 0 4px rgba(var(--k-err-rgb),0)}}
.ktk-caret{flex:none;margin-left:-2px;opacity:.7}
.ktk-callmenu__title{padding:4px 16px 6px;font-size:12px;font-weight:600;color:rgba(var(--k-fg-rgb),.64)}
.ktk-callmenu__radio{flex:none;width:14px;height:14px;margin-top:3px;border:1.5px solid rgba(var(--k-fg-rgb),.4);border-radius:50%}
.ktk-callmenu__item[aria-checked="true"] .ktk-callmenu__radio{border-color:var(--k-btn);box-shadow:inset 0 0 0 3px var(--k-bg),inset 0 0 0 8px var(--k-btn)}
/* The label column follows its content but never takes more than 40% of the
   card, so a long name or address wraps instead of squeezing the values out. */
.ktk-rows{display:grid;grid-template-columns:fit-content(40%) minmax(0,1fr);gap:3px 12px;margin:10px 0 0;font-size:12px;line-height:18px;color:rgba(var(--k-fg-rgb),.72)}
.ktk-row{display:contents}
.ktk-row dt{margin:0;min-width:0;overflow-wrap:anywhere;font-weight:400;color:rgba(var(--k-fg-rgb),.64)}
.ktk-row dd{margin:0;min-width:0;overflow-wrap:anywhere;color:var(--k-fg)}
.ktk-text.desc{grid-column:1/-1;font-size:13px;color:var(--k-fg)}
.ktk-chip{display:inline-block;padding:0 7px;border-radius:999px;font-size:11px;font-weight:600;line-height:18px;background:rgba(var(--k-fg-rgb),.08)}
.ktk-chip--ok{color:var(--k-online-text);background:rgba(var(--k-online-rgb),.14)}
.ktk-chip--no{color:var(--k-err);background:rgba(var(--k-err-rgb),.1)}
.ktk-chip--wait{background:rgba(var(--k-away-rgb),.22)}
.ktk-avatars{display:inline-flex;align-items:center;margin-right:6px;vertical-align:middle}
.ktk-av{display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;margin-left:-6px;border:2px solid var(--k-bg);border-radius:50%;font-size:9px;font-weight:700;line-height:1;color:#fff;box-sizing:content-box}
.ktk-av:first-child{margin-left:0}
.ktk-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:12px}
.ktk-btn{display:inline-flex;align-items:center;justify-content:center;gap:6px;height:32px;padding:0 12px;border:0;border-radius:4px;background:transparent;font-size:12px;font-weight:600;cursor:pointer;white-space:nowrap;text-decoration:none!important}
.ktk-btn:focus-visible,.ktk-link:focus-visible{outline:2px solid var(--k-btn);outline-offset:1px}
.ktk-tab:focus-visible{outline:2px solid var(--k-btn);outline-offset:-2px;border-radius:4px}
.ktk-btn--primary{background:var(--k-btn);color:var(--k-btn-fg)!important}
.ktk-btn--primary:hover{background:linear-gradient(0deg,rgba(0,0,0,.08),rgba(0,0,0,.08)),var(--k-btn)}
.ktk-btn--secondary{background:rgba(var(--k-btn-rgb),.08);color:var(--k-btn)!important}
.ktk-btn--secondary:hover{background:rgba(var(--k-btn-rgb),.16)}
.ktk-btn--danger{background:rgba(var(--k-err-rgb),.08);color:var(--k-err)!important}
.ktk-btn--danger:hover{background:rgba(var(--k-err-rgb),.16)}
.ktk-btn[disabled]{opacity:.5;cursor:default}
.ktk-md p{margin:0 0 4px}.ktk-md p:last-child{margin-bottom:0}
.ktk-md ul,.ktk-md ol,.ktk-md .markdown__list{margin:2px 0 4px;padding-left:20px}
.ktk-md ul:last-child,.ktk-md ol:last-child{margin-bottom:0}
.ktk-md li{margin:0}
.ktk-md h1,.ktk-md h2,.ktk-md h3,.ktk-md h4{margin:8px 0 4px;font-size:14px;font-weight:600;line-height:20px}
.ktk-card__title .ktk-md p{display:inline;margin:0}
.ktk-rhs{display:flex;flex-direction:column;height:100%;min-height:0;font-size:14px;container:ktk-rhs/inline-size}
.ktk-tabs{display:flex;gap:4px;padding:4px 12px 0;border-bottom:1px solid rgba(var(--k-fg-rgb),.12)}
.ktk-tab{display:inline-flex;align-items:center;gap:6px;padding:8px 10px;border:0;border-bottom:2px solid transparent;background:none;font-size:13px;font-weight:600;color:rgba(var(--k-fg-rgb),.64);cursor:pointer;white-space:nowrap}
.ktk-tab:hover{color:var(--k-fg)}
.ktk-tab--active,.ktk-tab--active:hover{color:var(--k-btn);border-bottom-color:var(--k-btn)}
.ktk-tab__count{min-width:18px;padding:0 5px;border-radius:999px;font-size:10.5px;font-weight:700;line-height:16px;text-align:center;background:rgba(var(--k-fg-rgb),.08);color:var(--k-fg)}
.ktk-rhs__actions{display:flex;flex-wrap:wrap;gap:8px;padding:12px}
.ktk-rhs__offer{padding:0 12px 8px}
.ktk-rhs__offer:empty{display:none}
.ktk-rhs__scroll{flex:1;overflow:auto;padding:8px 12px 16px;min-width:0;min-height:0}
.ktk-panel[hidden]{display:none}
.ktk-section-title{margin:8px 0 2px;font-size:11px;font-weight:700;letter-spacing:.05em;text-transform:uppercase;color:rgba(var(--k-fg-rgb),.56)}
/* A list row: text on the left, short actions on the right. */
.ktk-item{position:relative;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:2px 12px;align-items:center;padding:10px 0;border-bottom:1px solid rgba(var(--k-fg-rgb),.08);min-width:0}
.ktk-item__title{grid-column:1;font-weight:600;overflow-wrap:anywhere}
.ktk-item__meta{grid-column:1;display:flex;flex-wrap:wrap;gap:2px 6px;font-size:12px;color:rgba(var(--k-fg-rgb),.64)}
.ktk-item__meta>*+*::before{content:"· ";}
.ktk-item__actions{grid-column:2;grid-row:1/span 2;display:flex;gap:6px}
.ktk-item__actions .ktk-btn{height:28px;padding:0 10px}
.ktk-btn.ktk-btn--icon{aspect-ratio:1;padding:0!important}
.ktk-more{display:flex;margin-top:8px}
.ktk-empty{padding:24px 8px;text-align:center;color:rgba(var(--k-fg-rgb),.64)}
.ktk-empty--icon{display:flex;flex-direction:column;align-items:center;gap:10px;padding:40px 16px}
.ktk-empty--icon>svg{color:rgba(var(--k-fg-rgb),.4)}
.ktk-back{display:inline-flex;align-items:center;gap:2px;height:28px;margin-left:-6px;padding:0 10px 0 4px;border:0;border-radius:4px;background:none;color:rgba(var(--k-fg-rgb),.72);font-size:13px;font-weight:600;cursor:pointer;transition:background .12s ease,color .12s ease}
.ktk-back:hover{background:rgba(var(--k-fg-rgb),.08);color:var(--k-fg)}
.ktk-back:focus-visible{outline:2px solid var(--k-btn);outline-offset:1px}
.ktk-input{width:100%;height:32px;margin-bottom:8px;padding:0 10px;border:1px solid rgba(var(--k-fg-rgb),.24);border-radius:4px;background:var(--k-bg);color:var(--k-fg);font:inherit;font-size:13px;transition:border-color .12s ease,box-shadow .12s ease}
.ktk-input:hover{border-color:rgba(var(--k-fg-rgb),.48)}
.ktk-input:focus{border-color:var(--k-btn);box-shadow:inset 0 0 0 1px var(--k-btn);outline:none}
.ktk-input::placeholder{color:rgba(var(--k-fg-rgb),.56)}
.ktk-text{white-space:pre-wrap;overflow-wrap:anywhere;line-height:20px}
.ktk-md.ktk-text{white-space:normal}
.ktk-detail-title{padding:4px 0 8px;font-weight:600;overflow-wrap:anywhere}
.ktk-line{padding:6px 0}
/* Who speaks and when: the name leads, the time sits quietly on the right. */
.ktk-line__meta{display:flex;align-items:baseline;gap:8px;margin-bottom:2px;font-size:12px;color:rgba(var(--k-fg-rgb),.64)}
.ktk-line__who{display:inline-flex;align-items:center;gap:6px;min-width:0;font-size:13px;font-weight:600;color:var(--k-fg);overflow-wrap:anywhere}
.ktk-line__dot{flex:none;width:8px;height:8px;border-radius:50%}
.ktk-line__time{flex:none;margin-left:auto;font-size:11.5px;font-variant-numeric:tabular-nums;color:rgba(var(--k-fg-rgb),.56)}
.ktk-link{padding:0;border:0;background:none;color:var(--k-link);font-size:12px;cursor:pointer}
.ktk-error{padding:8px 0;color:var(--k-err)}
.ktk-notice{display:flex;gap:10px;align-items:flex-start;margin:4px 0 8px;padding:12px;border:1px solid rgba(var(--k-btn-rgb),.24);border-radius:8px;background:rgba(var(--k-btn-rgb),.06);font-size:13px;line-height:18px;color:var(--k-fg)}
.ktk-notice svg{flex:none;margin-top:1px;color:var(--k-btn)}
.ktk-key{align-items:flex-start}
.ktk-key[role="status"] svg{color:var(--k-online-text)}
.ktk-key>.ktk-link{flex:none;align-self:center}
.ktk-key__body{display:grid;flex:1;gap:8px;min-width:0}
.ktk-key__body>.ktk-link{justify-self:start}
.ktk-key__form{display:grid;gap:6px}
.ktk-sr{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0}
.ktk-period{min-width:0;margin:0 0 10px;padding:0;border:0}
.ktk-period__box{display:flex;align-items:center;gap:2px;height:32px;padding:0 4px 0 10px;border:1px solid rgba(var(--k-fg-rgb),.24);border-radius:4px;background:var(--k-bg);transition:border-color .12s ease,box-shadow .12s ease}
.ktk-period__box:hover{border-color:rgba(var(--k-fg-rgb),.48)}
.ktk-period__box:focus-within{border-color:var(--k-btn);box-shadow:inset 0 0 0 1px var(--k-btn)}
.ktk-period__box input{flex:1 1 0;min-width:0;height:100%;padding:0;border:0;background:transparent;color:var(--k-fg);font:inherit;font-size:13px;font-variant-numeric:tabular-nums;outline:none}
.ktk-period__box input::-webkit-calendar-picker-indicator{margin-left:2px;padding:3px;border-radius:4px;opacity:.45;cursor:pointer}
.ktk-period__box input::-webkit-calendar-picker-indicator:hover{opacity:.8;background-color:rgba(var(--k-fg-rgb),.08)}
.ktk-period__dash{flex:none;padding:0 6px;color:rgba(var(--k-fg-rgb),.48)}
.ktk-period__hint{margin-top:4px;font-size:11.5px;line-height:16px;color:rgba(var(--k-fg-rgb),.56)}
.ktk-key .ktk-input{margin:0}
.ktk-key__error{font-size:12px;line-height:16px;color:var(--k-err)}
.ktk-key__hint{font-size:12px;line-height:16px;color:rgba(var(--k-fg-rgb),.64)}
.ktk-key__actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:2px}
/* The current user's attendee row answers the invitation in place. */
.ktk-row--self dt{font-weight:600;color:var(--k-fg)}
.ktk-answer{display:inline-flex;vertical-align:middle;border:1px solid rgba(var(--k-fg-rgb),.16);border-radius:999px;overflow:hidden}
.ktk-answer__btn{padding:0 9px;border:0;background:none;font-size:11px;font-weight:600;line-height:18px;color:rgba(var(--k-fg-rgb),.72);cursor:pointer}
.ktk-answer__btn+.ktk-answer__btn{border-left:1px solid rgba(var(--k-fg-rgb),.16)}
.ktk-answer__btn:hover{background:rgba(var(--k-fg-rgb),.06)}
.ktk-answer__btn:focus-visible{outline:2px solid var(--k-btn);outline-offset:-2px}
.ktk-answer__btn[disabled]{opacity:.6;cursor:default}
.ktk-answer__btn--ok[aria-pressed="true"]{background:rgba(var(--k-online-rgb),.14);color:var(--k-online-text);cursor:default}
.ktk-answer__btn--no[aria-pressed="true"]{background:rgba(var(--k-err-rgb),.1);color:var(--k-err);cursor:default}
/* One quiet line about the personal key above the meeting list. */
.ktk-keyline-wrap{display:grid;gap:6px;margin:2px 0 6px}
.ktk-keyline-wrap .ktk-input{margin:0}
.ktk-keyline{display:flex;align-items:center;gap:6px;font-size:12px;line-height:16px;color:rgba(var(--k-fg-rgb),.64)}
.ktk-card__keyline{gap:4px;margin-top:10px;flex-wrap:wrap}
.ktk-keyline__text{flex:1;min-width:0}
.ktk-keyline__text b{font-weight:600;color:var(--k-fg)}
.ktk-keyline>.ktk-link{flex:none}
.ktk-keyline__close{flex:none;display:grid;place-items:center;width:20px;height:20px;padding:0;border:0;border-radius:4px;background:none;color:rgba(var(--k-fg-rgb),.56);cursor:pointer}
.ktk-keyline__close:hover{background:rgba(var(--k-fg-rgb),.08);color:var(--k-fg)}
.ktk-tip{position:relative;display:inline-flex;flex:none}
.ktk-tip__btn{display:grid;place-items:center;width:20px;height:20px;padding:0;border:0;border-radius:50%;background:none;color:rgba(var(--k-fg-rgb),.56);cursor:pointer}
.ktk-tip__btn:hover,.ktk-tip[data-open="true"] .ktk-tip__btn{color:var(--k-btn)}
.ktk-tip__btn:focus-visible{outline:2px solid var(--k-btn);outline-offset:0}
.ktk-tip__bubble{position:absolute;top:calc(100% + 6px);left:-4px;z-index:20;width:max-content;max-width:min(260px,calc(100vw - 48px));padding:8px 10px;border-radius:6px;background:var(--k-fg);color:var(--k-bg);font-size:12px;font-weight:400;line-height:16px;box-shadow:0 6px 18px rgba(0,0,0,.2);visibility:hidden;opacity:0;transition:opacity .12s ease}
.ktk-tip:hover .ktk-tip__bubble,.ktk-tip[data-open="true"] .ktk-tip__bubble,.ktk-tip__btn:focus-visible+.ktk-tip__bubble{visibility:visible;opacity:1}
[data-ktk-style="cards"] .ktk-notice{border-radius:14px}
[data-ktk-style="compact"] .ktk-notice{border-radius:4px;padding:8px 10px;font-size:12.5px}
.ktk-popover{padding:2px 0;font-size:12px;color:rgba(var(--center-channel-color-rgb),.72)}
/* Mattermost gives plugin actions no inset; this matches its own button row. */
.ktk-popover-action{display:flex;padding:0 16px 16px}
.ktk-popover-action .ktk-btn{flex:1}
.ktk-toast{position:fixed;left:50%;bottom:32px;transform:translateX(-50%);z-index:10000;max-width:480px;padding:10px 16px;border-radius:8px;background:var(--center-channel-color);color:var(--center-channel-bg);font-size:14px;box-shadow:0 8px 24px rgba(0,0,0,.24)}
.ktk-table{border-collapse:collapse;margin:8px 0;font-size:13px}
.ktk-table td{padding:3px 12px 3px 0}
.ktk-ok{color:var(--online-indicator)}
.ktk-missing{color:var(--error-text)}
.ktk-table td svg{display:block}
.ktk-table td:first-child{width:24px;padding-right:4px}

/* ---- Style B, "Карточки": the language of the Jira plugin's sidebar ---- */
[data-ktk-style="cards"]{--k-line:rgba(var(--k-fg-rgb),.12);--k-line-strong:rgba(var(--k-fg-rgb),.2);--k-surface:rgba(var(--k-fg-rgb),.035);--k-shadow:0 1px 2px rgba(var(--k-fg-rgb),.06),0 6px 16px rgba(var(--k-fg-rgb),.05);--k-shadow-hover:0 2px 4px rgba(var(--k-btn-rgb),.1),0 10px 24px rgba(var(--k-btn-rgb),.12)}
[data-ktk-style="cards"] .ktk-card{overflow:hidden;padding:14px 16px 14px 18px;border-color:var(--k-line);border-radius:14px;box-shadow:var(--k-shadow);transition:transform .15s ease,border-color .15s ease,box-shadow .15s ease}
[data-ktk-style="cards"] .ktk-card::before,[data-ktk-style="cards"] .ktk-item::before{content:"";position:absolute;top:0;bottom:0;left:0;width:3px;background:rgba(var(--k-fg-rgb),.2)}
[data-ktk-style="cards"] [data-state="live"]::before{background:var(--k-online)}
[data-ktk-style="cards"] [data-state="scheduled"]::before{background:var(--k-btn)}
[data-ktk-style="cards"] [data-state="ringing"]::before{background:var(--k-away)}
[data-ktk-style="cards"] .ktk-card:hover,[data-ktk-style="cards"] .ktk-item:hover{transform:translateY(-1px);border-color:rgba(var(--k-btn-rgb),.35);box-shadow:var(--k-shadow-hover)}
[data-ktk-style="cards"] .ktk-card__icon{width:40px;height:40px;border-radius:10px}
[data-ktk-style="cards"] .ktk-card__title{font-size:15px;font-weight:700;line-height:1.35}
[data-ktk-style="cards"] .ktk-card__status{margin-top:6px;gap:6px}
[data-ktk-style="cards"] .ktk-status{padding:2px 8px 2px 7px;border-radius:999px;background:rgba(var(--k-fg-rgb),.08);color:var(--k-fg)}
[data-ktk-style="cards"] [data-state="live"] .ktk-status{background:rgba(var(--k-online-rgb),.14);color:var(--k-online-text)}
[data-ktk-style="cards"] [data-state="scheduled"] .ktk-status{background:rgba(var(--k-btn-rgb),.12);color:var(--k-btn)}
[data-ktk-style="cards"] [data-state="ringing"] .ktk-status{background:rgba(var(--k-away-rgb),.2)}
[data-ktk-style="cards"] .ktk-rec{padding:2px 8px;border-radius:999px;background:rgba(var(--k-err-rgb),.1)}
[data-ktk-style="cards"] .ktk-rows{margin-top:12px;padding:10px 12px;border-radius:10px;background:var(--k-surface);gap:5px 12px;font-size:12.5px}
[data-ktk-style="cards"] .ktk-row dd{font-weight:500}
[data-ktk-style="cards"] .ktk-btn{height:34px;padding:0 14px;border-radius:10px;font-size:13px;transition:background .15s ease,box-shadow .15s ease,filter .15s ease}
[data-ktk-style="cards"] .ktk-btn--primary:not([disabled]){box-shadow:0 2px 8px rgba(var(--k-btn-rgb),.28)}
[data-ktk-style="cards"] .ktk-btn--primary:hover:not([disabled]){filter:brightness(1.08);background:var(--k-btn)}
[data-ktk-style="cards"] .ktk-btn--secondary{background:rgba(var(--k-btn-rgb),.1)}
[data-ktk-style="cards"] .ktk-rhs{background:radial-gradient(120% 200px at 0 0,rgba(var(--k-btn-rgb),.09),transparent 70%),radial-gradient(90% 160px at 100% 0,rgba(var(--k-online-rgb),.08),transparent 70%),var(--k-bg)}
[data-ktk-style="cards"] .ktk-rhs__actions{padding:12px 12px 8px}
[data-ktk-style="cards"] .ktk-tabs{margin:2px 12px 8px;padding:4px;border:1px solid var(--k-line);border-radius:14px;background:var(--k-surface)}
[data-ktk-style="cards"] .ktk-tab{flex:1;justify-content:center;height:30px;padding:0 6px;border:0;border-radius:10px;font-size:12.5px;font-weight:700}
[data-ktk-style="cards"] .ktk-tab--active,[data-ktk-style="cards"] .ktk-tab--active:hover{color:var(--k-btn);background:var(--k-bg);box-shadow:0 1px 3px rgba(var(--k-fg-rgb),.14)}
[data-ktk-style="cards"] .ktk-tab--active .ktk-tab__count{color:var(--k-btn);background:rgba(var(--k-btn-rgb),.12)}
[data-ktk-style="cards"] .ktk-rhs__scroll{padding:0 12px 12px}
[data-ktk-style="cards"] .ktk-item{overflow:hidden;margin-bottom:8px;padding:10px 12px 11px 14px;border:1px solid var(--k-line);border-radius:14px;background:var(--k-bg);box-shadow:var(--k-shadow);transition:transform .15s ease,border-color .15s ease,box-shadow .15s ease}
[data-ktk-style="cards"] .ktk-item__title{font-size:13.5px;line-height:1.35}
[data-ktk-style="cards"] .ktk-item__meta{margin-top:4px}
[data-ktk-style="cards"] .ktk-item__actions .ktk-btn{height:30px;padding:0 12px;font-size:12.5px}
[data-ktk-style="cards"] .ktk-input{height:34px;border-color:var(--k-line-strong);border-radius:10px}
[data-ktk-style="cards"] .ktk-period__box{height:34px;border-color:var(--k-line-strong);border-radius:10px}
[data-ktk-style="cards"] .ktk-empty{margin:0 0 12px;padding:28px 16px;border:1px dashed var(--k-line-strong);border-radius:14px}
[data-ktk-style="cards"] .ktk-section-title{margin:8px 2px;color:var(--k-btn)}
[data-ktk-style="cards"] .ktk-line{margin-bottom:6px;padding:8px 12px;border:1px solid var(--k-line);border-radius:12px;background:var(--k-bg)}
[data-ktk-style="cards"] .ktk-detail-title{padding:4px 2px 10px;font-size:16px;font-weight:700;line-height:1.3}
[data-ktk-style="cards"] .ktk-text.body{padding:12px 14px;border:1px solid var(--k-line);border-radius:14px;background:var(--k-bg);box-shadow:var(--k-shadow)}

/* ---- Style C, "Компактный": dense rows on a strict grid ---- */
[data-ktk-style="compact"] .ktk-card{max-width:520px;padding:10px 12px 10px 14px;border-left:3px solid rgba(var(--k-fg-rgb),.24);border-radius:4px;box-shadow:none}
[data-ktk-style="compact"] .ktk-card[data-state="live"]{border-color:rgba(var(--k-fg-rgb),.16);border-left-color:var(--k-online)}
[data-ktk-style="compact"] .ktk-card[data-state="scheduled"]{border-left-color:var(--k-btn)}
[data-ktk-style="compact"] .ktk-card[data-state="ringing"]{border-left-color:var(--k-away)}
[data-ktk-style="compact"] .ktk-card__head{gap:10px;align-items:center}
[data-ktk-style="compact"] .ktk-card__icon{width:28px;height:28px;border-radius:4px}
[data-ktk-style="compact"] .ktk-card__icon svg{width:16px;height:16px}
[data-ktk-style="compact"] .ktk-card__title{font-size:13.5px}
[data-ktk-style="compact"] .ktk-card__status{margin-top:1px;gap:4px 10px;font-size:11.5px}
[data-ktk-style="compact"] .ktk-status,[data-ktk-style="compact"] .ktk-rec{font-size:10.5px;font-weight:700;letter-spacing:.04em;text-transform:uppercase}
[data-ktk-style="compact"] [data-state="scheduled"] .ktk-status{color:var(--k-btn)}
[data-ktk-style="compact"] [data-state="ended"] .ktk-status{color:rgba(var(--k-fg-rgb),.64)}
[data-ktk-style="compact"] .ktk-dot,[data-ktk-style="compact"] .ktk-rec::before{width:6px;height:6px}
[data-ktk-style="compact"] .ktk-time{font-family:var(--k-mono);font-size:11.5px}
[data-ktk-style="compact"] .ktk-rows{margin-top:8px;padding-top:8px;border-top:1px solid rgba(var(--k-fg-rgb),.08);grid-template-columns:fit-content(36%) minmax(0,1fr);gap:2px 10px;line-height:17px}
[data-ktk-style="compact"] .ktk-row dt{padding-top:1px;font-size:11px;letter-spacing:.04em;text-transform:uppercase;color:rgba(var(--k-fg-rgb),.56)}
[data-ktk-style="compact"] .ktk-text.desc{margin:0 0 2px;font-size:12px;line-height:17px}
[data-ktk-style="compact"] .ktk-chip{padding:0;background:none;font-size:12px}
[data-ktk-style="compact"] .ktk-chip--wait{color:rgba(var(--k-fg-rgb),.64)}
[data-ktk-style="compact"] .ktk-avatars{display:none}
[data-ktk-style="compact"] .ktk-actions{gap:6px;margin-top:10px}
[data-ktk-style="compact"] .ktk-btn{height:28px;padding:0 10px}
[data-ktk-style="compact"] .ktk-btn--secondary{border:1px solid rgba(var(--k-fg-rgb),.24);background:transparent;color:var(--k-fg)!important}
[data-ktk-style="compact"] .ktk-btn--secondary:hover{background:rgba(var(--k-fg-rgb),.06)}
[data-ktk-style="compact"] .ktk-btn--danger{border:1px solid rgba(var(--k-err-rgb),.4);background:transparent}
[data-ktk-style="compact"] .ktk-btn--danger:hover{background:rgba(var(--k-err-rgb),.08)}
[data-ktk-style="compact"] .ktk-rhs__actions{gap:6px;padding:10px 12px 8px}
[data-ktk-style="compact"] .ktk-tabs{gap:0}
[data-ktk-style="compact"] .ktk-tab{padding:8px 10px 7px;font-size:12.5px}
[data-ktk-style="compact"] .ktk-tab__count{min-width:0;padding:0;background:none;color:rgba(var(--k-fg-rgb),.56);font-size:12px;font-weight:600}
[data-ktk-style="compact"] .ktk-rhs__scroll{padding:8px 12px 12px}
[data-ktk-style="compact"] .ktk-item{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:2px 10px;align-items:center;padding:8px 0}
[data-ktk-style="compact"] .ktk-item__title{grid-column:1;font-size:13px}
[data-ktk-style="compact"] .ktk-item__meta{grid-column:1;margin:0;font-size:11.5px}
[data-ktk-style="compact"] .ktk-item__meta .ktk-time{font-family:inherit;font-size:inherit}
[data-ktk-style="compact"] .ktk-item__actions{grid-column:2;grid-row:1/span 2;gap:4px;margin:0}
[data-ktk-style="compact"] .ktk-item__actions .ktk-btn{height:26px;padding:0 8px;font-size:11.5px}
[data-ktk-style="compact"] .ktk-item__actions .ktk-btn:not(.ktk-btn--icon) svg{display:none}
[data-ktk-style="compact"] .ktk-answer{border-radius:3px}
[data-ktk-style="compact"] .ktk-input{height:30px;font-size:13px}
[data-ktk-style="compact"] .ktk-period__box{height:30px}
[data-ktk-style="compact"] .ktk-period__box input{font-size:12.5px}
[data-ktk-style="compact"] .ktk-section-title{margin:10px 0 2px}
/* One column: a side column for names would take a third of a narrow panel. */
[data-ktk-style="compact"] .ktk-line{padding:5px 0;border-bottom:1px solid rgba(var(--k-fg-rgb),.06)}
[data-ktk-style="compact"] .ktk-line__meta{margin:0;font-size:11px;line-height:16px}
[data-ktk-style="compact"] .ktk-line__who{gap:5px;font-size:11.5px}
[data-ktk-style="compact"] .ktk-line__dot{width:6px;height:6px}
[data-ktk-style="compact"] .ktk-line__time{font-family:var(--k-mono);font-size:11px}
[data-ktk-style="compact"] .ktk-detail-title{padding:4px 0 8px;font-size:14px}
[data-ktk-style="compact"] .ktk-text.body{font-size:13px}

/* ---- Incoming call: a floating card in the language of the "ringing" card ---- */
/* The component places the stack under the channel header, inside the center
   column. These values are the fallback for views without that column. */
.ktk-callmenu{position:fixed;z-index:1100;max-width:calc(100vw - 16px);padding:8px 0;border:1px solid rgba(var(--k-fg-rgb),.16);border-radius:8px;background:var(--k-bg);box-shadow:0 8px 24px rgba(0,0,0,.12)}
.ktk-callmenu__item{display:flex;align-items:flex-start;gap:10px;width:calc(100% - 8px);margin:0 4px;padding:8px 12px;border-radius:6px;border:0;background:none;color:var(--k-fg);text-align:left;font:inherit;cursor:pointer}
.ktk-callmenu__item>svg{flex:none;margin-top:1px;color:rgba(var(--k-fg-rgb),.56)}
.ktk-callmenu__item:hover>svg,.ktk-callmenu__item:focus-visible>svg{color:var(--k-btn)}
.ktk-callmenu__item:hover,.ktk-callmenu__item:focus-visible{background:rgba(var(--k-fg-rgb),.08);outline:none}
.ktk-callmenu__item:focus-visible{box-shadow:inset 0 0 0 2px rgba(var(--k-btn-rgb),.5)}
.ktk-callmenu__label{display:block;font-size:14px;line-height:20px}
.ktk-callmenu__hint{display:block;font-size:12px;line-height:16px;color:rgba(var(--k-fg-rgb),.64)}
.ktk-incoming-stack{position:fixed;top:112px;right:16px;z-index:1100;display:flex;flex-direction:column;gap:8px;width:320px;max-width:calc(100vw - 32px)}
.ktk-incoming{position:relative;overflow:hidden;padding:12px 16px 14px;border:1px solid rgba(var(--k-fg-rgb),.16);border-radius:8px;background:var(--k-bg);box-shadow:0 8px 24px rgba(0,0,0,.16)}
.ktk-incoming__head{display:flex;gap:12px;align-items:center}
.ktk-incoming__avatar{position:relative;flex:none;width:36px;height:36px;border-radius:50%}
.ktk-incoming__avatar img{display:block;width:36px;height:36px;border-radius:50%}
.ktk-incoming__avatar--ringing::after{content:"";position:absolute;inset:-4px;border:2px solid var(--k-away);border-radius:50%;animation:ktk-ring 1.6s ease-out infinite}
@keyframes ktk-ring{0%{transform:scale(.92);opacity:.9}100%{transform:scale(1.22);opacity:0}}
.ktk-incoming__name{font-weight:600;font-size:14px;line-height:20px;overflow-wrap:anywhere}
.ktk-incoming .ktk-status{color:var(--k-fg)}
.ktk-incoming .ktk-status svg{color:var(--k-fg)}
.ktk-incoming__title{margin-top:8px;font-size:12px;line-height:16px;color:rgba(var(--k-fg-rgb),.72);overflow-wrap:anywhere}
.ktk-incoming .ktk-actions{flex-wrap:nowrap;margin-top:12px}
.ktk-incoming__btn{flex:1}

[data-ktk-style="cards"] .ktk-incoming{padding:14px 16px 14px 18px;border-color:var(--k-line);border-radius:14px;box-shadow:0 2px 4px rgba(var(--k-fg-rgb),.08),0 12px 32px rgba(var(--k-fg-rgb),.16)}
[data-ktk-style="cards"] .ktk-incoming::before{content:"";position:absolute;top:0;bottom:0;left:0;width:3px;background:var(--k-away)}
[data-ktk-style="cards"] .ktk-incoming__avatar,[data-ktk-style="cards"] .ktk-incoming__avatar img{width:40px;height:40px}
[data-ktk-style="cards"] .ktk-incoming__name{font-size:15px;font-weight:700;line-height:1.35}
[data-ktk-style="cards"] .ktk-incoming .ktk-card__status{margin-top:4px}
[data-ktk-style="cards"] .ktk-incoming .ktk-status{padding:2px 8px 2px 7px;border-radius:999px;background:rgba(var(--k-away-rgb),.2)}

[data-ktk-style="compact"] .ktk-incoming{padding:10px 12px 12px 14px;border-left:3px solid var(--k-away);border-radius:4px;box-shadow:0 6px 18px rgba(0,0,0,.16)}
[data-ktk-style="compact"] .ktk-incoming__head{gap:10px}
[data-ktk-style="compact"] .ktk-incoming__avatar,[data-ktk-style="compact"] .ktk-incoming__avatar img{width:28px;height:28px}
[data-ktk-style="compact"] .ktk-incoming__avatar--ringing::after{inset:-3px}
[data-ktk-style="compact"] .ktk-incoming__name{font-size:13.5px}
[data-ktk-style="compact"] .ktk-incoming .ktk-status svg{display:none}
[data-ktk-style="compact"] .ktk-incoming .ktk-actions{gap:6px;margin-top:10px}

/* Mattermost 11 draws channel intro buttons from plugins twice in public and
   private channels: inside the "Add people" block and again in its own row.
   The copy in the block hides only while the other copy is on screen, because
   in the default channel the block holds the only one. */
.channel-intro__actions:has(> button .ktk-intro-mark) .MoreThanMaxFreeUsersWrapper button:has(.ktk-intro-mark),
.channel-intro__actions > button:has(.ktk-intro-mark):has(~ .LessThanMaxFreeUsers):has(~ button .ktk-intro-mark){display:none}

/* ---- Panel header: one row at any width ---- */
/* Narrow: the main action keeps its label, the others are square icons.
   From 440 px the actions carry short labels; the settings button stays an icon. Without container queries the
   narrow layout stays, with padded icon buttons. */
.ktk-rhs__head{flex-wrap:nowrap}
.ktk-head__main{flex:1;min-width:0}
.ktk-split{display:flex;gap:1px;min-width:0}
.ktk-split>.ktk-btn.ktk-split__main{flex:1;min-width:0}
.ktk-split>.ktk-btn.ktk-split__main:not(:last-child){border-top-right-radius:0;border-bottom-right-radius:0}
.ktk-split>.ktk-btn.ktk-split__more{flex:none;padding:0 9px;border-top-left-radius:0;border-bottom-left-radius:0}
.ktk-split__more .ktk-caret{margin:0;opacity:.9}
.ktk-head__main span{overflow:hidden;text-overflow:ellipsis}
.ktk-head__aux{flex:none}
.ktk-rhs .ktk-head__icon{aspect-ratio:1;padding:0;margin-left:auto}
/* ---- Settings page ---- */
.ktk-settings{display:flex;flex-direction:column;padding-top:4px}
.ktk-set{min-width:0;margin:0;padding:16px 0;border:0;border-bottom:1px solid rgba(var(--k-fg-rgb),.12)}
.ktk-set:first-child{padding-top:4px}
.ktk-set__title{margin:0 0 10px;padding:0;font-size:12px;font-weight:600;line-height:16px;letter-spacing:.04em;text-transform:uppercase;color:rgba(var(--k-fg-rgb),.56)}
.ktk-set__row{display:flex;align-items:center;justify-content:space-between;gap:12px;min-height:36px;padding:2px 0}
.ktk-set__label{flex:1;min-width:0;margin:0;font-size:14px;font-weight:400;line-height:20px;color:var(--k-fg)}
.ktk-select{flex:none;width:52%;max-width:220px;height:32px;padding:0 28px 0 10px;border:1px solid rgba(var(--k-fg-rgb),.24);border-radius:4px;color:var(--k-fg);font:inherit;font-size:13px;cursor:pointer;appearance:none;background:var(--k-bg) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%23888' stroke-width='2.2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E") no-repeat right 9px center;transition:border-color .12s ease,box-shadow .12s ease}
.ktk-select:hover{border-color:rgba(var(--k-fg-rgb),.48)}
.ktk-select:focus{border-color:var(--k-btn);box-shadow:inset 0 0 0 1px var(--k-btn);outline:none}
.ktk-switch{position:relative;flex:none;width:36px;height:20px;padding:0;border:0;border-radius:10px;background:rgba(var(--k-fg-rgb),.24);cursor:pointer;transition:background .15s ease}
.ktk-switch[aria-checked="true"]{background:var(--k-btn)}
.ktk-switch__knob{position:absolute;top:2px;left:2px;width:16px;height:16px;border-radius:50%;background:#fff;box-shadow:0 1px 2px rgba(0,0,0,.24);transition:transform .15s ease}
.ktk-switch[aria-checked="true"] .ktk-switch__knob{transform:translateX(16px)}
.ktk-switch:focus-visible{outline:2px solid var(--k-btn);outline-offset:2px}
.ktk-switch:disabled{opacity:.6;cursor:progress}
.ktk-set__foot{display:grid;gap:4px;padding-top:14px;font-size:12px;line-height:16px;color:rgba(var(--k-fg-rgb),.56)}
@media (prefers-reduced-motion:reduce){.ktk-switch,.ktk-switch__knob{transition:none}}
.ktk-set__lead{margin:0 0 12px;font-size:13px;line-height:18px;color:rgba(var(--k-fg-rgb),.72)}
.ktk-wide{display:none}
@container ktk-rhs (max-width:439.98px){
  .ktk-rhs .ktk-head__aux{aspect-ratio:1;padding:0}
}
@container ktk-rhs (min-width:440px){
  .ktk-head__main{flex:none}
  .ktk-wide{display:inline}
  .ktk-narrow{display:none}
}

/* ---- Loading: grey stand-ins instead of the word ---- */
.ktk-skel{display:grid;gap:8px;padding:4px 0;animation:ktk-skel-in .2s ease .15s both}
.ktk-skel__item{pointer-events:none}
.ktk-skel__bar{display:block;height:10px;border-radius:5px;background:rgba(var(--k-fg-rgb),.08);animation:ktk-skel-pulse 1.4s ease-in-out infinite}
.ktk-skel__title{grid-column:1;height:12px;margin:3px 0}
.ktk-skel__meta{grid-column:1;width:34%;height:9px;margin:2px 0}
.ktk-skel__button{width:64px;height:28px;border-radius:4px}
.ktk-skel__line{height:9px}
[data-ktk-style="cards"] .ktk-skel__button{border-radius:10px}
[data-ktk-style="compact"] .ktk-skel__button{width:52px;height:26px}
@keyframes ktk-skel-in{from{opacity:0}to{opacity:1}}
@keyframes ktk-skel-pulse{0%,100%{opacity:1}50%{opacity:.45}}

@media (prefers-reduced-motion:reduce){.ktk-rec::before{animation:none}
  .ktk-skel,.ktk-skel__bar{animation:none}
  .ktk-incoming__avatar--ringing::after{animation:none;opacity:.6}
  [data-ktk-style="cards"] .ktk-card,[data-ktk-style="cards"] .ktk-item{transition:none}
  [data-ktk-style="cards"] .ktk-card:hover,[data-ktk-style="cards"] .ktk-item:hover{transform:none}
}
`;

// An update reloads the plugin in tabs that stay open, and the element from
// the previous version is still there: its text is replaced, or parts added
// since, such as the call menu, would stay unstyled until a reload.
export function injectStyles() {
    const id = 'konturtalk-styles';
    let element = document.getElementById(id);
    if (!element) {
        element = document.createElement('style');
        element.id = id;
        document.head.appendChild(element);
    }
    if (element.textContent !== CSS) {
        element.textContent = CSS;
    }
}
