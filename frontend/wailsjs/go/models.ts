export namespace backend {
	
	export class Capabilities {
	    name: string;
	    idStyle: string;
	    supportsJqlScope: boolean;
	    stepModel: string;
	    supportsTestTypes: boolean;
	    supportsFolders: boolean;
	    supportsPreconditionObjects: boolean;
	    supportsRequirementObjects: boolean;
	    supportsIssueLinkTypes: boolean;
	    supportsEnvironments: boolean;
	    supportsContainers: boolean;
	    containerKinds: string[];
	    supportsTestRuns: boolean;
	    statusModel: string;
	    supportsWorkflowTransitions: boolean;
	    supportsBugCreation: boolean;
	    supportsBugLinks: boolean;
	    supportsTags: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Capabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.idStyle = source["idStyle"];
	        this.supportsJqlScope = source["supportsJqlScope"];
	        this.stepModel = source["stepModel"];
	        this.supportsTestTypes = source["supportsTestTypes"];
	        this.supportsFolders = source["supportsFolders"];
	        this.supportsPreconditionObjects = source["supportsPreconditionObjects"];
	        this.supportsRequirementObjects = source["supportsRequirementObjects"];
	        this.supportsIssueLinkTypes = source["supportsIssueLinkTypes"];
	        this.supportsEnvironments = source["supportsEnvironments"];
	        this.supportsContainers = source["supportsContainers"];
	        this.containerKinds = source["containerKinds"];
	        this.supportsTestRuns = source["supportsTestRuns"];
	        this.statusModel = source["statusModel"];
	        this.supportsWorkflowTransitions = source["supportsWorkflowTransitions"];
	        this.supportsBugCreation = source["supportsBugCreation"];
	        this.supportsBugLinks = source["supportsBugLinks"];
	        this.supportsTags = source["supportsTags"];
	    }
	}

}

export namespace coverage {
	
	export class CRDecision {
	    requirementKey: string;
	    projectKey: string;
	    decision: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new CRDecision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requirementKey = source["requirementKey"];
	        this.projectKey = source["projectKey"];
	        this.decision = source["decision"];
	        this.note = source["note"];
	    }
	}
	export class ChangeRequest {
	    id: string;
	    crKey: string;
	    title: string;
	    status: string;
	    targetVersionId: string;
	    risk: string;
	    description: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ChangeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.crKey = source["crKey"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.targetVersionId = source["targetVersionId"];
	        this.risk = source["risk"];
	        this.description = source["description"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class CRImpactResult {
	    cr: ChangeRequest;
	    decisions: CRDecision[];
	    canAccept: number;
	    cannotAccept: number;
	    pending: number;
	
	    static createFrom(source: any = {}) {
	        return new CRImpactResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cr = this.convertValues(source["cr"], ChangeRequest);
	        this.decisions = this.convertValues(source["decisions"], CRDecision);
	        this.canAccept = source["canAccept"];
	        this.cannotAccept = source["cannotAccept"];
	        this.pending = source["pending"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CRShare {
	    crId: string;
	    title: string;
	    status: string;
	    canAccept: number;
	    cannotAccept: number;
	    pending: number;
	
	    static createFrom(source: any = {}) {
	        return new CRShare(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.crId = source["crId"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.canAccept = source["canAccept"];
	        this.cannotAccept = source["cannotAccept"];
	        this.pending = source["pending"];
	    }
	}
	export class CandidateTest {
	    testKey: string;
	    summary: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new CandidateTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	    }
	}
	export class CanonicalRequirement {
	    id: string;
	    name: string;
	    category: string;
	    description: string;
	    createdAt: string;
	    updatedAt: string;
	    memberCount: number;
	
	    static createFrom(source: any = {}) {
	        return new CanonicalRequirement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.description = source["description"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.memberCount = source["memberCount"];
	    }
	}
	
	export class ValueCoverage {
	    valueId: string;
	    testKeys: string[];
	    tested: boolean;
	    runStatus: string;
	    isRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ValueCoverage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.valueId = source["valueId"];
	        this.testKeys = source["testKeys"];
	        this.tested = source["tested"];
	        this.runStatus = source["runStatus"];
	        this.isRequired = source["isRequired"];
	    }
	}
	export class GroupCoverage {
	    groupId: string;
	    name: string;
	    total: number;
	    tested: number;
	    percent: number;
	
	    static createFrom(source: any = {}) {
	        return new GroupCoverage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groupId = source["groupId"];
	        this.name = source["name"];
	        this.total = source["total"];
	        this.tested = source["tested"];
	        this.percent = source["percent"];
	    }
	}
	export class CoverageReport {
	    versionId: string;
	    totalValues: number;
	    testedValues: number;
	    percent: number;
	    groups: GroupCoverage[];
	    values: Record<string, ValueCoverage>;
	
	    static createFrom(source: any = {}) {
	        return new CoverageReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.totalValues = source["totalValues"];
	        this.testedValues = source["testedValues"];
	        this.percent = source["percent"];
	        this.groups = this.convertValues(source["groups"], GroupCoverage);
	        this.values = this.convertValues(source["values"], ValueCoverage, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EUICCSeedSummary {
	    features: number;
	    requirements: number;
	    tests: number;
	    versions: number;
	    changeRequests: number;
	    mappings: number;
	
	    static createFrom(source: any = {}) {
	        return new EUICCSeedSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.features = source["features"];
	        this.requirements = source["requirements"];
	        this.tests = source["tests"];
	        this.versions = source["versions"];
	        this.changeRequests = source["changeRequests"];
	        this.mappings = source["mappings"];
	    }
	}
	export class Gap {
	    groupName: string;
	    paramName: string;
	    valueId: string;
	    valueLabel: string;
	    valueKind: string;
	    errorCode: string;
	
	    static createFrom(source: any = {}) {
	        return new Gap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groupName = source["groupName"];
	        this.paramName = source["paramName"];
	        this.valueId = source["valueId"];
	        this.valueLabel = source["valueLabel"];
	        this.valueKind = source["valueKind"];
	        this.errorCode = source["errorCode"];
	    }
	}
	
	export class ImportSummary {
	    groups: number;
	    parameters: number;
	    values: number;
	    mappedTests: number;
	    skipped: number;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groups = source["groups"];
	        this.parameters = source["parameters"];
	        this.values = source["values"];
	        this.mappedTests = source["mappedTests"];
	        this.skipped = source["skipped"];
	        this.warnings = source["warnings"];
	    }
	}
	export class NodeEdit {
	    kind: string;
	    canonicalId: string;
	    versionId: string;
	    groupId: string;
	    parameterId: string;
	    id: string;
	    name: string;
	    paramKind: string;
	    valueKind: string;
	    errorCode: string;
	    isRequired: boolean;
	    notes: string;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new NodeEdit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.canonicalId = source["canonicalId"];
	        this.versionId = source["versionId"];
	        this.groupId = source["groupId"];
	        this.parameterId = source["parameterId"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.paramKind = source["paramKind"];
	        this.valueKind = source["valueKind"];
	        this.errorCode = source["errorCode"];
	        this.isRequired = source["isRequired"];
	        this.notes = source["notes"];
	        this.sortOrder = source["sortOrder"];
	    }
	}
	export class PKCSSeedSummary {
	    features: number;
	    requirements: number;
	    tests: number;
	    versions: number;
	    changeRequests: number;
	    mappings: number;
	
	    static createFrom(source: any = {}) {
	        return new PKCSSeedSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.features = source["features"];
	        this.requirements = source["requirements"];
	        this.tests = source["tests"];
	        this.versions = source["versions"];
	        this.changeRequests = source["changeRequests"];
	        this.mappings = source["mappings"];
	    }
	}
	export class ParamValue {
	    id: string;
	    valueLabel: string;
	    valueKind: string;
	    errorCode: string;
	    isRequired: boolean;
	    notes: string;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new ParamValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.valueLabel = source["valueLabel"];
	        this.valueKind = source["valueKind"];
	        this.errorCode = source["errorCode"];
	        this.isRequired = source["isRequired"];
	        this.notes = source["notes"];
	        this.sortOrder = source["sortOrder"];
	    }
	}
	export class Parameter {
	    id: string;
	    name: string;
	    kind: string;
	    description: string;
	    sortOrder: number;
	    values: ParamValue[];
	
	    static createFrom(source: any = {}) {
	        return new Parameter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.description = source["description"];
	        this.sortOrder = source["sortOrder"];
	        this.values = this.convertValues(source["values"], ParamValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ParamGroup {
	    id: string;
	    name: string;
	    sortOrder: number;
	    parameters: Parameter[];
	
	    static createFrom(source: any = {}) {
	        return new ParamGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.sortOrder = source["sortOrder"];
	        this.parameters = this.convertValues(source["parameters"], Parameter);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ParamModel {
	    versionId: string;
	    groups: ParamGroup[];
	
	    static createFrom(source: any = {}) {
	        return new ParamModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.groups = this.convertValues(source["groups"], ParamGroup);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class ProjectConfig {
	    projectKey: string;
	    role: string;
	    label: string;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new ProjectConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectKey = source["projectKey"];
	        this.role = source["role"];
	        this.label = source["label"];
	        this.sortOrder = source["sortOrder"];
	    }
	}
	export class ProjectCoverageRow {
	    projectKey: string;
	    role: string;
	    label: string;
	    requirementCount: number;
	    functionsReused: number;
	    coveredValues: number;
	    totalValues: number;
	    percent: number;
	
	    static createFrom(source: any = {}) {
	        return new ProjectCoverageRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectKey = source["projectKey"];
	        this.role = source["role"];
	        this.label = source["label"];
	        this.requirementCount = source["requirementCount"];
	        this.functionsReused = source["functionsReused"];
	        this.coveredValues = source["coveredValues"];
	        this.totalValues = source["totalValues"];
	        this.percent = source["percent"];
	    }
	}
	export class ReuseRow {
	    canonicalId: string;
	    requirementKey: string;
	    projectKey: string;
	    summary: string;
	    status: string;
	    acceptedVersionId: string;
	
	    static createFrom(source: any = {}) {
	        return new ReuseRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canonicalId = source["canonicalId"];
	        this.requirementKey = source["requirementKey"];
	        this.projectKey = source["projectKey"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.acceptedVersionId = source["acceptedVersionId"];
	    }
	}
	export class StaleMapping {
	    valueId: string;
	    valueLabel: string;
	    testKey: string;
	
	    static createFrom(source: any = {}) {
	        return new StaleMapping(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.valueId = source["valueId"];
	        this.valueLabel = source["valueLabel"];
	        this.testKey = source["testKey"];
	    }
	}
	
	export class Version {
	    id: string;
	    name: string;
	    status: string;
	    notes: string;
	    sortOrder: number;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Version(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.status = source["status"];
	        this.notes = source["notes"];
	        this.sortOrder = source["sortOrder"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class VersionShare {
	    versionId: string;
	    versionName: string;
	    status: string;
	    memberCount: number;
	
	    static createFrom(source: any = {}) {
	        return new VersionShare(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.versionName = source["versionName"];
	        this.status = source["status"];
	        this.memberCount = source["memberCount"];
	    }
	}

}

export namespace jira {
	
	export class BugFieldOption {
	    id: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new BugFieldOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.value = source["value"];
	    }
	}
	export class BugCreateField {
	    id: string;
	    name: string;
	    required: boolean;
	    type: string;
	    allowedValues: BugFieldOption[];
	
	    static createFrom(source: any = {}) {
	        return new BugCreateField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.required = source["required"];
	        this.type = source["type"];
	        this.allowedValues = this.convertValues(source["allowedValues"], BugFieldOption);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BugDetail {
	    description: string;
	    defectOrigin: string;
	    defectAnalysis: string;
	    correctionDetails: string;
	    reporter: string;
	    severity: string;
	
	    static createFrom(source: any = {}) {
	        return new BugDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.description = source["description"];
	        this.defectOrigin = source["defectOrigin"];
	        this.defectAnalysis = source["defectAnalysis"];
	        this.correctionDetails = source["correctionDetails"];
	        this.reporter = source["reporter"];
	        this.severity = source["severity"];
	    }
	}
	
	export class TestMeta {
	    created: string;
	    creator: string;
	    updated: string;
	    updatedBy: string;
	
	    static createFrom(source: any = {}) {
	        return new TestMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = source["created"];
	        this.creator = source["creator"];
	        this.updated = source["updated"];
	        this.updatedBy = source["updatedBy"];
	    }
	}
	export class Transition {
	    id: string;
	    name: string;
	    to: string;
	
	    static createFrom(source: any = {}) {
	        return new Transition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.to = source["to"];
	    }
	}

}

export namespace main {
	
	export class BulkTransitionOptions {
	    currentStatusCounts: Record<string, number>;
	    reachableTargets: string[];
	
	    static createFrom(source: any = {}) {
	        return new BulkTransitionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentStatusCounts = source["currentStatusCounts"];
	        this.reachableTargets = source["reachableTargets"];
	    }
	}
	export class BulkTransitionSkip {
	    testKey: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new BulkTransitionSkip(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.reason = source["reason"];
	    }
	}
	export class BulkTransitionResult {
	    succeeded: string[];
	    skipped: BulkTransitionSkip[];
	    failed: testrepo.BulkFailure[];
	
	    static createFrom(source: any = {}) {
	        return new BulkTransitionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.succeeded = source["succeeded"];
	        this.skipped = this.convertValues(source["skipped"], BulkTransitionSkip);
	        this.failed = this.convertValues(source["failed"], testrepo.BulkFailure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Diagnostics {
	    version: string;
	    dbPath: string;
	    logPath: string;
	    os: string;
	    arch: string;
	    goVersion: string;
	    schemaVersion: number;
	    profileCount: number;
	    startupError: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.dbPath = source["dbPath"];
	        this.logPath = source["logPath"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.goVersion = source["goVersion"];
	        this.schemaVersion = source["schemaVersion"];
	        this.profileCount = source["profileCount"];
	        this.startupError = source["startupError"];
	    }
	}
	export class HealthInfo {
	    ok: boolean;
	    error: string;
	    dbPath: string;
	    logPath: string;
	
	    static createFrom(source: any = {}) {
	        return new HealthInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.dbPath = source["dbPath"];
	        this.logPath = source["logPath"];
	    }
	}
	export class JiraStepInfo {
	    count: number;
	    allBlank: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JiraStepInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.allBlank = source["allBlank"];
	    }
	}

}

export namespace profile {
	
	export class Profile {
	    id: string;
	    name: string;
	    jiraUrl: string;
	    projectKey: string;
	    scopeJql: string;
	    bugIssueType: string;
	    bugProjectMode: string;
	    bugProjectKey: string;
	    caCert: string;
	    allowUntrustedTls: boolean;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.jiraUrl = source["jiraUrl"];
	        this.projectKey = source["projectKey"];
	        this.scopeJql = source["scopeJql"];
	        this.bugIssueType = source["bugIssueType"];
	        this.bugProjectMode = source["bugProjectMode"];
	        this.bugProjectKey = source["bugProjectKey"];
	        this.caCert = source["caCert"];
	        this.allowUntrustedTls = source["allowUntrustedTls"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace settings {
	
	export class Settings {
	    defaultProfileId: string;
	    theme: string;
	    requirementLinkType: string;
	    showCoverage: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.defaultProfileId = source["defaultProfileId"];
	        this.theme = source["theme"];
	        this.requirementLinkType = source["requirementLinkType"];
	        this.showCoverage = source["showCoverage"];
	    }
	}

}

export namespace syncer {
	
	export class CreatedTest {
	    tempKey: string;
	    key: string;
	
	    static createFrom(source: any = {}) {
	        return new CreatedTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tempKey = source["tempKey"];
	        this.key = source["key"];
	    }
	}
	export class FailedCommit {
	    testKey: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new FailedCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.error = source["error"];
	    }
	}
	export class ConflictField {
	    pendingId: number;
	    entityType: string;
	    entityKey: string;
	    field: string;
	    label: string;
	    base: string;
	    remote: string;
	    mine: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pendingId = source["pendingId"];
	        this.entityType = source["entityType"];
	        this.entityKey = source["entityKey"];
	        this.field = source["field"];
	        this.label = source["label"];
	        this.base = source["base"];
	        this.remote = source["remote"];
	        this.mine = source["mine"];
	    }
	}
	export class Conflict {
	    testKey: string;
	    testSummary: string;
	    baseVersion: string;
	    remoteVersion: string;
	    remoteDeleted: boolean;
	    fields: ConflictField[];
	
	    static createFrom(source: any = {}) {
	        return new Conflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.testSummary = source["testSummary"];
	        this.baseVersion = source["baseVersion"];
	        this.remoteVersion = source["remoteVersion"];
	        this.remoteDeleted = source["remoteDeleted"];
	        this.fields = this.convertValues(source["fields"], ConflictField);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CommitResult {
	    succeeded: string[];
	    conflicted: Conflict[];
	    failed: FailedCommit[];
	    created: CreatedTest[];
	
	    static createFrom(source: any = {}) {
	        return new CommitResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.succeeded = source["succeeded"];
	        this.conflicted = this.convertValues(source["conflicted"], Conflict);
	        this.failed = this.convertValues(source["failed"], FailedCommit);
	        this.created = this.convertValues(source["created"], CreatedTest);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	

}

export namespace testrepo {
	
	export class AllocateResult {
	    added: string[];
	    alreadyMembers: string[];
	
	    static createFrom(source: any = {}) {
	        return new AllocateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.alreadyMembers = source["alreadyMembers"];
	    }
	}
	export class AuditEntry {
	    id: number;
	    occurredAt: string;
	    actor: string;
	    entityType: string;
	    entityKey: string;
	    action: string;
	    field: string;
	    beforeVal: string;
	    afterVal: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new AuditEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.occurredAt = source["occurredAt"];
	        this.actor = source["actor"];
	        this.entityType = source["entityType"];
	        this.entityKey = source["entityKey"];
	        this.action = source["action"];
	        this.field = source["field"];
	        this.beforeVal = source["beforeVal"];
	        this.afterVal = source["afterVal"];
	        this.note = source["note"];
	    }
	}
	export class Bucket {
	    label: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new Bucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.count = source["count"];
	    }
	}
	export class Bug {
	    key: string;
	    projectKey: string;
	    issueType: string;
	    summary: string;
	    status: string;
	    priority: string;
	    updated: string;
	
	    static createFrom(source: any = {}) {
	        return new Bug(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.projectKey = source["projectKey"];
	        this.issueType = source["issueType"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.updated = source["updated"];
	    }
	}
	export class BugTest {
	    key: string;
	    project: string;
	    summary: string;
	    status: string;
	    runStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new BugTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.project = source["project"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.runStatus = source["runStatus"];
	    }
	}
	export class BugWithTests {
	    key: string;
	    projectKey: string;
	    issueType: string;
	    summary: string;
	    status: string;
	    priority: string;
	    updated: string;
	    testKeys: string[];
	
	    static createFrom(source: any = {}) {
	        return new BugWithTests(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.projectKey = source["projectKey"];
	        this.issueType = source["issueType"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.updated = source["updated"];
	        this.testKeys = source["testKeys"];
	    }
	}
	export class BulkEdit {
	    operation: string;
	    field: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new BulkEdit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operation = source["operation"];
	        this.field = source["field"];
	        this.value = source["value"];
	    }
	}
	export class BulkFailure {
	    testKey: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new BulkFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.error = source["error"];
	    }
	}
	export class BulkEditResult {
	    succeeded: string[];
	    failed: BulkFailure[];
	
	    static createFrom(source: any = {}) {
	        return new BulkEditResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.succeeded = source["succeeded"];
	        this.failed = this.convertValues(source["failed"], BulkFailure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ConflictDecision {
	    pendingId: number;
	    entityType: string;
	    entityKey: string;
	    field: string;
	    choice: string;
	    remoteValue: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictDecision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pendingId = source["pendingId"];
	        this.entityType = source["entityType"];
	        this.entityKey = source["entityKey"];
	        this.field = source["field"];
	        this.choice = source["choice"];
	        this.remoteValue = source["remoteValue"];
	    }
	}
	export class Container {
	    key: string;
	    kind: string;
	    summary: string;
	    status: string;
	    parentKey: string;
	    parentSummary: string;
	    issueType: string;
	    environments: string[];
	    fixVersions: string[];
	    created: string;
	    updated: string;
	    resolved: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new Container(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.kind = source["kind"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.parentKey = source["parentKey"];
	        this.parentSummary = source["parentSummary"];
	        this.issueType = source["issueType"];
	        this.environments = source["environments"];
	        this.fixVersions = source["fixVersions"];
	        this.created = source["created"];
	        this.updated = source["updated"];
	        this.resolved = source["resolved"];
	        this.description = source["description"];
	    }
	}
	export class ContainerMembership {
	    key: string;
	    kind: string;
	    summary: string;
	    status: string;
	    runStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new ContainerMembership(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.kind = source["kind"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.runStatus = source["runStatus"];
	    }
	}
	export class CreateContainerResult {
	    tempKey: string;
	    added: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateContainerResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tempKey = source["tempKey"];
	        this.added = source["added"];
	    }
	}
	export class CustomFieldValue {
	    fieldId: string;
	    name: string;
	    type: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new CustomFieldValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fieldId = source["fieldId"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.value = source["value"];
	    }
	}
	export class DeallocateResult {
	    removed: string[];
	    notMembers: string[];
	
	    static createFrom(source: any = {}) {
	        return new DeallocateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.removed = source["removed"];
	        this.notMembers = source["notMembers"];
	    }
	}
	export class DuplicateMember {
	    key: string;
	    summary: string;
	    description: string;
	    status: string;
	    folderId: string;
	
	    static createFrom(source: any = {}) {
	        return new DuplicateMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.folderId = source["folderId"];
	    }
	}
	export class DuplicateGroup {
	    normalizedSummary: string;
	    displaySummary: string;
	    stepsVerdict: string;
	    members: DuplicateMember[];
	
	    static createFrom(source: any = {}) {
	        return new DuplicateGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.normalizedSummary = source["normalizedSummary"];
	        this.displaySummary = source["displaySummary"];
	        this.stepsVerdict = source["stepsVerdict"];
	        this.members = this.convertValues(source["members"], DuplicateMember);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class DuplicateReport {
	    groups: DuplicateGroup[];
	    groupCount: number;
	    testCount: number;
	    stepsIdentical: number;
	    stepsDiffer: number;
	    stepsUnscanned: number;
	    excluded: number;
	    scannedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new DuplicateReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groups = this.convertValues(source["groups"], DuplicateGroup);
	        this.groupCount = source["groupCount"];
	        this.testCount = source["testCount"];
	        this.stepsIdentical = source["stepsIdentical"];
	        this.stepsDiffer = source["stepsDiffer"];
	        this.stepsUnscanned = source["stepsUnscanned"];
	        this.excluded = source["excluded"];
	        this.scannedAt = source["scannedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExecMemberRun {
	    testKey: string;
	    summary: string;
	    status: string;
	    runStatus: string;
	    startedAt: string;
	    finishedAt: string;
	    executedBy: string;
	    environment: string;
	    fixVersions: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExecMemberRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.runStatus = source["runStatus"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.executedBy = source["executedBy"];
	        this.environment = source["environment"];
	        this.fixVersions = source["fixVersions"];
	    }
	}
	export class Folder {
	    id: string;
	    parentId: string;
	    name: string;
	    xrayId: string;
	    testCount: number;
	    totalTestCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Folder(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.parentId = source["parentId"];
	        this.name = source["name"];
	        this.xrayId = source["xrayId"];
	        this.testCount = source["testCount"];
	        this.totalTestCount = source["totalTestCount"];
	    }
	}
	export class FolderMismatch {
	    summary: string;
	    referenceFolder: string;
	    targetFolder: string;
	
	    static createFrom(source: any = {}) {
	        return new FolderMismatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = source["summary"];
	        this.referenceFolder = source["referenceFolder"];
	        this.targetFolder = source["targetFolder"];
	    }
	}
	export class GapTest {
	    summary: string;
	    description: string;
	    priority: string;
	    labels: string[];
	    components: string[];
	    folder: string;
	
	    static createFrom(source: any = {}) {
	        return new GapTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.priority = source["priority"];
	        this.labels = source["labels"];
	        this.components = source["components"];
	        this.folder = source["folder"];
	    }
	}
	export class GapResult {
	    referenceSource: string;
	    referenceCount: number;
	    targetCount: number;
	    matched: number;
	    missingFromReference: GapTest[];
	    missingFromTarget: GapTest[];
	    threeWay: boolean;
	    projectCount: number;
	    missingFromProject: GapTest[];
	    folderMismatches: FolderMismatch[];
	
	    static createFrom(source: any = {}) {
	        return new GapResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.referenceSource = source["referenceSource"];
	        this.referenceCount = source["referenceCount"];
	        this.targetCount = source["targetCount"];
	        this.matched = source["matched"];
	        this.missingFromReference = this.convertValues(source["missingFromReference"], GapTest);
	        this.missingFromTarget = this.convertValues(source["missingFromTarget"], GapTest);
	        this.threeWay = source["threeWay"];
	        this.projectCount = source["projectCount"];
	        this.missingFromProject = this.convertValues(source["missingFromProject"], GapTest);
	        this.folderMismatches = this.convertValues(source["folderMismatches"], FolderMismatch);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ImportError {
	    row: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.row = source["row"];
	        this.message = source["message"];
	    }
	}
	export class ImportMapping {
	    summary: string;
	    description: string;
	    priority: string;
	    labels: string;
	    components: string;
	    folder: string;
	    action: string;
	    data: string;
	    expected: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportMapping(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.priority = source["priority"];
	        this.labels = source["labels"];
	        this.components = source["components"];
	        this.folder = source["folder"];
	        this.action = source["action"];
	        this.data = source["data"];
	        this.expected = source["expected"];
	    }
	}
	export class ImportPreview {
	    headers: string[];
	    rowCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.headers = source["headers"];
	        this.rowCount = source["rowCount"];
	    }
	}
	export class ImportResult {
	    created: number;
	    skipped: number;
	    errors: ImportError[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = source["created"];
	        this.skipped = source["skipped"];
	        this.errors = this.convertValues(source["errors"], ImportError);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JUnitSkip {
	    testcase: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new JUnitSkip(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testcase = source["testcase"];
	        this.reason = source["reason"];
	    }
	}
	export class JUnitMatch {
	    testcase: string;
	    testKey: string;
	    summary: string;
	    result: string;
	    currentRun: string;
	
	    static createFrom(source: any = {}) {
	        return new JUnitMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testcase = source["testcase"];
	        this.testKey = source["testKey"];
	        this.summary = source["summary"];
	        this.result = source["result"];
	        this.currentRun = source["currentRun"];
	    }
	}
	export class JUnitImportPreview {
	    execKey: string;
	    total: number;
	    matched: JUnitMatch[];
	    skipped: JUnitSkip[];
	
	    static createFrom(source: any = {}) {
	        return new JUnitImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.execKey = source["execKey"];
	        this.total = source["total"];
	        this.matched = this.convertValues(source["matched"], JUnitMatch);
	        this.skipped = this.convertValues(source["skipped"], JUnitSkip);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class JUnitNewExecRow {
	    testcase: string;
	    testKey: string;
	    summary: string;
	    result: string;
	    create: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JUnitNewExecRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testcase = source["testcase"];
	        this.testKey = source["testKey"];
	        this.summary = source["summary"];
	        this.result = source["result"];
	        this.create = source["create"];
	    }
	}
	export class JUnitNewExecPreview {
	    total: number;
	    rows: JUnitNewExecRow[];
	    skipped: JUnitSkip[];
	
	    static createFrom(source: any = {}) {
	        return new JUnitNewExecPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.rows = this.convertValues(source["rows"], JUnitNewExecRow);
	        this.skipped = this.convertValues(source["skipped"], JUnitSkip);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JUnitNewExecResult {
	    execKey: string;
	    created: number;
	    allocated: number;
	    resultsSet: number;
	    failed: string[];
	
	    static createFrom(source: any = {}) {
	        return new JUnitNewExecResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.execKey = source["execKey"];
	        this.created = source["created"];
	        this.allocated = source["allocated"];
	        this.resultsSet = source["resultsSet"];
	        this.failed = source["failed"];
	    }
	}
	
	
	export class TestCase {
	    key: string;
	    id: string;
	    summary: string;
	    description: string;
	    status: string;
	    priority: string;
	    labels: string[];
	    components: string[];
	    updated: string;
	    folderId: string;
	    execType: string;
	    fixVersions: string[];
	
	    static createFrom(source: any = {}) {
	        return new TestCase(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.id = source["id"];
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.labels = source["labels"];
	        this.components = source["components"];
	        this.updated = source["updated"];
	        this.folderId = source["folderId"];
	        this.execType = source["execType"];
	        this.fixVersions = source["fixVersions"];
	    }
	}
	export class Page {
	    tests: TestCase[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new Page(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tests = this.convertValues(source["tests"], TestCase);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PendingChange {
	    id: number;
	    entityType: string;
	    entityKey: string;
	    field: string;
	    beforeVal: string;
	    afterVal: string;
	    baseVersion: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new PendingChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.entityType = source["entityType"];
	        this.entityKey = source["entityKey"];
	        this.field = source["field"];
	        this.beforeVal = source["beforeVal"];
	        this.afterVal = source["afterVal"];
	        this.baseVersion = source["baseVersion"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Precondition {
	    key: string;
	    summary: string;
	    type: string;
	    description: string;
	    condition: string;
	
	    static createFrom(source: any = {}) {
	        return new Precondition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
	        this.type = source["type"];
	        this.description = source["description"];
	        this.condition = source["condition"];
	    }
	}
	export class PreconditionTest {
	    key: string;
	    summary: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new PreconditionTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	    }
	}
	export class PreconditionUsage {
	    key: string;
	    summary: string;
	    type: string;
	    description: string;
	    condition: string;
	    testCount: number;
	
	    static createFrom(source: any = {}) {
	        return new PreconditionUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
	        this.type = source["type"];
	        this.description = source["description"];
	        this.condition = source["condition"];
	        this.testCount = source["testCount"];
	    }
	}
	export class Query {
	    search: string;
	    status: string;
	    folderId: string;
	    containerKey: string;
	    component: string;
	    execType: string;
	    review: string;
	    sortBy: string;
	    desc: boolean;
	    limit: number;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new Query(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.search = source["search"];
	        this.status = source["status"];
	        this.folderId = source["folderId"];
	        this.containerKey = source["containerKey"];
	        this.component = source["component"];
	        this.execType = source["execType"];
	        this.review = source["review"];
	        this.sortBy = source["sortBy"];
	        this.desc = source["desc"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class ReqReqLink {
	    fromKey: string;
	    toKey: string;
	    linkType: string;
	    linkId: string;
	
	    static createFrom(source: any = {}) {
	        return new ReqReqLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fromKey = source["fromKey"];
	        this.toKey = source["toKey"];
	        this.linkType = source["linkType"];
	        this.linkId = source["linkId"];
	    }
	}
	export class Requirement {
	    key: string;
	    projectKey: string;
	    issueType: string;
	    summary: string;
	    status: string;
	    updated: string;
	    priority: string;
	    components: string;
	    fixVersions: string;
	    sprint: string;
	    description: string;
	    epicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new Requirement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.projectKey = source["projectKey"];
	        this.issueType = source["issueType"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.updated = source["updated"];
	        this.priority = source["priority"];
	        this.components = source["components"];
	        this.fixVersions = source["fixVersions"];
	        this.sprint = source["sprint"];
	        this.description = source["description"];
	        this.epicKey = source["epicKey"];
	    }
	}
	export class RequirementCoverage {
	    key: string;
	    projectKey: string;
	    issueType: string;
	    summary: string;
	    status: string;
	    testCount: number;
	    coverage: string;
	    priority: string;
	    components: string;
	    fixVersions: string;
	    sprint: string;
	    description: string;
	    epicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new RequirementCoverage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.projectKey = source["projectKey"];
	        this.issueType = source["issueType"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.testCount = source["testCount"];
	        this.coverage = source["coverage"];
	        this.priority = source["priority"];
	        this.components = source["components"];
	        this.fixVersions = source["fixVersions"];
	        this.sprint = source["sprint"];
	        this.description = source["description"];
	        this.epicKey = source["epicKey"];
	    }
	}
	export class RequirementImportRow {
	    summary: string;
	    description: string;
	    priority: string;
	    components: string;
	    fixVersions: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new RequirementImportRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.priority = source["priority"];
	        this.components = source["components"];
	        this.fixVersions = source["fixVersions"];
	        this.status = source["status"];
	    }
	}
	export class RequirementImportPreview {
	    rows: RequirementImportRow[];
	    newCount: number;
	    existingCount: number;
	
	    static createFrom(source: any = {}) {
	        return new RequirementImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = this.convertValues(source["rows"], RequirementImportRow);
	        this.newCount = source["newCount"];
	        this.existingCount = source["existingCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RequirementImportResult {
	    created: number;
	    skippedExisting: number;
	    errors: ImportError[];
	
	    static createFrom(source: any = {}) {
	        return new RequirementImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = source["created"];
	        this.skippedExisting = source["skippedExisting"];
	        this.errors = this.convertValues(source["errors"], ImportError);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RequirementSource {
	    projectKey: string;
	    issueTypes: string;
	    scopeJql: string;
	
	    static createFrom(source: any = {}) {
	        return new RequirementSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectKey = source["projectKey"];
	        this.issueTypes = source["issueTypes"];
	        this.scopeJql = source["scopeJql"];
	    }
	}
	export class RequirementTest {
	    key: string;
	    summary: string;
	    status: string;
	    runStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new RequirementTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.runStatus = source["runStatus"];
	    }
	}
	export class Review {
	    verdict: string;
	    reviewer: string;
	    note: string;
	    reviewedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Review(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.verdict = source["verdict"];
	        this.reviewer = source["reviewer"];
	        this.note = source["note"];
	        this.reviewedAt = source["reviewedAt"];
	    }
	}
	export class RunRollup {
	    passed: number;
	    failed: number;
	    notRun: number;
	    executing: number;
	    aborted: number;
	    blocked: number;
	    total: number;
	    execCount: number;
	
	    static createFrom(source: any = {}) {
	        return new RunRollup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.passed = source["passed"];
	        this.failed = source["failed"];
	        this.notRun = source["notRun"];
	        this.executing = source["executing"];
	        this.aborted = source["aborted"];
	        this.blocked = source["blocked"];
	        this.total = source["total"];
	        this.execCount = source["execCount"];
	    }
	}
	export class SankeyLink {
	    source: string;
	    target: string;
	    value: number;
	
	    static createFrom(source: any = {}) {
	        return new SankeyLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.target = source["target"];
	        this.value = source["value"];
	    }
	}
	export class SankeyNode {
	    id: string;
	    label: string;
	    layer: number;
	    value: number;
	
	    static createFrom(source: any = {}) {
	        return new SankeyNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.layer = source["layer"];
	        this.value = source["value"];
	    }
	}
	export class Sankey {
	    nodes: SankeyNode[];
	    links: SankeyLink[];
	
	    static createFrom(source: any = {}) {
	        return new Sankey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodes = this.convertValues(source["nodes"], SankeyNode);
	        this.links = this.convertValues(source["links"], SankeyLink);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class SavedView {
	    id: string;
	    name: string;
	    query: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SavedView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.query = source["query"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class SeedResult {
	    sets: number;
	    plans: number;
	    executions: number;
	    linked: number;
	
	    static createFrom(source: any = {}) {
	        return new SeedResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sets = source["sets"];
	        this.plans = source["plans"];
	        this.executions = source["executions"];
	        this.linked = source["linked"];
	    }
	}
	export class Statistics {
	    total: number;
	    pendingChanges: number;
	    executedTests: number;
	    testSets: number;
	    testPlans: number;
	    testExecutions: number;
	    testsInSet: number;
	    testsInPlan: number;
	    byStatus: Bucket[];
	    byPriority: Bucket[];
	    byLabel: Bucket[];
	    byFolder: Bucket[];
	    byComponent: Bucket[];
	    updatedTrend: Bucket[];
	    byRunStatus: Bucket[];
	    byCoverage: Bucket[];
	    byRequirement: Bucket[];
	
	    static createFrom(source: any = {}) {
	        return new Statistics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.pendingChanges = source["pendingChanges"];
	        this.executedTests = source["executedTests"];
	        this.testSets = source["testSets"];
	        this.testPlans = source["testPlans"];
	        this.testExecutions = source["testExecutions"];
	        this.testsInSet = source["testsInSet"];
	        this.testsInPlan = source["testsInPlan"];
	        this.byStatus = this.convertValues(source["byStatus"], Bucket);
	        this.byPriority = this.convertValues(source["byPriority"], Bucket);
	        this.byLabel = this.convertValues(source["byLabel"], Bucket);
	        this.byFolder = this.convertValues(source["byFolder"], Bucket);
	        this.byComponent = this.convertValues(source["byComponent"], Bucket);
	        this.updatedTrend = this.convertValues(source["updatedTrend"], Bucket);
	        this.byRunStatus = this.convertValues(source["byRunStatus"], Bucket);
	        this.byCoverage = this.convertValues(source["byCoverage"], Bucket);
	        this.byRequirement = this.convertValues(source["byRequirement"], Bucket);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Step {
	    xrayId: string;
	    index: number;
	    action: string;
	    data: string;
	    expected: string;
	    calledTestKey: string;
	
	    static createFrom(source: any = {}) {
	        return new Step(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.xrayId = source["xrayId"];
	        this.index = source["index"];
	        this.action = source["action"];
	        this.data = source["data"];
	        this.expected = source["expected"];
	        this.calledTestKey = source["calledTestKey"];
	    }
	}
	export class StepDraft {
	    action: string;
	    data: string;
	    expected: string;
	
	    static createFrom(source: any = {}) {
	        return new StepDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.data = source["data"];
	        this.expected = source["expected"];
	    }
	}
	export class SyncLogEntry {
	    id: number;
	    startedAt: string;
	    finishedAt: string;
	    outcome: string;
	    fetched: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncLogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.outcome = source["outcome"];
	        this.fetched = source["fetched"];
	        this.error = source["error"];
	    }
	}
	export class SyncState {
	    profileId: string;
	    lastSyncedAt: string;
	    testCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SyncState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.lastSyncedAt = source["lastSyncedAt"];
	        this.testCount = source["testCount"];
	    }
	}
	export class TestBug {
	    key: string;
	    projectKey: string;
	    summary: string;
	    status: string;
	    priority: string;
	
	    static createFrom(source: any = {}) {
	        return new TestBug(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.projectKey = source["projectKey"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	    }
	}
	export class TestCallLink {
	    callerKey: string;
	    callerSummary: string;
	    calledKey: string;
	    calledSummary: string;
	    calledExists: boolean;
	    stepIndex: number;
	
	    static createFrom(source: any = {}) {
	        return new TestCallLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.callerKey = source["callerKey"];
	        this.callerSummary = source["callerSummary"];
	        this.calledKey = source["calledKey"];
	        this.calledSummary = source["calledSummary"];
	        this.calledExists = source["calledExists"];
	        this.stepIndex = source["stepIndex"];
	    }
	}
	
	export class TestDraft {
	    summary: string;
	    description: string;
	    priority: string;
	    labels: string;
	    components: string;
	    folderId: string;
	    steps: StepDraft[];
	    precondKeys: string[];
	
	    static createFrom(source: any = {}) {
	        return new TestDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.priority = source["priority"];
	        this.labels = source["labels"];
	        this.components = source["components"];
	        this.folderId = source["folderId"];
	        this.steps = this.convertValues(source["steps"], StepDraft);
	        this.precondKeys = source["precondKeys"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TestPlanBoardRow {
	    testKey: string;
	    summary: string;
	    status: string;
	    runStatus: string;
	    isExternal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TestPlanBoardRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.runStatus = source["runStatus"];
	        this.isExternal = source["isExternal"];
	    }
	}
	export class TestPlanBoard {
	    key: string;
	    summary: string;
	    description: string;
	    rows: TestPlanBoardRow[];
	    runCounts: Bucket[];
	
	    static createFrom(source: any = {}) {
	        return new TestPlanBoard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.rows = this.convertValues(source["rows"], TestPlanBoardRow);
	        this.runCounts = this.convertValues(source["runCounts"], Bucket);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TestRunEntry {
	    execKey: string;
	    execSummary: string;
	    planKeys: string[];
	    environment: string;
	    fixVersions: string[];
	    runStatus: string;
	    startedAt: string;
	    finishedAt: string;
	    executedBy: string;
	    defects: string[];
	    createdAt: string;
	    updatedAt: string;
	    execIssueType: string;
	    execParentKey: string;
	    execParentSummary: string;
	    execCreated: string;
	    execUpdated: string;
	    execResolved: string;
	
	    static createFrom(source: any = {}) {
	        return new TestRunEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.execKey = source["execKey"];
	        this.execSummary = source["execSummary"];
	        this.planKeys = source["planKeys"];
	        this.environment = source["environment"];
	        this.fixVersions = source["fixVersions"];
	        this.runStatus = source["runStatus"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.executedBy = source["executedBy"];
	        this.defects = source["defects"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.execIssueType = source["execIssueType"];
	        this.execParentKey = source["execParentKey"];
	        this.execParentSummary = source["execParentSummary"];
	        this.execCreated = source["execCreated"];
	        this.execUpdated = source["execUpdated"];
	        this.execResolved = source["execResolved"];
	    }
	}

}

