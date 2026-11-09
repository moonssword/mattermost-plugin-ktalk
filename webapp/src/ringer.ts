// A ring made with Web Audio, so the plugin ships no sound files. The pattern
// is two short soft chimes, then a pause, repeated while a call rings.

let context: AudioContext | null = null;
let timer: ReturnType<typeof setInterval> | null = null;
let users = 0;

function chime(ctx: AudioContext, at: number, frequency: number) {
    const gain = ctx.createGain();
    gain.gain.setValueAtTime(0, at);
    gain.gain.linearRampToValueAtTime(0.12, at + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.6);
    gain.connect(ctx.destination);
    for (const [ratio, level] of [[1, 1], [2, 0.25]]) {
        const osc = ctx.createOscillator();
        const partial = ctx.createGain();
        osc.type = 'sine';
        osc.frequency.value = frequency * ratio;
        partial.gain.value = level;
        osc.connect(partial).connect(gain);
        osc.start(at);
        osc.stop(at + 0.65);
    }
}

function ringOnce() {
    if (!context) {
        return;
    }
    const now = context.currentTime + 0.05;
    chime(context, now, 880);
    chime(context, now + 0.35, 660);
}

/** Starts ringing. Calls nest: the sound stops when every caller has stopped. */
export function startRinging() {
    users++;
    if (timer) {
        return;
    }
    try {
        const Ctor = window.AudioContext || (window as unknown as {webkitAudioContext: typeof AudioContext}).webkitAudioContext;
        context = new Ctor();
        context.resume?.().catch(() => undefined);
    } catch {
        context = null;
        return;
    }
    ringOnce();
    timer = setInterval(ringOnce, 2500);
}

export function stopRinging() {
    users = Math.max(0, users - 1);
    if (users > 0) {
        return;
    }
    if (timer) {
        clearInterval(timer);
        timer = null;
    }
    context?.close().catch(() => undefined);
    context = null;
}
