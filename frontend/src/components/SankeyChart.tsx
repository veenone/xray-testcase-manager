import { useEffect, useMemo, useRef, useState } from "react";
import type { Sankey } from "../api";

interface Props {
  data: Sankey;
  // filtered tells the empty state to read as "filter matched nothing" rather
  // than "no data synced yet" — the two look identical otherwise.
  filtered?: boolean;
  onClearFilter?: () => void;
}

const NODE_W = 14;
const MIN_NODE_H = 5; // floor so tiny executions stay visible at scale
const GAP = 7; // vertical gap between stacked nodes
const PAD_Y = 12;
const MIN_HEIGHT = 340;
const LABEL_PAD = 9;

// Run-status palette — mirrors the .link-* CSS so legend swatches and ribbons
// agree. Used for legend chips (inline) and to colour status labels.
const STATUS_COLORS: Record<string, string> = {
  pass: "#22c55e",
  passed: "#22c55e",
  fail: "#ef4444",
  failed: "#ef4444",
  todo: "#6366f1",
  executing: "#f59e0b",
  aborted: "#94a3b8",
  blocked: "#a855f7",
};
function statusColor(label: string): string {
  return STATUS_COLORS[label.toLowerCase()] ?? "#94a3b8";
}

interface Placed {
  id: string;
  label: string;
  layer: number;
  value: number;
  x: number;
  y: number;
  h: number;
}
interface PlacedLink {
  source: string;
  target: string;
  value: number;
  thick: number;
  sy: number;
  ty: number;
}

// SankeyChart hand-renders the Plan → Execution → run-status traceability flow.
// It fills the container width (measured), grows vertically with min-height
// nodes so even single-run executions stay visible, and scrolls when tall —
// so hundreds of executions remain legible. Hovering a node traces its flow.
export function SankeyChart({ data, filtered, onClearFilter }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [hoverId, setHoverId] = useState<string | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      setWidth(Math.floor(entries[0].contentRect.width));
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const layout = useMemo(() => buildLayout(data, width), [data, width]);
  const statuses = useMemo(
    () =>
      [...(data?.nodes ?? [])]
        .filter((n) => n.layer === 2)
        .sort((a, b) => b.value - a.value),
    [data],
  );

  const empty = !data || data.nodes.length === 0;

  return (
    <div className="sankey-wrap" ref={ref}>
      {empty ? (
        filtered ? (
          <p className="muted sankey-empty">
            No execution runs match the selected Test Plan / Execution. The
            chosen plan&apos;s tests may not be in any execution yet.{" "}
            {onClearFilter && (
              <button className="btn btn-ghost sankey-clear" onClick={onClearFilter}>
                Clear filter
              </button>
            )}
          </p>
        ) : (
          <p className="muted sankey-empty">
            No execution data to trace yet — sync test executions to populate the
            traceability flow.
          </p>
        )
      ) : !layout ? (
        <div className="sankey-loading muted">…</div>
      ) : (
        <>
          <div className="sankey-cols">
            <span
              className="sankey-col-head"
              style={{ left: 0, width: layout.planX - 4, textAlign: "right" }}
            >
              Test Plans
            </span>
            <span className="sankey-col-head" style={{ left: layout.execX }}>
              Test Executions
            </span>
            <span className="sankey-col-head" style={{ left: layout.statusX }}>
              Run Status
            </span>
          </div>

          <div className="sankey-scroll">
            <Diagram layout={layout} hoverId={hoverId} setHoverId={setHoverId} />
          </div>

          {statuses.length > 0 && (
            <div className="sankey-legend">
              {statuses.map((s) => (
                <span key={s.id}>
                  <i
                    className="sankey-swatch"
                    style={{ background: statusColor(s.label) }}
                  />
                  {s.label}{" "}
                  <b className="sankey-legend-n">{s.value.toLocaleString()}</b>
                </span>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

function Diagram({
  layout,
  hoverId,
  setHoverId,
}: {
  layout: NonNullable<ReturnType<typeof buildLayout>>;
  hoverId: string | null;
  setHoverId: (id: string | null) => void;
}) {
  const { placed, links, width, height } = layout;
  const byId = useMemo(() => new Map(placed.map((p) => [p.id, p])), [placed]);

  const isLit = (l: PlacedLink) =>
    hoverId === null || l.source === hoverId || l.target === hoverId;

  return (
    <svg
      className="sankey"
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label="Traceability from test plans through executions to run status"
    >
      <g className={`sankey-links${hoverId ? " has-hover" : ""}`}>
        {links.map((lk, i) => {
          const s = byId.get(lk.source);
          const t = byId.get(lk.target);
          if (!s || !t) return null;
          const sx = s.x + NODE_W;
          const tx = t.x;
          const sy = s.y + lk.sy;
          const ty = t.y + lk.ty;
          const xm = (sx + tx) / 2;
          const d = `M${sx},${sy} C${xm},${sy} ${xm},${ty} ${tx},${ty} L${tx},${ty + lk.thick} C${xm},${ty + lk.thick} ${xm},${sy + lk.thick} ${sx},${sy + lk.thick} Z`;
          return (
            <path
              key={i}
              d={d}
              className={`sankey-link ${linkClass(t)}${isLit(lk) ? " lit" : " dim"}`}
            >
              <title>
                {s.label} → {t.label}: {lk.value}
              </title>
            </path>
          );
        })}
      </g>

      <g className="sankey-nodes">
        {placed.map((p) => (
          <g
            key={p.id}
            onMouseEnter={() => setHoverId(p.id)}
            onMouseLeave={() => setHoverId(null)}
            className={hoverId === p.id ? "node-hover" : ""}
          >
            <rect
              x={p.x}
              y={p.y}
              width={NODE_W}
              height={p.h}
              rx={3}
              className={`sankey-node sankey-node-l${p.layer}`}
              style={p.layer === 2 ? { fill: statusColor(p.label) } : undefined}
            >
              <title>
                {p.label}: {p.value}
              </title>
            </rect>
            {nodeLabel(p, layout)}
          </g>
        ))}
      </g>
    </svg>
  );
}

function buildLayout(data: Sankey, width: number) {
  if (!data || data.nodes.length === 0 || width <= 0) return null;
  const layers = [0, 1, 2];
  const byLayer = layers.map((L) => data.nodes.filter((n) => n.layer === L));
  if (byLayer.some((col) => col.length === 0)) return null;

  const layerTotal = Math.max(
    ...byLayer.map((col) => col.reduce((a, n) => a + n.value, 0)),
    1,
  );

  const gutterL = clamp(Math.round(width * 0.2), 130, 230);
  const gutterR = clamp(Math.round(width * 0.14), 108, 168);
  const planX = gutterL;
  const statusX = width - gutterR - NODE_W;
  const execX = Math.round(planX + (statusX - planX) * 0.44);
  const colX = [planX, execX, statusX];

  // Scale so the value-proportional layout fits the base height; node heights
  // are then floored to MIN_NODE_H and the chart grows (and scrolls) if that
  // pushes a column past the base.
  const busiest = Math.max(...byLayer.map((col) => col.length));
  const baseAvail = MIN_HEIGHT - PAD_Y * 2 - (busiest - 1) * GAP;
  const scale = Math.max(baseAvail, 1) / layerTotal;

  const colHeight = (col: typeof byLayer[number]) =>
    col.reduce((a, n) => a + Math.max(MIN_NODE_H, n.value * scale), 0) +
    Math.max(0, col.length - 1) * GAP;
  const contentH = Math.max(...byLayer.map(colHeight));
  const height = Math.max(MIN_HEIGHT, Math.ceil(contentH + PAD_Y * 2));

  const placed: Placed[] = [];
  const place = new Map<string, Placed>();
  byLayer.forEach((col, L) => {
    let y = PAD_Y + (height - PAD_Y * 2 - colHeight(col)) / 2; // vertically centred
    for (const n of col) {
      const h = Math.max(MIN_NODE_H, n.value * scale);
      const p: Placed = { ...n, x: colX[L], y, h };
      placed.push(p);
      place.set(n.id, p);
      y += h + GAP;
    }
  });

  const outCursor = new Map<string, number>();
  const inCursor = new Map<string, number>();
  const links: PlacedLink[] = [];
  for (const lk of data.links) {
    const s = place.get(lk.source);
    const t = place.get(lk.target);
    if (!s || !t) continue;
    const thick = Math.max(1, lk.value * scale);
    const sy = outCursor.get(lk.source) ?? 0;
    const ty = inCursor.get(lk.target) ?? 0;
    outCursor.set(lk.source, sy + thick);
    inCursor.set(lk.target, ty + thick);
    links.push({ ...lk, thick, sy, ty });
  }

  return { placed, links, width, height, planX, execX, statusX, gutterL, gutterR };
}

function nodeLabel(p: Placed, layout: NonNullable<ReturnType<typeof buildLayout>>) {
  const cy = p.y + p.h / 2;
  if (p.layer === 0) {
    // Plans: label to the left, right-anchored.
    if (p.h < 9) return null;
    return (
      <text
        x={p.x - LABEL_PAD}
        y={cy}
        className="sankey-label"
        textAnchor="end"
        dominantBaseline="middle"
      >
        {fit(p.label, layout.gutterL - LABEL_PAD)}
      </text>
    );
  }
  if (p.layer === 2) {
    // Run status: label + count to the right.
    return (
      <text
        x={p.x + NODE_W + LABEL_PAD}
        y={cy}
        className="sankey-label sankey-label-status"
        textAnchor="start"
        dominantBaseline="middle"
      >
        {p.label} ({p.value})
      </text>
    );
  }
  // Executions: label to the right, only where the node is tall enough so a
  // crowded middle column doesn't turn into overlapping text.
  if (p.h < 11) return null;
  const room = layout.statusX - (p.x + NODE_W) - LABEL_PAD * 2;
  return (
    <text
      x={p.x + NODE_W + LABEL_PAD}
      y={cy}
      className="sankey-label sankey-label-exec"
      textAnchor="start"
      dominantBaseline="middle"
    >
      {fit(p.label, room)}
    </text>
  );
}

function linkClass(target: Placed): string {
  if (target.layer === 2)
    return `link-status link-${target.label.toLowerCase()}`;
  return "link-plan";
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

// fit truncates a label to roughly the available pixel width (~6.2px/char).
function fit(s: string, pxWidth: number): string {
  const max = Math.max(4, Math.floor(pxWidth / 6.2));
  return s.length > max ? s.slice(0, max - 1) + "…" : s;
}
