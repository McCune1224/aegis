import uPlot, { type Options } from "uplot";
import { createEffect, createSignal, For, Show } from "solid-js";
import type { QueryEntry } from "./api";
import "uplot/dist/uPlot.min.css";
import { aggregate } from "./stats";

type Props = {
  entries: QueryEntry[];
  onOpenLog: () => void;
};

// Canvas text cannot read CSS custom properties, so the chart mirrors the
// constellation tokens from app.css by value.
const ink = "#8b96b5";
const grid = "rgba(125, 211, 252, 0.08)";
const accent = "#7dd3fc";
const block = "#fb7185";

export default function Dashboard(props: Props) {
  const stats = () => aggregate(props.entries, Date.now());
  const rate = () => `${(stats().blockRate * 100).toFixed(1)}%`;

  return (
    <div class="screen-inner wide">
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
          <h2>Queries over the last hour</h2>
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
                  <span class="name">{row.name}</span>
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
                  <span class="name">{row.client}</span>
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
      height: 190,
      cursor: { show: false },
      legend: { show: true },
      scales: { x: { time: false } },
      axes: [
        {
          stroke: ink,
          grid: { stroke: grid },
          ticks: { stroke: grid },
          values: (_plot, values) =>
            values.map((value) => new Date(Number(value) * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })),
        },
        { stroke: ink, grid: { stroke: grid }, ticks: { stroke: grid } },
      ],
      series: [
        {},
        {
          label: "total",
          stroke: accent,
          fill: "rgba(125, 211, 252, 0.14)",
          width: 1.6,
          points: { show: false },
        },
        {
          label: "blocked",
          stroke: block,
          width: 1.6,
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
    () => props.series.map((bucket) => `${bucket.total}:${bucket.blocked}`).join(","),
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
            plot.setSize({ width: next, height: 190 });
          }
        });
        observer.observe(element);
      }}
    />
  );
}
