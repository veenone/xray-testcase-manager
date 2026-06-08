import { useEffect, useState } from "react";

import { GetDiagnostics, BrowserOpenURL } from "../api";
import type { Diagnostics } from "../api";

const DOCS_URL = "https://docs.getxray.app/display/XRAY/REST+API";

// LogoMark is a small emblem for the app: a checkmark (tests passing) over faint
// horizontal "scan" lines (the X-ray motif), on an accent-gradient tile. Drawn
// with currentColor-free theme variables so it reads in both light and dark.
function LogoMark() {
  return (
    <svg
      className="about-logo"
      viewBox="0 0 44 44"
      width="44"
      height="44"
      aria-hidden="true"
    >
      <defs>
        <linearGradient id="xtm-logo" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--accent-hover)" />
        </linearGradient>
      </defs>
      <rect x="2" y="2" width="40" height="40" rx="11" fill="url(#xtm-logo)" />
      <g stroke="#fff" strokeLinecap="round">
        <line x1="11" y1="16" x2="33" y2="16" strokeWidth="1.5" opacity="0.22" />
        <line x1="11" y1="28" x2="33" y2="28" strokeWidth="1.5" opacity="0.22" />
        <path
          d="M14 22.5 l5 5 l11 -12.5"
          fill="none"
          strokeWidth="3.4"
          strokeLinejoin="round"
        />
      </g>
    </svg>
  );
}

// AboutModal is the Help → About dialog: app identity in a "scan panel" header,
// then a precise technical readout of the runtime (useful for bug reports).
export function AboutModal({ onClose }: { onClose: () => void }) {
  const [diag, setDiag] = useState<Diagnostics | null>(null);

  useEffect(() => {
    GetDiagnostics()
      .then(setDiag)
      .catch(() => {});
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const rows: Array<[string, string]> = [
    ["Version", diag?.version ? `v${diag.version}` : "…"],
    ["Schema", diag ? `v${diag.schemaVersion}` : "…"],
    ["Runtime", diag ? `${diag.goVersion} · ${diag.os}/${diag.arch}` : "…"],
    ["Targets", "Jira DC 8.14+ · Xray 8.4.0"],
    ["Database", diag?.dbPath || "—"],
  ];

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="about-card"
        role="dialog"
        aria-label="About Xray Test Manager"
        onClick={(e) => e.stopPropagation()}
      >
        <button className="about-close" onClick={onClose} title="Close" aria-label="Close">
          ✕
        </button>

        <header className="about-banner">
          <span className="about-banner-grid" aria-hidden="true" />
          <LogoMark />
          <div className="about-id">
            <h2 className="about-name">Xray Test Manager</h2>
            <p className="about-kicker">Test Repository · Data Center</p>
          </div>
        </header>

        <div className="about-body">
          <p className="about-tagline">
            A fast, local-first desktop tool for managing Xray test cases in
            Jira Data Center at scale.
          </p>

          <div className="about-plate">
            <span className="about-plate-label">Build &amp; environment</span>
            <dl className="about-info">
              {rows.map(([k, v]) => (
                <div className="about-row" key={k}>
                  <dt>{k}</dt>
                  <dd className="mono">{v}</dd>
                </div>
              ))}
            </dl>
          </div>

          <div className="about-actions">
            <button
              className="btn about-link"
              onClick={() => BrowserOpenURL(DOCS_URL)}
            >
              Documentation <span className="about-arrow">↗</span>
            </button>
            <button className="btn btn-primary" onClick={onClose}>
              Close
            </button>
          </div>
        </div>

        <footer className="about-foot">
          © 2026 Achmad Fienan Rahardianto
        </footer>
      </div>
    </div>
  );
}
