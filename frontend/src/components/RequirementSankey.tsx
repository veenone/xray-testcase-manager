import { useEffect, useMemo, useRef, useState } from "react";
import type { Sankey } from "../api";

interface Props {
  data: Sankey;
}

const NODE_W = 14;
const MIN_NODE_H = 6;
const GAP = 10;
const PAD_Y = 14;
const HEIGHT = 320;
const LABEL_PAD = 9;

// Semantic palette keyed by node id, so colours stay stable regardless of the
// display label. Coverage and result share the pass-green / fail-red / amber /
// grey vocabulary so a thread reads consistently across the columns.
const NODE_COLORS: Record<string, string> = {
  "cov:PASSED": "#16a34a",
  "cov:FAILED": "#dc2626",
  "cov:NOTRUN": "#d97706",
  "cov:UNCOVERED": "#64748b",
  "res:PASS": "#16a34a",
  "res:FAIL": "#dc2626",
  "res:__norun__": "#d97706",
  "res:__notest__": "#cbd5e1",
};
function nodeColor(id: string): string {
  if (NODE_COLORS[id]) return NODE_COLORS[id];
  if (id.startsWith("req:")) return "#6366f1"; // requirement node
  if (id.startsWith("plan:")) return "#0891b2"; // Test plan bucket
  if (id.startsWith("res:")) return "#6366f1"; // other Xray statuses (TODO, …)
  return "#64748b";
}

// The flow is always four layers: requirement → coverage status → Test plan →
// run result.
const COLUMNS = ["Requirement", "Coverage", "Test plan", "Test result"];

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

// RequirementSankey hand-renders the requirement traceability flow: requirement
// → coverage status → Test plan → covering Test run result. Node counts are
// small (requirements + status buckets), so every node is labelled and the flow
// fits without scrolling. Hovering a node traces its threads.
export function RequirementSankey({ data }: Props) {
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
  const empty = !data || data.nodes.length === 0;

  return (
    <div className="sankey-wrap" ref={ref}>
      {empty ? (
        <p className="muted sankey-empty">
          No requirements cached yet — add a requirement source and sync (or
          sync a demo profile) to trace coverage.
        </p>
      ) : !layout ? (
        <div className="sankey-loading muted">…</div>
      ) : (
        <>
          <div className="sankey-cols">
            {layout.colX.map((_, i) => (
              <span
                key={i}
                className="sankey-col-head"
                style={
                  i === 0
                    ? {
                        left: 0,
                        width: layout.colX[0] - 4,
                        textAlign: "right",
                      }
                    : { left: layout.colX[i] }
                }
              >
                {COLUMNS[i]}
              </span>
            ))}
          </div>
          <Diagram layout={layout} hoverId={hoverId} setHoverId={setHoverId} />
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
      aria-label="Requirement traceability from coverage through Test plan to run results"
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
          const lit = isLit(lk);
          return (
            <path
              key={i}
              d={d}
              fill={nodeColor(lk.target)}
              opacity={lit ? 0.4 : 0.08}
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
              fill={nodeColor(p.id)}
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
  const maxLayer = data.nodes.reduce((m, n) => Math.max(m, n.layer), 0);
  const layers = Array.from({ length: maxLayer + 1 }, (_, i) => i);
  const byLayer = layers.map((L) => data.nodes.filter((n) => n.layer === L));
  if (byLayer.some((col) => col.length === 0)) return null;

  const layerTotal = Math.max(
    ...byLayer.map((col) => col.reduce((a, n) => a + n.value, 0)),
    1,
  );

  const gutterL = clamp(Math.round(width * 0.2), 140, 240);
  const gutterR = clamp(Math.round(width * 0.16), 120, 190);
  const leftX = gutterL;
  const rightX = width - gutterR - NODE_W;
  // Distribute N columns evenly between the left and right gutters.
  const n = layers.length;
  const colX = layers.map((_, i) =>
    n <= 1 ? leftX : Math.round(leftX + ((rightX - leftX) * i) / (n - 1)),
  );

  const busiest = Math.max(...byLayer.map((col) => col.length));
  const avail = HEIGHT - PAD_Y * 2 - (busiest - 1) * GAP;
  const scale = Math.max(avail, 1) / layerTotal;

  const colHeight = (col: (typeof byLayer)[number]) =>
    col.reduce((a, n) => a + Math.max(MIN_NODE_H, n.value * scale), 0) +
    Math.max(0, col.length - 1) * GAP;
  const contentH = Math.max(...byLayer.map(colHeight));
  const height = Math.max(HEIGHT, Math.ceil(contentH + PAD_Y * 2));

  const placed: Placed[] = [];
  const place = new Map<string, Placed>();
  byLayer.forEach((col, L) => {
    let y = PAD_Y + (height - PAD_Y * 2 - colHeight(col)) / 2;
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

  return { placed, links, width, height, colX, gutterL, gutterR };
}

function nodeLabel(
  p: Placed,
  layout: NonNullable<ReturnType<typeof buildLayout>>,
) {
  const cy = p.y + p.h / 2;
  if (p.layer === 0) {
    return (
      <text
        x={p.x - LABEL_PAD}
        y={cy}
        className="sankey-label"
        textAnchor="end"
        dominantBaseline="middle"
      >
        {fit(p.label, layout.gutterL - LABEL_PAD)} ({p.value})
      </text>
    );
  }
  // Middle + right columns: label to the right of the node.
  return (
    <text
      x={p.x + NODE_W + LABEL_PAD}
      y={cy}
      className="sankey-label"
      textAnchor="start"
      dominantBaseline="middle"
    >
      {p.label} ({p.value})
    </text>
  );
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

function fit(s: string, pxWidth: number): string {
  const max = Math.max(4, Math.floor(pxWidth / 6.2));
  return s.length > max ? s.slice(0, max - 1) + "…" : s;
}
