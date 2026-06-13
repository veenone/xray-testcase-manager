export namespace jira {
	
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
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.defaultProfileId = source["defaultProfileId"];
	        this.theme = source["theme"];
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
	
	    static createFrom(source: any = {}) {
	        return new Container(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.kind = source["kind"];
	        this.summary = source["summary"];
	        this.status = source["status"];
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
	    status: string;
	    folderId: string;
	
	    static createFrom(source: any = {}) {
	        return new DuplicateMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
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
	
	    static createFrom(source: any = {}) {
	        return new Precondition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
	        this.type = source["type"];
	        this.description = source["description"];
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
	        this.testCount = source["testCount"];
	    }
	}
	export class Query {
	    search: string;
	    status: string;
	    folderId: string;
	    containerKey: string;
	    component: string;
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
	        this.review = source["review"];
	        this.sortBy = source["sortBy"];
	        this.desc = source["desc"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class Requirement {
	    key: string;
	    projectKey: string;
	    issueType: string;
	    summary: string;
	    status: string;
	    updated: string;
	
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
	
	    static createFrom(source: any = {}) {
	        return new TestPlanBoardRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.testKey = source["testKey"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.runStatus = source["runStatus"];
	    }
	}
	export class TestPlanBoard {
	    key: string;
	    summary: string;
	    rows: TestPlanBoardRow[];
	    runCounts: Bucket[];
	
	    static createFrom(source: any = {}) {
	        return new TestPlanBoard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.summary = source["summary"];
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

}

