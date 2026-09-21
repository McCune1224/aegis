import uPlot, { type Options } from "uplot";
import { createEffect, createSignal, For, Show } from "solid-js";
import type { Discovery, QueryEntry, ThreatFinding } from "./api";
import Discoveries from "./Discoveries";
import "uplot/dist/uPlot.min.css";
import { aggregate } from "./stats";

type Props = {
  entries: QueryEntry[];
  discoveries: Discovery[];
  threats: ThreatFinding[];
  onClaimDiscovery: (discovery: Discovery, name: string) => Promise<void>;
  onDismissDiscovery: (mac: string) => Promise<void>;
  windowMinutes: number;
  onSetWindow: (minutes: number) => void;
  onOpenLog: () => void;
  onFilter: (filter: { client?: string; name?: string }) => void;
};

const ink = "#6e7681";
const grid = "rgba(255, 255, 255, 0.04)";
const accent = "#58a6ff";
const block = "#f85149";
const axisFont = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif';

const CHART_HEIGHT = 200;

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
    <div class="screen-inner wide">
      <Discoveries
        discoveries={props.discoveries}
        onClaim={props.onClaimDiscovery}
        onDismiss={props.onDismissDiscovery}
      />
      <Show when={props.threats.length > 0}>
        <section class="panel" data-testid="threat-panel">
          <header>
            <h2>Threat activity</h2>
          </header>
          <ol class="top-list" data-testid="threat-list">
            <For each={props.threats}>
              {(finding) => (
                <li>
                  <div>
                    <strong>{finding.client}</strong>
                    <span class="muted">{finding.summary}</span>
                  </div>
                  <span class="badge block">{finding.kind}</span>
                </li>
              )}
            </For>
          </ol>
        </section>
      </Show>
      <div class="card-row">
        <div class="panel stat">
          <div class="value" data-testid="stat-total">{stats().total}</div>
          <div class="label">queries</div>
        </div>
        <div class="panel stat blocked">
          <div class="value" data-testid="stat-blocked">{stats().blocked}</div>
          <div class="label">blocked</div>
        </div>
        <div class="panel stat">
          <div class="value">{rate()}</div>
          <div class="label">block rate</div>
        </div>
        <div class="panel stat">
          <div class="value">{stats().clients}</div>
          <div class="label">clients seen</div>
        </div>
      </div>

      <section class="panel">
        <header>
          <h2>Queries, last {props.windowMinutes >= 1440 ? "24 hours" : "hour"}</h2>
          <div class="chart-meta">
            <div class="chart-legend">
              <span class="total">
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
        </header>
        <div class="chart" data-testid="chart">
          <SeriesChart series={stats().series} />
        </div>
      </section>

      <div class="two-col">
        <section class="panel">
          <header>
            <h2>Top blocked</h2>
          </header>
          <ol class="top-list" data-testid="top-blocked">
            <For each={stats().topBlocked}>
              {(row) => (
                <li>
                  <button
                    type="button"
                    class="link"
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
        </section>
        <section class="panel">
          <header>
            <h2>Top clients</h2>
          </header>
          <ol class="top-list" data-testid="top-clients">
            <For each={stats().topClients}>
              {(row) => (
                <li>
                  <button
                    type="button"
                    class="link"
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
        </section>
      </div>

      <div class="two-col">
        <section class="panel">
          <header>
            <h2>Query types</h2>
          </header>
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
        </section>
        <section class="panel">
          <header>
            <h2>Latest queries</h2>
            <button type="button" class="btn-ghost" onClick={props.onOpenLog}>
              Open the log
            </button>
          </header>
          <table>
            <tbody>
              <For each={props.entries.slice(0, 8)}>
                {(entry) => (
                  <tr>
                    <td class="name">{entry.name}</td>
                    <td class="muted">{entry.client}</td>
                    <td>
                      <span class={`badge ${entry.verdict === "block" ? "block" : "allow"}`}>{entry.verdict}</span>
                    </td>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
        </section>
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
          fill: "rgba(88, 166, 255, 0.06)",
          width: 2,
          points: { show: false },
        },
        {
          label: "blocked",
          stroke: block,
          fill: "rgba(248, 81, 73, 0.04)",
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
