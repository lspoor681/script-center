import {useCallback, useEffect, useMemo, useState} from 'react';
import * as api from './api';
import * as types from './types';
import './App.css';

// The workspace crosses as a pointer to a struct rather than a named view type,
// so it is normalized here into a shape the rest of the component can rely on
// being non-null.
function asWorkspace(ws: types.WorkspaceState | null): types.WorkspaceState {
    return {roots: ws?.roots ?? [], favorites: ws?.favorites ?? []};
}

const NO_ROOTS: types.Root[] = [];

function App() {
    const [roots, setRoots] = useState<types.Root[]>(NO_ROOTS);
    const [selected, setSelected] = useState<string>('');
    const [view, setView] = useState<types.RootView | null>(null);
    const [picked, setPicked] = useState<types.ScriptView | null>(null);
    const [detail, setDetail] = useState<types.ExplainView | null>(null);
    const [tools, setTools] = useState<types.Toolchain[]>([]);
    const [status, setStatus] = useState<types.Status | null>(null);
    const [busy, setBusy] = useState<string>('');
    const [error, setError] = useState<string>('');
    // The document being read, and which of the script's documents is chosen.
    // A readme is not fetched with the script because it can be far larger
    // than the metadata, and because a user often wants the form, not the
    // prose.
    const [doc, setDoc] = useState<types.DocumentView | null>(null);
    const [docPath, setDocPath] = useState<string>('');

    // Reading a root is the slow thing this app does, so every failure is shown
    // rather than swallowed and leaving the user with an empty window.
    const fail = useCallback((where: string, err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        setError(`${where}: ${message}`);
    }, []);

    const refreshWorkspace = useCallback(async () => {
        try {
            const ws = asWorkspace(await api.workspace());
            setRoots(ws.roots);
            setSelected((current) => current || ws.roots[0]?.path || '');
        } catch (err) {
            fail('reading the workspace', err);
        }
    }, [fail]);

    // The status bar reports how many reports the cache holds. That number moves
    // every time a root is read, so it is refreshed wherever the cache changes
    // rather than left showing whatever was true at startup.
    const refreshStatus = useCallback(
        () => api.status().then(setStatus).catch(() => undefined),
        [],
    );

    useEffect(() => {
        void refreshWorkspace();
        void refreshStatus();
        void api.toolchains().then(setTools).catch(() => undefined);
    }, [refreshWorkspace, refreshStatus]);

    // Opening the selected root is where the metadata arrives.
    useEffect(() => {
        if (!selected) {
            setView(null);
            return;
        }
        setBusy(`Reading ${selected}…`);
        api.openRoot(selected)
            .then((next) => {
                setView(next);
                setError('');
            })
            .catch((err) => fail('reading the root', err))
            .finally(() => {
                setBusy('');
                void refreshStatus();
            });
    }, [selected, fail, refreshStatus]);

    // Opening a script is a second step: the list is cheap to show, the
    // documentation is not, so it is only fetched when a script is chosen.
    const open = useCallback((script: types.ScriptView) => {
        setPicked(script);
        setDetail(null);
        setDoc(null);
        setDocPath('');
        api.explain(script.root, script.rel)
            .then(setDetail)
            .catch((err) => fail('opening the script', err));
    }, [fail]);

    // Reading a document is a separate call because the metadata and the prose
    // are different sizes, and a large readme should not be paid for by someone
    // who only wants the parameter form.
    const showDocument = useCallback(
        async (root: string, rel: string, path: string) => {
            setDocPath(path);
            setDoc(null);
            try {
                setDoc(await api.renderDocument(root, rel, path));
            } catch (err) {
                fail('reading the document', err);
            }
        },
        [fail],
    );

    // When a script is opened, its most specific document is read for it.
    useEffect(() => {
        const primary = detail?.docs?.primary;
        const root = detail?.script?.root;
        const rel = detail?.script?.rel;
        if (!primary || !root || !rel) {
            setDoc(null);
            setDocPath('');
            return;
        }
        void showDocument(root, rel, primary.path);
    }, [detail, showDocument]);

    const addRoot = useCallback(async () => {
        setBusy('Choosing a folder…');
        try {
            const ws = asWorkspace(await api.chooseRoot());
            if (ws.roots.length > 0) {
                setRoots(ws.roots);
                setSelected(ws.roots[ws.roots.length - 1].path);
            }
        } catch (err) {
            fail('adding a folder', err);
        } finally {
            setBusy('');
        }
    }, [fail]);

    const removeRoot = useCallback(async (path: string) => {
        try {
            const ws = asWorkspace(await api.removeRoot(path));
            setRoots(ws.roots);
            setSelected(ws.roots[0]?.path ?? '');
            setView(null);
            void refreshStatus();
        } catch (err) {
            fail('removing a folder', err);
        }
    }, [fail, refreshStatus]);

    // A re-read exists because the cache trusts a file's size and timestamp, and
    // a user who has just edited a script needs a way to be certain.
    const reread = useCallback(async (script: types.ScriptView) => {
        setBusy('Re-reading…');
        try {
            const fresh = await api.invalidate(script.root, script.rel);
            setView((current) => current && {
                ...current,
                scripts: current.scripts.map((s) => (s.rel === fresh.rel ? fresh : s)),
                meta: {
                    ...current.meta,
                    fromCache: Math.max(0, current.meta.fromCache - 1),
                    read: current.meta.read + 1,
                },
            });
            if (picked?.rel === fresh.rel) {
                setPicked(fresh);
                api.explain(fresh.root, fresh.rel).then(setDetail).catch(() => undefined);
            }
        } catch (err) {
            fail('re-reading the script', err);
        } finally {
            setBusy('');
            void refreshStatus();
        }
    }, [fail, picked, refreshStatus]);

    const star = useCallback(async (script: types.ScriptView) => {
        try {
            const ws = asWorkspace(await api.toggleFavorite(script.root, script.rel));
            setRoots(ws.roots);
        } catch (err) {
            fail('starring the script', err);
        }
    }, [fail]);

    const missing = useMemo(
        () => tools.filter((tool) => !tool.available),
        [tools],
    );

    // Hoisted so the markup can branch on the count without repeating the
    // optional chain, which would leave TypeScript unable to narrow it.
    const detailParams = picked?.metadata.params ?? [];

    return (
        <div className="app">
            <aside className="sidebar">
                <div className="sidebar-head">
                    <h1>Script Center</h1>
                    <button className="primary" onClick={addRoot}>Add folder</button>
                </div>

                {roots.length === 0 ? (
                    <p className="empty">
                        Add a folder to get started. Script Center looks for scripts in it
                        and works out what each one accepts.
                    </p>
                ) : (
                    <ul className="roots">
                        {roots.map((root) => (
                            <li key={root.path}>
                                <button
                                    className={root.path === selected ? 'root current' : 'root'}
                                    onClick={() => setSelected(root.path)}
                                    title={root.path}
                                >
                                    {root.name}
                                </button>
                                <button
                                    className="ghost"
                                    title="Remove this folder"
                                    onClick={() => void removeRoot(root.path)}
                                >
                                    ×
                                </button>
                            </li>
                        ))}
                    </ul>
                )}

                {status && (
                    <p className="status">
                        {status.cachedReports} cached
                        {status.configDir && <span title={status.configDir}> · {status.configDir}</span>}
                    </p>
                )}

                {missing.length > 0 && (
                    <div className="toolchains">
                        <h2>No parameter forms for</h2>
                        <ul>
                            {missing.map((tool) => (
                                <li key={tool.language} title={tool.reason}>
                                    {tool.language}
                                </li>
                            ))}
                        </ul>
                    </div>
                )}
            </aside>

            <main className="list">
                {busy && <p className="busy">{busy}</p>}
                {error && <p className="error">{error}</p>}

                {view?.warnings?.map((warning) => (
                    <p className="warning" key={warning}>{warning}</p>
                ))}

                {view && (
                    <p className="meta">
                        {view.meta.total} scripts · {view.meta.read} read · {view.meta.fromCache} from cache
                        {' · '}{view.meta.durationMs}ms
                    </p>
                )}

                <ul className="scripts">
                    {(view?.scripts ?? []).map((script) => {
                        const count = script.metadata.params?.length ?? 0;
                        return (
                        <li key={script.rel}>
                            <button
                                className={script.rel === picked?.rel ? 'script current' : 'script'}
                                onClick={() => open(script)}
                            >
                                <span className="name">{script.name}</span>
                                <span className="where">{script.dir || '.'}</span>
                                <span className="badges">
                                    <span className="badge lang">{script.lang}</span>
                                    {count > 0 && (
                                        <span className="badge params">{count} params</span>
                                    )}
                                    {script.cached && <span className="badge cached">cached</span>}
                                    {!script.inferred && count === 0 && (
                                        <span className="badge none">no form</span>
                                    )}
                                </span>
                            </button>
                            <button
                                className="ghost"
                                title="Read this script again"
                                onClick={() => void reread(script)}
                            >
                                ↻
                            </button>
                        </li>
                        );
                    })}
                </ul>
            </main>

            <section className="detail">
                {!picked && <p className="empty">Choose a script to see what it accepts.</p>}
                {picked && (
                    <>
                        <header>
                            <h2>{picked.name}</h2>
                            <p className="path">{picked.path}</p>
                            <button className="ghost" onClick={() => void star(picked)}>
                                ☆ Star
                            </button>
                        </header>

                        {picked.metadata.warnings?.map((warning) => (
                            <p className="warning" key={warning}>{warning}</p>
                        ))}

                        {picked.metadata.help?.synopsis && (
                            <p className="synopsis">{picked.metadata.help.synopsis}</p>
                        )}

                        {detailParams.length > 0 ? (
                            <table className="params">
                                <thead>
                                <tr>
                                    <th>Parameter</th>
                                    <th>Type</th>
                                    <th>Default</th>
                                    <th>Notes</th>
                                </tr>
                                </thead>
                                <tbody>
                                {detailParams.map((param) => (
                                    <tr key={param.name}>
                                        <td>
                                            <code>{param.name}</code>
                                            {(param.aliases?.join(', ')) && (
                                                <span className="aliases"> {param.aliases?.join(', ')}</span>
                                            )}
                                            {param.required && <span className="required">required</span>}
                                        </td>
                                        <td className="type">{param.typeName || param.kind}</td>
                                        <td className="default">{formatValue(param.default)}</td>
                                        <td className="help">
                                            {param.help}
                                            {param.constraints?.map((c, i) => (
                                                <span className="constraint" key={`${c.kind}-${i}`}>
                                                    {formatConstraint(c)}
                                                </span>
                                            ))}
                                        </td>
                                    </tr>
                                ))}
                                </tbody>
                            </table>
                        ) : (
                            <p className="empty">
                                This script has no parameter form. It will still run; you would
                                supply its arguments yourself.
                            </p>
                        )}

                        {detail?.docs?.help?.description && (
                            <p className="description">{detail.docs.help.description}</p>
                        )}

                        {detail?.docs?.help?.examples?.map((example, i) => (
                            <div className="example" key={i}>
                                {example.title && <strong>{example.title}</strong>}
                                <pre>{example.command}</pre>
                                {example.remarks && <p>{example.remarks}</p>}
                            </div>
                        ))}

                        {detail?.docs?.documents && detail.docs.documents.length > 0 && (
                            <>
                                <h3>Documentation</h3>
                                {detail.docs.documents.length > 1 && (
                                    <div className="doc-tabs">
                                        {detail.docs.documents.map((document) => (
                                            <button
                                                key={document.path}
                                                className={
                                                    document.path === docPath ? 'doc-tab chosen' : 'doc-tab'
                                                }
                                                onClick={() => void showDocument(
                                                    detail.script.root,
                                                    detail.script.rel,
                                                    document.path,
                                                )}
                                            >
                                                {document.title}
                                                <span className="doc-kind">{document.kind}</span>
                                            </button>
                                        ))}
                                    </div>
                                )}
                                {doc ? (
                                    doc.error ? (
                                        <p className="empty">
                                            This document could not be shown: {doc.error}
                                        </p>
                                    ) : (
                                        // The backend has already sanitized this markup.
                                        // Rendering it as HTML is the point of the feature,
                                        // so it is the one place an escape hatch is right.
                                        <div
                                            className="doc-body"
                                            dangerouslySetInnerHTML={{__html: doc.html}}
                                        />
                                    )
                                ) : (
                                    <p className="empty">Reading the document…</p>
                                )}
                            </>
                        )}

                        {detail?.related && detail.related.length > 0 && (
                            <>
                                <h3>Related</h3>
                                <ul className="related">
                                    {detail.related.map((item) => (
                                        <li key={item.rel}>
                                            <strong>{item.name}</strong>
                                            <span className="reason">{item.reason}</span>
                                        </li>
                                    ))}
                                </ul>
                            </>
                        )}
                    </>
                )}
            </section>
        </div>
    );
}

// formatValue renders a parameter default for display.
//
// A default that is an expression the reader could not evaluate is marked as
// such, because showing "(Get-Date)" as though it were a value would mislead
// whoever reads the form.
function formatValue(value?: types.Value): string {
    if (!value) {
        return '';
    }
    const shown = value.display ?? '';
    return value.isExpression ? `${shown} (evaluated when the script runs)` : shown;
}

// formatConstraint renders one validation rule in a sentence.
function formatConstraint(rule: types.Constraint): string {
    if (rule.values?.length) {
        return `one of ${rule.values.join(', ')}`;
    }
    if (rule.hasMin && rule.hasMax) {
        const {includeMin, includeMax, min, max} = rule;
        return `between ${min}${includeMin ? '' : ' (exclusive)'} and ${max}${includeMax ? '' : ' (exclusive)'}`;
    }
    if (rule.hasMin) {
        return `at least ${rule.min}${rule.includeMin ? '' : ' (exclusive)'}`;
    }
    if (rule.hasMax) {
        return `at most ${rule.max}${rule.includeMax ? '' : ' (exclusive)'}`;
    }
    if (rule.pattern) {
        return `must match ${rule.pattern}`;
    }
    return rule.label ?? rule.kind;
}

export default App;
