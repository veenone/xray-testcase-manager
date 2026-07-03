// CoverageGuide — static explanatory content for the Coverage module.
// No props required; content is documentation only.
export function CoverageGuide() {
  return (
    <div className="cov-guide">

      {/* ── Introduction ─────────────────────────────────────────────── */}
      <h2 className="cov-guide-heading">What the coverage module models</h2>
      <p className="cov-guide-body">
        A <strong>canonical functional requirement</strong> is a reusable function — for example a PKCS#11
        function such as <code className="cov-guide-code">C_GenerateKeyPair</code> — that one or more
        customer requirements across different projects reuse. You register canonical functions here, then
        measure how thoroughly each is tested relative to its parameter space.
      </p>
      <p className="cov-guide-body">
        Coverage % = <em>values with at least one mapped test</em> / <em>total required values</em> within
        the selected version.
      </p>

      {/* ── Parameter model hierarchy diagram ───────────────────────── */}
      <h2 className="cov-guide-heading">The parameter model hierarchy</h2>
      <p className="cov-guide-body">
        Every canonical function is modelled as a five-level hierarchy. The diagram below shows the levels
        from top to bottom.
      </p>

      <div className="cov-guide-diagram">
        <div className="cov-guide-node cov-guide-node-fn">
          <span className="cov-guide-label">Canonical function</span>
          <span className="cov-guide-desc">e.g. C_GenerateKeyPair</span>
        </div>
        <div className="cov-guide-arrow">▼</div>
        <div className="cov-guide-node cov-guide-node-ver">
          <span className="cov-guide-label">Version</span>
          <span className="cov-guide-desc">e.g. PKCS#11 v2.40 — coverage is measured per version</span>
        </div>
        <div className="cov-guide-arrow">▼</div>
        <div className="cov-guide-node cov-guide-node-grp">
          <span className="cov-guide-label">Group</span>
          <span className="cov-guide-desc">organises related parameters (e.g. one group per function argument)</span>
        </div>
        <div className="cov-guide-arrow">▼</div>
        <div className="cov-guide-node cov-guide-node-par">
          <span className="cov-guide-label">Parameter</span>
          <span className="cov-guide-desc">a function argument or behaviour axis, e.g. <code className="cov-guide-code">pMechanism</code></span>
        </div>
        <div className="cov-guide-arrow">▼</div>
        <div className="cov-guide-node cov-guide-node-val">
          <span className="cov-guide-label">Value</span>
          <span className="cov-guide-desc">a concrete input or error code, e.g. <code className="cov-guide-code">CKM_RSA_PKCS_KEY_PAIR_GEN</code> or <code className="cov-guide-code">CKR_TEMPLATE_INCOMPLETE</code></span>
        </div>
        <div className="cov-guide-arrow">▼</div>
        <div className="cov-guide-node cov-guide-node-tst">
          <span className="cov-guide-label">Mapped tests</span>
          <span className="cov-guide-desc">one or more Xray tests that exercise this value — a value with at least one mapped test is <em>covered</em>; otherwise it is a <em>gap</em></span>
        </div>
      </div>

      {/* ── Where groups & parameters come from ─────────────────────── */}
      <h2 className="cov-guide-heading">Where groups and parameters come from</h2>
      <p className="cov-guide-body">Three ways to populate the model:</p>
      <ol className="cov-guide-list">
        <li>
          <strong>Built-in demo seed</strong> — in demo mode the "Seed PKCS#11 reference" action loads a
          ready-made model for the most common PKCS#11 functions, so you can explore the UI immediately.
        </li>
        <li>
          <strong>Excel import</strong> — download the blank template ("Download blank template…"), fill in
          groups, parameters, and values using the function's specification, then use the "Import" action to
          load it.
        </li>
        <li>
          <strong>Manual entry</strong> — add groups and parameters directly in the <em>Coverage</em> tab,
          then map tests to individual values with the "Map…" button.
        </li>
      </ol>
      <p className="cov-guide-body">
        Parameters and values typically come from the function's specification or API signature.
      </p>

      {/* ── Expected Jira structure diagram ─────────────────────────── */}
      <h2 className="cov-guide-heading">Expected Jira / Xray structure</h2>
      <p className="cov-guide-body">
        The coverage module works on top of your existing Jira and Xray data. The diagram below shows how
        projects, requirements, and tests connect.
      </p>

      <div className="cov-guide-jira">

        {/* Row 1: projects */}
        <div className="cov-guide-jira-row">
          <div className="cov-guide-jira-box cov-guide-jira-source">
            <span className="cov-guide-jira-kind">Source project</span>
            <span className="cov-guide-jira-name">Owns canonical requirements</span>
          </div>
          <div className="cov-guide-jira-sep">+</div>
          <div className="cov-guide-jira-box cov-guide-jira-customer">
            <span className="cov-guide-jira-kind">Customer project(s)</span>
            <span className="cov-guide-jira-name">Reuse the canonical via member requirements</span>
          </div>
        </div>

        {/* Arrow down */}
        <div className="cov-guide-jira-arrow">▼</div>

        {/* Row 2: requirements */}
        <div className="cov-guide-jira-row">
          <div className="cov-guide-jira-box cov-guide-jira-req">
            <span className="cov-guide-jira-kind">Requirements (Jira issues)</span>
            <span className="cov-guide-jira-name">
              Customer requirement issues become <em>members</em> of a canonical function via the
              "Members" action in this view.
            </span>
          </div>
        </div>

        {/* Arrow down */}
        <div className="cov-guide-jira-arrow">▼</div>

        {/* Row 3: tests */}
        <div className="cov-guide-jira-row">
          <div className="cov-guide-jira-box cov-guide-jira-tst">
            <span className="cov-guide-jira-kind">Tests (Xray test issues)</span>
            <span className="cov-guide-jira-name">
              Xray tests are linked to requirements and then mapped to parameter values here. A Sync
              pulls requirements and tests from Jira; the coverage model (parameters / values) is
              managed locally.
            </span>
          </div>
        </div>
      </div>

      <p className="cov-guide-body cov-guide-tip">
        Declare which projects are <em>source</em> or <em>customer</em> in the <strong>Coverage Map</strong> tab's
        project configuration. A regular Sync then pulls the matching requirements and tests so you can map
        them to parameter values here.
      </p>

    </div>
  );
}
