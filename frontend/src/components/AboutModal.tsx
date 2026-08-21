import { useEffect, useState } from "react";

import { GetDiagnostics, BrowserOpenURL } from "../api";
import type { Diagnostics } from "../api";
import { Modal } from "./Modal";
import appIcon from "../assets/images/appicon.png";

const DOCS_URL = "https://docs.getxray.app/display/XRAY/REST+API";

// LogoMark shows the application's own icon (the same image used for the window
// / executable), so the About dialog matches the app's identity everywhere.
function LogoMark() {
  return (
    <img
      className="about-logo"
      src={appIcon}
      width={44}
      height={44}
      alt=""
      aria-hidden="true"
    />
  );
}

// AboutModal is the Help → About dialog: app identity in a "scan panel" header,
// then a precise technical readout of the runtime (useful for bug reports).
//
// onTakeTour, when supplied, adds a button that closes this dialog and replays
// the onboarding tour. About is where people look for help, so the tour is
// offered here as well as in the More menu (RND_P_4TFINT_05-335).
export function AboutModal({
  onClose,
  onTakeTour,
}: {
  onClose: () => void;
  onTakeTour?: () => void;
}) {
  const [diag, setDiag] = useState<Diagnostics | null>(null);

  useEffect(() => {
    GetDiagnostics()
      .then(setDiag)
      .catch(() => {});
  }, []);

  const rows: Array<[string, string]> = [
    ["Version", diag?.version ? `v${diag.version}` : "…"],
    ["Schema", diag ? `v${diag.schemaVersion}` : "…"],
    ["Runtime", diag ? `${diag.goVersion} · ${diag.os}/${diag.arch}` : "…"],
    ["Targets", "Jira DC 8.14+ · Xray 8.4.0"],
    ["Database", diag?.dbPath || "—"],
  ];

  return (
    <Modal onClose={onClose} className="about-card" label="About Xray Test Manager">
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
            {onTakeTour && (
              <button
                className="btn about-link"
                onClick={() => {
                  onClose();
                  onTakeTour();
                }}
                title="Replay the onboarding walkthrough"
              >
                🧭 Take the tour
              </button>
            )}
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
    </Modal>
  );
}
