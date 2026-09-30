// Fixtures and defaults for the App tests. The backend and the Wails runtime are
// mocked once in ./test-setup, which vitest loads before any test file, so this
// module only has to import the mocked ./api and hand it back to tests.
import {vi} from 'vitest';

import * as api from './api';
import type {
    ExplainView,
    GitRefresh,
    RootView,
    ScriptView,
    Status,
    Toolchain,
    WorkspaceState,
} from './types';

export {api};

// A promise the test resolves by hand, which is how a response that arrives
// later than a newer one is expressed.
export function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return {promise, resolve, reject};
}

export function workspaceOf(...paths: string[]): WorkspaceState {
    return {roots: paths.map((path) => ({path, name: path.split('/').pop() ?? path}))};
}

// The fixtures below fill in every required field rather than casting a partial
// object, so a field added to a view type breaks these instead of quietly
// becoming undefined and throwing somewhere inside the component.
export function scriptIn(root: string, rel: string): ScriptView {
    return {
        path: `${root}/${rel}`,
        root,
        rel,
        name: rel.split('/').pop() ?? rel,
        dir: root,
        kind: 'script',
        language: 'powershell',
        lang: 'PowerShell',
        size: 128,
        modTime: '2026-01-01T00:00:00Z',
        metadata: {path: `${root}/${rel}`, help: {source: 'none'}},
        cached: true,
        inferred: false,
    };
}

export function rootView(root: string, rels: string[]): RootView {
    return {
        root,
        name: root.split('/').pop() ?? root,
        source: 'folder',
        isRepo: false,
        scripts: rels.map((rel) => scriptIn(root, rel)),
        meta: {
            durationMs: 1,
            fromCache: rels.length,
            read: 0,
            total: rels.length,
            cacheHits: rels.length,
            cacheMisses: 0,
        },
    };
}

export function explainView(script: ScriptView): ExplainView {
    return {script, docs: {help: {source: 'none'}, recovered: false, documents: []}};
}

const idleStatus: Status = {configDir: '/tmp', roots: 0, cachedReports: 0};

// idle answers every call the component makes while it mounts, so a test only
// has to say which of them it cares about.
export function idleApi(roots: string[] = []) {
    vi.mocked(api.workspace).mockResolvedValue(workspaceOf(...roots));
    vi.mocked(api.status).mockResolvedValue(idleStatus);
    vi.mocked(api.toolchains).mockResolvedValue([] as Toolchain[]);
    vi.mocked(api.detectEditors).mockResolvedValue([]);
    vi.mocked(api.explain).mockResolvedValue(explainView(scriptIn('', '')));
    vi.mocked(api.invalidate).mockImplementation(async (root, rel) => scriptIn(root, rel));
    // status is absent, not null, for a root that is not a repository.
    vi.mocked(api.refreshGit).mockResolvedValue({modified: []} satisfies GitRefresh);
}
