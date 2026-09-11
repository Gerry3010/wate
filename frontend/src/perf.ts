/**
 * Counters for `wate ctl debug`: where a frame's time goes. Everything here is a plain integer
 * add on the hot path; the timing calls only run once `perf.timing` is on (`ctl action __perf`).
 */
export const perf = {
  /** Time the wall clock, not just count (off by default: performance.now() is not free). */
  timing: false,
  /** Ligature joiner: how often the renderer asked, over how much text, for how long. */
  joinerCalls: 0,
  joinerChars: 0,
  joinerMs: 0,
  /** Full tab-bar rebuilds and the title changes that ask for one. */
  tabRenders: 0,
  titleFires: 0,
  /** Session snapshots and the serializing inside them. */
  saves: 0,
  serializeMs: 0,
  /** PTY bytes arriving, and how long the terminal took to show them. */
  writes: 0,
  bytes: 0,
  latency: [] as number[],
  since: Date.now(),
};

/** Record how long a chunk of output took from the socket to the next render. */
export function sample(ms: number) {
  if (perf.latency.length < 500) perf.latency.push(ms);
}

/** Percentile of the recorded latencies (0..1), rounded to a tenth of a millisecond. */
function pct(values: number[], p: number): number {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  return Math.round(sorted[Math.min(sorted.length - 1, Math.floor(p * sorted.length))] * 10) / 10;
}

/** Snapshot for the debug dump; resets the counters so each dump covers one interval. */
export function takePerf() {
  const p = perf;
  const out = {
    seconds: Math.round((Date.now() - p.since) / 100) / 10,
    timing: p.timing,
    joiner: { calls: p.joinerCalls, chars: p.joinerChars, ms: Math.round(p.joinerMs) },
    tabRenders: p.tabRenders,
    titleFires: p.titleFires,
    saves: p.saves,
    serializeMs: Math.round(p.serializeMs),
    output: { writes: p.writes, bytes: p.bytes },
    latencyMs: { p50: pct(p.latency, 0.5), p95: pct(p.latency, 0.95), max: pct(p.latency, 1) },
  };
  p.joinerCalls = p.joinerChars = p.joinerMs = 0;
  p.tabRenders = p.titleFires = p.saves = p.serializeMs = 0;
  p.writes = p.bytes = 0;
  p.latency = [];
  p.since = Date.now();
  return out;
}
