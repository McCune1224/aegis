import uPlot, { type Options } from "uplot";
import { createEffect, createSignal, For, Show } from "solid-js";
import type { QueryEntry, ThreatFinding } from "./api";
import "uplot/dist/uPlot.min.css";
import { aggregate } from "./stats";

type Props = {
  entries: QueryEntry[];
  threats: ThreatFinding[];
  windowMinutes: number;
  onSetWindow: (minutes: number) => void;
  onOpenLog: () => void;
  onFilter: (filter: { client?: string; name?: string }) => void;
};

const ink = "#7d8ab0";
const grid = "rgba(48, 51, 88, 0.6)";
const accent = "#7dd3fc";
const block = "#fb7185";
const axisFont = '11.5px system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif';

const CHART_HEIGHT = 220;

// clockTime is the wall clock a feed reader scans for.
function clockTime(iso: string): string {
  const parsed = new Date(iso);
  return Number.isNaN(parsed.getTime()) ? iso : parsed.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export default function Dashboard(props: Props) {
  const stats = () => aggregate(props.entries, Date.now(), props.windowMinutes);
  const rate = () => `${(stats().blockRate * 100).toFixed(1)}%`;

  const types = () => {
    const counts = new Map<string, number>();
    for (const entry of props.entries) {
      counts.set(entry.type, (counts.get(entry.type) ?? 0) + 1);
    }
    const rows = [...counts.entries()].map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count);
    const max = rows[0]?.count ?? 1;
    return rows.slice(0, 6).map((row) => ({ ...row, share: row.count / max }));
  };

  return (
    <div class="view">
      <div class="telemetry">
        <div class="tele">
          <span class="tele-value" data-testid="stat-total">{stats().total}</span>
          <span class="tele-label">queries</span>
        </div>
        <div class="tele blocked">
          <span class="tele-value" data-testid="stat-blocked">{stats().blocked}</span>
          <span class="tele-label">blocked</span>
        </div>
        <div class="tele">
          <span class="tele-value">{rate()}</span>
          <span class="tele-label">block rate</span>
        </div>
        <div class="tele">
          <span class="tele-value">{stats().clients}</span>
          <span class="tele-label">clients seen</span>
        </div>
      </div>

      <Show when={props.threats.length > 0}>
        <section data-testid="threat-panel" role="alert">
          <ol class="empty-list" data-testid="threat-list" style={{ "list-style": "none" }}>
            <For each={props.threats}>
              {(finding) => (
                <li class="alert-line">
                  <strong>{finding.client}</strong>
                  <span class="muted">{finding.summary}</span>
                  <span class="badge block">{finding.kind}</span>
                </li>
              )}
            </For>
          </ol>
        </section>
      </Show>

      <div class="overview-grid">
        <div class="overview-main">
          <section class="sheet">
            <div class="sheet-head">
              <h2>Queries, last {props.windowMinutes >= 1440 ? "24 hours" : "hour"}</h2>
              <div class="chart-meta">
                <div class="legend">
                  <span>
                    <i />total
                  </span>
                  <span class="blocked">
                    <i />blocked
                  </span>
                </div>
                <div class="seg">
                  <button
                    type="button"
                    class={props.windowMinutes < 1440 ? "seg-btn active" : "seg-btn"}
                    onClick={() => props.onSetWindow(60)}
                  >
                    1h
                  </button>
                  <button
                    type="button"
                    class={props.windowMinutes >= 1440 ? "seg-btn active" : "seg-btn"}
                    onClick={() => props.onSetWindow(1440)}
                  >
                    24h
                  </button>
                </div>
              </div>
            </div>
            <div class="chart-frame" data-testid="chart" style={{ padding: "10px 12px 4px" }}>
              <SeriesChart series={stats().series} />
            </div>
          </section>

          <section class="sheet">
            <div class="sheet-head">
              <h2>Query types</h2>
            </div>
            <div class="sheet-body">
              <div class="bars">
                <For each={types()}>
                  {(row) => (
                    <div class="bar-row">
                      <span class="bar-label">{row.name}</span>
                      <span class="bar-track">
                        <span class="bar-fill" style={{ width: `${Math.max(4, row.share * 100)}%` }} />
                      </span>
                      <span class="bar-count">{row.count}</span>
                    </div>
                  )}
                </For>
                <Show when={types().length === 0}>
                  <p class="empty">no queries yet</p>
                </Show>
              </div>
            </div>
          </section>
        </div>

        <aside class="overview-side">
          <section class="sheet">
            <div class="sheet-head">
              <h2>Latest queries</h2>
              <button type="button" class="btn-mini" onClick={props.onOpenLog}>
                Open the log
              </button>
            </div>
            <div class="sheet-body" style={{ padding: "8px 12px" }}>
              <ul class="feed">
                <For each={props.entries.slice(0, 14)}>
                  {(entry) => (
                    <li>
                      <span class="f-time">{clockTime(entry.time)}</span>
                      <span class="f-name">{entry.name}</span>
                      <span class="f-client">{entry.client}</span>
                      <span class={`badge ${entry.verdict === "block" ? "block" : entry.verdict === "rewrite" ? "rewrite" : "allow"}`}>
                        {entry.verdict}
                      </span>
                    </li>
                  )}
                </For>
                <Show when={props.entries.length === 0}>
                  <li class="empty">no queries yet</li>
                </Show>
              </ul>
            </div>
          </section>

          <section class="sheet">
            <div class="sheet-head">
              <h2>Top blocked</h2>
            </div>
            <div class="sheet-body" style={{ padding: "6px 12px" }}>
              <ol class="rank-list" data-testid="top-blocked">
                <For each={stats().topBlocked}>
                  {(row) => (
                    <li>
                      <button
                        type="button"
                        class="link rank-name"
                        title="show this name in the query log"
                        onClick={() => props.onFilter({ name: row.name })}
                      >
                        {row.name}
                      </button>
                      <span class="badge block">{row.count}</span>
                    </li>
                  )}
                </For>
                <Show when={stats().topBlocked.length === 0}>
                  <li class="empty">nothing blocked yet</li>
                </Show>
              </ol>
            </div>
          </section>

          <section class="sheet">
            <div class="sheet-head">
              <h2>Top clients</h2>
            </div>
            <div class="sheet-body" style={{ padding: "6px 12px" }}>
              <ol class="rank-list" data-testid="top-clients">
                <For each={stats().topClients}>
                  {(row) => (
                    <li>
                      <button
                        type="button"
                        class="link rank-name"
                        title="show this client in the query log"
                        onClick={() => props.onFilter({ client: row.client })}
                      >
                        {row.client}
                      </button>
                      <span class="badge kind">{row.count}</span>
                    </li>
                  )}
                </For>
                <Show when={stats().topClients.length === 0}>
                  <li class="empty">no queries yet</li>
                </Show>
              </ol>
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}

function SeriesChart(props: { series: { t: number; total: number; blocked: number }[] }) {
  let host: HTMLDivElement | undefined;
  let plot: uPlot | undefined;
  const [width, setWidth] = createSignal(600);

  function data() {
    const times = Float64Array.from(props.series, (bucket) => bucket.t / 1000);
    const total = Float64Array.from(props.series, (bucket) => bucket.total);
    const blocked = Float64Array.from(props.series, (bucket) => bucket.blocked);
    return [times, total, blocked];
  }

  function options(): Options {
    return {
      width: width(),
      height: CHART_HEIGHT,
      legend: { show: false },
      cursor: { show: false },
      padding: [12, 12, 0, 0],
      scales: { x: { time: false } },
      axes: [
        {
          stroke: ink,
          font: axisFont,
          size: 30,
          grid: { stroke: grid },
          ticks: { stroke: grid },
          values: (_plot, values) =>
            values.map((value) => new Date(Number(value) * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })),
        },
        { stroke: ink, font: axisFont, size: 44, grid: { stroke: grid }, ticks: { stroke: grid } },
      ],
      series: [
        {},
        {
          label: "total",
          stroke: accent,
          fill: "rgba(125, 211, 252, 0.08)",
          width: 2,
          points: { show: false },
        },
        {
          label: "blocked",
          stroke: block,
          fill: "rgba(251, 113, 133, 0.05)",
          width: 2,
          points: { show: false },
        },
      ],
    };
  }

  createEffect(
    () => undefined,
    () => {
      if (!host) {
        return;
      }
      setWidth(host.clientWidth);
      plot = new uPlot(options(), data(), host);
    },
  );

  createEffect(
    () => [width(), props.series.length, props.series[0]?.t] as const,
    () => {
      plot?.setData(data());
    },
  );

  return (
    <div
      ref={(element) => {
        host = element as HTMLDivElement;
        const observer = new ResizeObserver(() => {
          if (host && plot) {
            const next = host.clientWidth;
            setWidth(next);
            plot.setSize({ width: next, height: CHART_HEIGHT });
          }
        });
        observer.observe(element);
      }}
    />
  );
}
