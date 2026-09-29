export namespace app {
	
	export class DocumentView {
	    path: string;
	    kind: string;
	    title: string;
	    html: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DocumentView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.html = source["html"];
	        this.error = source["error"];
	    }
	}
	export class Suggestion {
	    path: string;
	    root: string;
	    rel: string;
	    name: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new Suggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.root = source["root"];
	        this.rel = source["rel"];
	        this.name = source["name"];
	        this.reason = source["reason"];
	    }
	}
	export class ScriptView {
	    path: string;
	    root: string;
	    rel: string;
	    name: string;
	    dir: string;
	    kind: string;
	    language: string;
	    lang: string;
	    size: number;
	    modTime: string;
	    metadata: params.Report;
	    cached: boolean;
	    inferred: boolean;
	    modified?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ScriptView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.root = source["root"];
	        this.rel = source["rel"];
	        this.name = source["name"];
	        this.dir = source["dir"];
	        this.kind = source["kind"];
	        this.language = source["language"];
	        this.lang = source["lang"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	        this.metadata = this.convertValues(source["metadata"], params.Report);
	        this.cached = source["cached"];
	        this.inferred = source["inferred"];
	        this.modified = source["modified"];
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
	export class ExplainView {
	    script: ScriptView;
	    docs: docs.Explanation;
	    related?: Suggestion[];
	    dependencies?: string[];
	    unresolved?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExplainView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.script = this.convertValues(source["script"], ScriptView);
	        this.docs = this.convertValues(source["docs"], docs.Explanation);
	        this.related = this.convertValues(source["related"], Suggestion);
	        this.dependencies = source["dependencies"];
	        this.unresolved = source["unresolved"];
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
	export class GitCommit {
	    oid: string;
	    shortOid: string;
	    author: string;
	    // Go type: time
	    date: any;
	    subject: string;
	
	    static createFrom(source: any = {}) {
	        return new GitCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.oid = source["oid"];
	        this.shortOid = source["shortOid"];
	        this.author = source["author"];
	        this.date = this.convertValues(source["date"], null);
	        this.subject = source["subject"];
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
	export class GitStatus {
	    branch: string;
	    detached: boolean;
	    oid: string;
	    upstream?: string;
	    ahead: number;
	    behind: number;
	    staged: number;
	    unstaged: number;
	    untracked: number;
	    conflicted: number;
	    stashed: number;
	    commit?: GitCommit;
	    remote?: string;
	
	    static createFrom(source: any = {}) {
	        return new GitStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.detached = source["detached"];
	        this.oid = source["oid"];
	        this.upstream = source["upstream"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.staged = source["staged"];
	        this.unstaged = source["unstaged"];
	        this.untracked = source["untracked"];
	        this.conflicted = source["conflicted"];
	        this.stashed = source["stashed"];
	        this.commit = this.convertValues(source["commit"], GitCommit);
	        this.remote = source["remote"];
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
	export class GitRefresh {
	    status?: GitStatus;
	    modified?: string[];
	
	    static createFrom(source: any = {}) {
	        return new GitRefresh(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = this.convertValues(source["status"], GitStatus);
	        this.modified = source["modified"];
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
	
	export class Meta {
	    durationMs: number;
	    fromCache: number;
	    read: number;
	    total: number;
	    cacheHits: number;
	    cacheMisses: number;
	
	    static createFrom(source: any = {}) {
	        return new Meta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.durationMs = source["durationMs"];
	        this.fromCache = source["fromCache"];
	        this.read = source["read"];
	        this.total = source["total"];
	        this.cacheHits = source["cacheHits"];
	        this.cacheMisses = source["cacheMisses"];
	    }
	}
	export class RootView {
	    root: string;
	    name: string;
	    source: string;
	    isRepo: boolean;
	    scripts: ScriptView[];
	    warnings?: string[];
	    git?: GitStatus;
	    meta: Meta;
	
	    static createFrom(source: any = {}) {
	        return new RootView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.name = source["name"];
	        this.source = source["source"];
	        this.isRepo = source["isRepo"];
	        this.scripts = this.convertValues(source["scripts"], ScriptView);
	        this.warnings = source["warnings"];
	        this.git = this.convertValues(source["git"], GitStatus);
	        this.meta = this.convertValues(source["meta"], Meta);
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
	export class RunView {
	    id: string;
	    command: string[];
	    dir: string;
	    warning?: string;
	    blind?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RunView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.command = source["command"];
	        this.dir = source["dir"];
	        this.warning = source["warning"];
	        this.blind = source["blind"];
	    }
	}
	
	export class Status {
	    configDir: string;
	    roots: number;
	    cachedReports: number;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configDir = source["configDir"];
	        this.roots = source["roots"];
	        this.cachedReports = source["cachedReports"];
	        this.warnings = source["warnings"];
	    }
	}
	
	export class Toolchain {
	    language: string;
	    available: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new Toolchain(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.available = source["available"];
	        this.reason = source["reason"];
	    }
	}

}

export namespace docs {
	
	export class Document {
	    path: string;
	    kind: string;
	    title: string;
	    section?: string;
	    excerpted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Document(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.section = source["section"];
	        this.excerpted = source["excerpted"];
	    }
	}
	export class Explanation {
	    help: params.Help;
	    recovered: boolean;
	    documents: Document[];
	    primary?: Document;
	
	    static createFrom(source: any = {}) {
	        return new Explanation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.help = this.convertValues(source["help"], params.Help);
	        this.recovered = source["recovered"];
	        this.documents = this.convertValues(source["documents"], Document);
	        this.primary = this.convertValues(source["primary"], Document);
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

export namespace params {
	
	export class Constraint {
	    kind: string;
	    label?: string;
	    values?: string[];
	    min?: number;
	    max?: number;
	    hasMin?: boolean;
	    hasMax?: boolean;
	    includeMin?: boolean;
	    includeMax?: boolean;
	    pattern?: string;
	
	    static createFrom(source: any = {}) {
	        return new Constraint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.values = source["values"];
	        this.min = source["min"];
	        this.max = source["max"];
	        this.hasMin = source["hasMin"];
	        this.hasMax = source["hasMax"];
	        this.includeMin = source["includeMin"];
	        this.includeMax = source["includeMax"];
	        this.pattern = source["pattern"];
	    }
	}
	export class Example {
	    title?: string;
	    command: string;
	    remarks?: string;
	
	    static createFrom(source: any = {}) {
	        return new Example(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.command = source["command"];
	        this.remarks = source["remarks"];
	    }
	}
	export class Help {
	    source: string;
	    synopsis?: string;
	    description?: string;
	    notes?: string;
	    links?: string[];
	    examples?: Example[];
	    inputTypes?: string;
	    outputTypes?: string;
	    params?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Help(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.synopsis = source["synopsis"];
	        this.description = source["description"];
	        this.notes = source["notes"];
	        this.links = source["links"];
	        this.examples = this.convertValues(source["examples"], Example);
	        this.inputTypes = source["inputTypes"];
	        this.outputTypes = source["outputTypes"];
	        this.params = source["params"];
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
	export class ModuleRequirement {
	    name: string;
	    version?: string;
	
	    static createFrom(source: any = {}) {
	        return new ModuleRequirement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	    }
	}
	export class Value {
	    source: string;
	    display?: string;
	    isExpression?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Value(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.display = source["display"];
	        this.isExpression = source["isExpression"];
	    }
	}
	export class Param {
	    name: string;
	    aliases?: string[];
	    kind: string;
	    typeName?: string;
	    required: boolean;
	    position?: number;
	    fromPipeline?: boolean;
	    default?: Value;
	    help?: string;
	    constraints?: Constraint[];
	
	    static createFrom(source: any = {}) {
	        return new Param(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.aliases = source["aliases"];
	        this.kind = source["kind"];
	        this.typeName = source["typeName"];
	        this.required = source["required"];
	        this.position = source["position"];
	        this.fromPipeline = source["fromPipeline"];
	        this.default = this.convertValues(source["default"], Value);
	        this.help = source["help"];
	        this.constraints = this.convertValues(source["constraints"], Constraint);
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
	export class Requirements {
	    modules?: ModuleRequirement[];
	    runAsAdministrator: boolean;
	    psVersion?: string;
	    psEditions?: string[];
	    pythonVersion?: string;
	
	    static createFrom(source: any = {}) {
	        return new Requirements(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modules = this.convertValues(source["modules"], ModuleRequirement);
	        this.runAsAdministrator = source["runAsAdministrator"];
	        this.psVersion = source["psVersion"];
	        this.psEditions = source["psEditions"];
	        this.pythonVersion = source["pythonVersion"];
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
	export class Report {
	    path: string;
	    params: Param[];
	    help: Help;
	    requirements: Requirements;
	    dotSources?: string[];
	    warnings?: string[];
	    unparsedHelpBlock?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.params = this.convertValues(source["params"], Param);
	        this.help = this.convertValues(source["help"], Help);
	        this.requirements = this.convertValues(source["requirements"], Requirements);
	        this.dotSources = source["dotSources"];
	        this.warnings = source["warnings"];
	        this.unparsedHelpBlock = source["unparsedHelpBlock"];
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

