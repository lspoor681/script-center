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
    name: string;
    version?: string;
};

export type Requirements = {
    modules?: ModuleRequirement[];
    runAsAdministrator?: boolean;
    psVersion?: string;
    psEditions?: string[];
    pythonVersion?: string;
};

export type Report = {
    path: string;
    help: Help;
    params?: Param[];
    requirements?: Requirements;
    dotSources?: string[];
    warnings?: string[];
    unparsedHelpBlock?: boolean;
};

export type Document = {
    path: string;
    kind: string;
    title: string;
    section?: string;
    excerpted: boolean;
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
    // language is the language's own name, such as "powershell". lang is its
    // display name. The backend needs the first to re-read this script, which is
    // why it is sent rather than recovered from the second.
    language: string;
    lang: string;
    size: number;
    modTime: string;
    metadata: Report;
    cached: boolean;
    inferred: boolean;
    // modified is true when git reports a change to this script. It is only
    // ever set for a script in a repository.
    modified?: boolean;
};

// GitCommit is the most recent commit on a root's branch, as the strip shows it.
export type GitCommit = {
    oid: string;
    shortOid: string;
    author: string;
    date: string;
    subject: string;
};

// GitStatus is the repository state of a root, when the root is a repository.
export type GitStatus = {
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
};

// RunView is the start of a script run, returned from RunScript so the panel
// can attach the streaming events to it by id.
export type RunView = {
    id: string;
    command: string[];
    dir: string;
    warning?: string;
};

export type RootView = {
    root: string;
    name: string;
    source: string;
    isRepo: boolean;
    scripts: ScriptView[];
    warnings?: string[];
    meta: Meta;
    // git is the repository state of the root, when the root is a repository.
    git?: GitStatus;
};

export type ExplainView = {
    script: ScriptView;
    docs: Explanation;
    related?: Suggestion[];
    // dependencies are the files this script dot-sources, relative to the root.
    dependencies?: string[];
    // unresolved are dot-source expressions that could not be resolved to a file.
    unresolved?: string[];
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
