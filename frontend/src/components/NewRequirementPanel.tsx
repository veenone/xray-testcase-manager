import { useEffect, useState } from "react";
import {
  CreateRequirement,
  GetRequirementCreateFields,
  ListRequirementSources,
  ListPriorities,
  ListProjectComponents,
  ListProjectFixVersions,
  errMsg,
} from "../api";
import type { BugCreateField, RequirementSource } from "../api";
import { MarkdownField } from "./MarkdownField";
import { MultiSelect } from "./MultiSelect";
import type { MultiOption } from "./MultiSelect";
import {
  buildCreateFieldsPayload,
  createFieldsValid,
  initCreateFieldDefaults,
  type RawFieldValue,
} from "./createFields";

interface Props {
  profileId: string;
  onCreated: (tempKey: string) => void;
  onCancel: () => void;
}

export function NewRequirementPanel({ profileId, onCreated, onCancel }: Props) {
  const [sources, setSources] = useState<RequirementSource[]>([]);
  const [projectKey, setProjectKey] = useState("");
  const [issueType, setIssueType] = useState("");
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState("");
  // #5: selected tags for components / fix versions.
  const [selectedComponents, setSelectedComponents] = useState<string[]>([]);
  const [selectedFixVersions, setSelectedFixVersions] = useState<string[]>([]);
  // Available values loaded from the backend per project.
  const [componentOptions, setComponentOptions] = useState<string[]>([]);
  const [fixVersionOptions, setFixVersionOptions] = useState<string[]>([]);
  // Free-text inputs for adding a new value not in the list.
  const [newComponent, setNewComponent] = useState("");
  const [newFixVersion, setNewFixVersion] = useState("");
  const [priorityOptions, setPriorityOptions] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  // Required custom fields on the requirement create screen (createmeta-driven),
  // e.g. "Req. type". Loaded per project + issue type; blocked create until set.
  const [extraFields, setExtraFields] = useState<BugCreateField[]>([]);
  const [extraLoading, setExtraLoading] = useState(false);
  const [extraValues, setExtraValues] = useState<Record<string, RawFieldValue>>({});

  // #2: width persisted under its own key (not shared with the right-side detail).
  const [width, setWidth] = useState<number>(() => {
    const saved = Number(localStorage.getItem("xtm.newReqPanelWidth"));
    return saved >= 320 && saved <= 900 ? saved : 440;
  });
  useEffect(() => {
    localStorage.setItem("xtm.newReqPanelWidth", String(width));
  }, [width]);

  // #2: left-docked — resizer lives on the RIGHT edge; drag right = wider.
  function startResize(e: React.MouseEvent) {
    e.preventDefault();
    const startX = e.clientX;
    const startW = width;
    const onMove = (ev: MouseEvent) =>
      setWidth(Math.min(900, Math.max(320, startW + (ev.clientX - startX))));
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }

  // Load sources and priorities when the profile changes.
  useEffect(() => {
    if (!profileId) return;
    ListRequirementSources(profileId)
      .then((ss) => {
        setSources(ss ?? []);
        if (ss && ss.length > 0) {
          setProjectKey(ss[0].projectKey);
          // #4: split issueTypes on whitespace.
          const types = (ss[0].issueTypes ?? "").split(/\s+/).filter(Boolean);
          setIssueType(types[0] ?? "");
        }
      })
      .catch(() => setSources([]));
    ListPriorities(profileId)
      .then((ps) => setPriorityOptions(ps ?? []))
      .catch(() => setPriorityOptions([]));
  }, [profileId]);

  // #4: split issueTypes on whitespace into separate options.
  const selectedSource = sources.find((s) => s.projectKey === projectKey) ?? null;
  const issueTypeOptions = (selectedSource?.issueTypes ?? "")
    .split(/\s+/)
    .filter(Boolean);

  // #5: load available components and fix versions when projectKey changes.
  useEffect(() => {
    if (!profileId || !projectKey) {
      setComponentOptions([]);
      setFixVersionOptions([]);
      return;
    }
    Promise.all([
      ListProjectComponents(profileId, projectKey).catch(() => [] as string[]),
      ListProjectFixVersions(profileId, projectKey).catch(() => [] as string[]),
    ]).then(([comps, vers]) => {
      setComponentOptions(comps ?? []);
      setFixVersionOptions(vers ?? []);
    });
  }, [profileId, projectKey]);

  // Load required custom fields when the project or issue type changes, so the
  // form can collect them before commit (some instances mark e.g. "Req. type"
  // required). Non-fatal: a createmeta failure just shows the base form.
  useEffect(() => {
    if (!profileId || !projectKey) {
      setExtraFields([]);
      setExtraValues({});
      return;
    }
    let cancelled = false;
    setExtraLoading(true);
    GetRequirementCreateFields(profileId, projectKey, issueType)
      .then((fields) => {
        if (cancelled) return;
        setExtraFields(fields ?? []);
        setExtraValues(initCreateFieldDefaults(fields ?? []));
      })
      .catch(() => {
        if (!cancelled) setExtraFields([]);
      })
      .finally(() => {
        if (!cancelled) setExtraLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, projectKey, issueType]);

  async function submit() {
    if (!summary.trim()) {
      setError("A summary is required.");
      return;
    }
    if (!projectKey.trim()) {
      setError("A project key is required.");
      return;
    }
    if (!createFieldsValid(extraFields, extraValues)) {
      setError("Fill in all required fields.");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const tempKey = await CreateRequirement(
        profileId,
        projectKey,
        issueType,
        summary.trim(),
        description,
        priority,
        selectedComponents.join(", "),  // #5: comma-joined
        selectedFixVersions.join(", "), // #5: comma-joined
        buildCreateFieldsPayload(extraFields, extraValues),
      );
      onCreated(tempKey);
    } catch (e) {
      setError(errMsg(e));
      setSaving(false);
    }
  }

  function setSingleExtra(id: string, value: string) {
    setExtraValues((prev) => ({ ...prev, [id]: value }));
  }
  function setMultiExtra(id: string, selected: string[]) {
    setExtraValues((prev) => ({ ...prev, [id]: selected }));
  }

  // Render one required custom field using the panel's field styles.
  function renderExtraField(f: BugCreateField) {
    const raw = extraValues[f.id] ?? "";
    const label = `${f.name}${f.required ? " *" : ""}`;
    if (f.type === "text") {
      return (
        <div key={f.id}>
          <div className="field-label">{label}</div>
          <textarea
            className="detail-input"
            rows={3}
            value={typeof raw === "string" ? raw : ""}
            onChange={(e) => setSingleExtra(f.id, e.target.value)}
          />
        </div>
      );
    }
    if (f.type === "number" || f.type === "date") {
      return (
        <div key={f.id}>
          <div className="field-label">{label}</div>
          <input
            className="detail-input"
            type={f.type === "number" ? "number" : "date"}
            value={typeof raw === "string" ? raw : ""}
            onChange={(e) => setSingleExtra(f.id, e.target.value)}
          />
        </div>
      );
    }
    if (f.type === "option" || f.type === "version") {
      const selected = typeof raw === "string" ? raw : "";
      return (
        <div key={f.id}>
          <div className="field-label">{label}</div>
          <select
            className="detail-input"
            value={selected}
            onChange={(e) => setSingleExtra(f.id, e.target.value)}
          >
            {!selected && <option value="">(select)</option>}
            {f.allowedValues.map((av) => (
              <option key={av.id} value={av.id}>
                {av.value}
              </option>
            ))}
          </select>
        </div>
      );
    }
    if (f.type === "versions" || f.type === "array") {
      const selected = Array.isArray(raw) ? raw : [];
      const opts: MultiOption[] = f.allowedValues.map((av) => ({
        value: av.id,
        label: av.value,
      }));
      return (
        <div key={f.id}>
          <div className="field-label">{label}</div>
          <MultiSelect
            options={opts}
            selected={selected}
            onChange={(next) => setMultiExtra(f.id, next)}
            allLabel={`Select ${f.name}…`}
          />
        </div>
      );
    }
    return (
      <div key={f.id}>
        <div className="field-label">{label}</div>
        <textarea
          className="detail-input"
          rows={3}
          value={typeof raw === "string" ? raw : ""}
          onChange={(e) => setSingleExtra(f.id, e.target.value)}
        />
      </div>
    );
  }

  // #5 helpers: add/remove a tag in a multi-select tag list.
  function addComponent(value: string) {
    const v = value.trim();
    if (v && !selectedComponents.includes(v)) {
      setSelectedComponents([...selectedComponents, v]);
    }
    setNewComponent("");
  }
  function addFixVersion(value: string) {
    const v = value.trim();
    if (v && !selectedFixVersions.includes(v)) {
      setSelectedFixVersions([...selectedFixVersions, v]);
    }
    setNewFixVersion("");
  }

  return (
    // #2: new-req-panel class for left-dock overrides; new-test-panel for shared
    // form styles (ntp-* rules); detail for base panel structure.
    <aside className="detail new-test-panel new-req-panel" style={{ width }}>
      <div className="detail-resizer" onMouseDown={startResize} title="Drag to resize" />
      <div className="detail-head">
        <div className="detail-head-id">
          <span className="detail-key">New Requirement</span>
          <span className="ntp-badge">unsaved</span>
        </div>
        <button className="btn btn-ghost detail-close" onClick={onCancel} title="Cancel">
          &#10005;
        </button>
      </div>

      <div className="detail-body">
        {error && <div className="error-text detail-save-error">{error}</div>}

        {/* Project + Issue type row */}
        <div className="ntp-row">
          <div className="ntp-col">
            <div className="field-label">Project *</div>
            <select
              className="detail-input"
              value={projectKey}
              onChange={(e) => {
                const key = e.target.value;
                setProjectKey(key);
                const src = sources.find((s) => s.projectKey === key);
                // #4: space-split when updating issue type on project change.
                const types = (src?.issueTypes ?? "").split(/\s+/).filter(Boolean);
                setIssueType(types[0] ?? "");
                // Reset tag selections when project changes.
                setSelectedComponents([]);
                setSelectedFixVersions([]);
              }}
            >
              <option value="">(select project)</option>
              {sources.map((s) => (
                <option key={s.projectKey} value={s.projectKey}>
                  {s.projectKey}
                </option>
              ))}
            </select>
          </div>
          <div className="ntp-col">
            <div className="field-label">Issue type</div>
            {/* #4: each space-separated token is a separate <option>. */}
            <select
              className="detail-input"
              value={issueType}
              onChange={(e) => setIssueType(e.target.value)}
              disabled={issueTypeOptions.length === 0}
            >
              {issueTypeOptions.length === 0 && <option value="">(any)</option>}
              {issueTypeOptions.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* #3: Summary as multiline MarkdownField. */}
        <div className="field-label">Summary *</div>
        <MarkdownField
          className="detail-input"
          value={summary}
          onChange={setSummary}
          onCommit={() => {}}
          rows={2}
          placeholder="What does this requirement specify?"
        />

        <div className="field-label">Description</div>
        <MarkdownField
          className="detail-input"
          value={description}
          onChange={setDescription}
          onCommit={() => {}}
          rows={4}
          placeholder="Markdown supported"
        />

        <div className="ntp-row">
          <div className="ntp-col">
            <div className="field-label">Priority</div>
            <select
              className="detail-input"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
            >
              <option value="">(default)</option>
              {priorityOptions.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* #5: Components — pick from available list or free-add. */}
        <div className="field-label">Components</div>
        <div className="req-tag-field">
          {selectedComponents.length > 0 && (
            <div className="req-tags">
              {selectedComponents.map((c) => (
                <span key={c} className="req-tag">
                  {c}
                  <button
                    type="button"
                    className="req-tag-remove"
                    onClick={() =>
                      setSelectedComponents(selectedComponents.filter((x) => x !== c))
                    }
                    title="Remove"
                  >
                    &#10005;
                  </button>
                </span>
              ))}
            </div>
          )}
          {componentOptions.length > 0 && (
            <select
              className="detail-input req-tag-select"
              value=""
              onChange={(e) => {
                const v = e.target.value;
                if (v && !selectedComponents.includes(v)) {
                  setSelectedComponents([...selectedComponents, v]);
                }
              }}
            >
              <option value="">+ pick from list…</option>
              {componentOptions
                .filter((c) => !selectedComponents.includes(c))
                .map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
            </select>
          )}
          <div className="req-tag-add">
            <input
              className="detail-input req-tag-input"
              value={newComponent}
              onChange={(e) => setNewComponent(e.target.value)}
              placeholder={
                componentOptions.length === 0
                  ? "Add component (sync to load list)"
                  : "Add new…"
              }
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  addComponent(newComponent);
                }
              }}
            />
            <button
              type="button"
              className="btn"
              onClick={() => addComponent(newComponent)}
              disabled={!newComponent.trim()}
            >
              Add
            </button>
          </div>
          {componentOptions.length === 0 && (
            <p className="muted req-tag-hint">Sync to load available components.</p>
          )}
        </div>

        {/* #5: Fix versions — pick from available list or free-add. */}
        <div className="field-label">Fix versions</div>
        <div className="req-tag-field">
          {selectedFixVersions.length > 0 && (
            <div className="req-tags">
              {selectedFixVersions.map((v) => (
                <span key={v} className="req-tag">
                  {v}
                  <button
                    type="button"
                    className="req-tag-remove"
                    onClick={() =>
                      setSelectedFixVersions(selectedFixVersions.filter((x) => x !== v))
                    }
                    title="Remove"
                  >
                    &#10005;
                  </button>
                </span>
              ))}
            </div>
          )}
          {fixVersionOptions.length > 0 && (
            <select
              className="detail-input req-tag-select"
              value=""
              onChange={(e) => {
                const v = e.target.value;
                if (v && !selectedFixVersions.includes(v)) {
                  setSelectedFixVersions([...selectedFixVersions, v]);
                }
              }}
            >
              <option value="">+ pick from list…</option>
              {fixVersionOptions
                .filter((v) => !selectedFixVersions.includes(v))
                .map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
            </select>
          )}
          <div className="req-tag-add">
            <input
              className="detail-input req-tag-input"
              value={newFixVersion}
              onChange={(e) => setNewFixVersion(e.target.value)}
              placeholder={
                fixVersionOptions.length === 0
                  ? "Add version (sync to load list)"
                  : "Add new…"
              }
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  addFixVersion(newFixVersion);
                }
              }}
            />
            <button
              type="button"
              className="btn"
              onClick={() => addFixVersion(newFixVersion)}
              disabled={!newFixVersion.trim()}
            >
              Add
            </button>
          </div>
          {fixVersionOptions.length === 0 && (
            <p className="muted req-tag-hint">Sync to load available fix versions.</p>
          )}
        </div>

        {/* Required custom fields from Jira createmeta (e.g. "Req. type"). */}
        {extraLoading && (
          <p className="muted req-tag-hint">Loading required fields…</p>
        )}
        {!extraLoading && extraFields.map(renderExtraField)}
      </div>

      <div className="ntp-foot">
        <button className="btn" onClick={onCancel} disabled={saving}>
          Cancel
        </button>
        <button
          className="btn btn-primary"
          onClick={submit}
          disabled={
            saving ||
            !summary.trim() ||
            !projectKey.trim() ||
            extraLoading ||
            !createFieldsValid(extraFields, extraValues)
          }
        >
          {saving ? "Creating…" : "Create →"}
        </button>
      </div>
    </aside>
  );
}
