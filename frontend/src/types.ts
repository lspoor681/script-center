// The shapes the Go side actually sends, spelled with the JSON field names.
//
// Wails' generated models.ts names its fields after the Go identifiers, which for
// the docs package differ in case from the JSON tags on the wire. Reading those
// generated classes is therefore a trap: they type-check against a field name
// the payload does not have, and the result is a silent undefined. These types
// describe the bytes, so a mistake here is a compile error rather than a blank
// panel.

export type Value = {
    source: string;
    display?: string;
    isExpression?: boolean;
};

export type Constraint = {
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
};

export type Param = {
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
};

export type Example = {
    title?: string;
    command: string;
    remarks?: string;
};

export type Help = {
    source: string;
    synopsis?: string;
    description?: string;
    notes?: string;
    links?: string[];
    examples?: Example[];
    inputTypes?: string;
    outputTypes?: string;
    params?: Record<string, string>;
};

export type ModuleRequirement = {
    module: string;
    version?: string;
    optional?: boolean;
    reason?: string;
};

export type Requirements = {
    modules?: ModuleRequirement[];
    source?: string;
    pythonVersion?: string;
    errors?: string[];
};

export type Report = {
    path: string;
    help: Help;
    params?: Param[];
    requirements?: Requirements;
    warnings?: string[];
    durationMS?: number;
    cached?: boolean;
    version?: number;
};

export type Document = {
    path: string;
    rel: string;
    kind: string;
    name: string;
    title?: string;
    section?: string;
    distance: number;
    bytes: number;
    truncated?: boolean;
};

export type Explanation = {
    help: Help;
    recovered: boolean;
    documents: Document[];
    primary?: Document;
};

export type Suggestion = {
    path: string;
    root: string;
    rel: string;
    name: string;
    reason: string;
};

export type Meta = {
    durationMs: number;
    fromCache: number;
    read: number;
    total: number;
    cacheHits: number;
    cacheMisses: number;
};

export type ScriptView = {
    path: string;
    root: string;
    rel: string;
    name: string;
    dir: string;
    kind: string;
    lang: string;
    size: number;
    modTime: string;
    metadata: Report;
    cached: boolean;
    inferred: boolean;
};

export type RootView = {
    root: string;
    name: string;
    source: string;
    isRepo: boolean;
    scripts: ScriptView[];
    warnings?: string[];
    meta: Meta;
};

export type ExplainView = {
    script: ScriptView;
    docs: Explanation;
    related?: Suggestion[];
    dependencies?: string[];
};

export type Toolchain = {
    language: string;
    available: boolean;
    reason?: string;
};

export type Status = {
    configDir: string;
    roots: number;
    cachedReports: number;
    warnings?: string[];
};

export type Root = {path: string; name: string};
export type Ref = {root: string; rel: string};

// Workspace is bound as an untyped value because it crosses as a pointer to a
// struct that is not one of the app package's named view types.
export type WorkspaceState = {
    version?: number;
    roots: Root[];
    favorites?: Ref[];
};

// A documentation file with its content already rendered and sanitized. The
// backend renders it because a readme is untrusted input and the sanitizer
// belongs in one place; the frontend only ever displays what survived.
export type DocumentView = {
    path: string;
    kind: string;
    title: string;
    html: string;
    error?: string;
};
