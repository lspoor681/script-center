import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import * as api from './api';
import * as types from './types';
import {BrowserOpenURL, EventsOn} from '../wailsjs/runtime/runtime';
import './App.css';

// The workspace crosses as a pointer to a struct rather than a named view type,
// so it is normalized here into a shape the rest of the component can rely on
// being non-null.
function asWorkspace(ws: types.WorkspaceState | null): types.WorkspaceState {
    return {roots: ws?.roots ?? [], favorites: ws?.favorites ?? []};
}

const NO_ROOTS: types.Root[] = [];

// The run panel is independent of the picked script, because a run can also be
// started from a context menu on the list. One run is active at a time, which
// matches the backend runner.
type RunStatus = 'running' | 'done' | 'stopped' | 'failed';

type RunState = {
    id: string;
    scriptName: string;
    command: string[];
    dir: string;
    warning?: string;
    error?: string;
    output: string;
    status: RunStatus;
    code?: number;
    blind?: boolean;
    // origin remembers what launched this run so the panel can rerun it with
    // the same script, arguments and elevation as the first time.
    origin?: {script: types.ScriptView; extra: string; raised: boolean};
};

// The context menu needs where it opened and what it was opened for.
type MenuState = {x: number; y: number; script: types.ScriptView};

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
    // The search box filters the already-loaded scripts of the selected root.
    const [query, setQuery] = useState('');
    // The run panel shows the live output of the one active run. It is set here
    // rather than in the detail header because a run can start from a context
    // menu without a script being picked.
    const [run, setRun] = useState<RunState | null>(null);
    const [extraArgs, setExtraArgs] = useState('');
    // Requests the run start with administrator privileges, as the run bar's
    // checkbox. A script whose metadata demands elevation is checked and
    // locked regardless of what a user picks.
    const [runAsAdmin, setRunAsAdmin] = useState(false);
    // The text typed into the run panel for the still-running script. It is
    // kept in state rather than read from the input so that Send is the
    // single place a line leaves the panel.
    const [runInput, setRunInput] = useState('');
    // activeIdRef lets a late event from an already-finished run be recognized
    // and ignored: ids are unique per run, and the frontend only ever watches
    // the most recent one.
    const activeIdRef = useRef<string | null>(null);
    // outputRef is the terminal's <pre>. New output scrolls it to the bottom,
    // so a long stream stays readable without the user dragging for it.
    const outputRef = useRef<HTMLPreElement>(null);
    // copied briefly relabels the "Copy command" button after a successful
    // copy, so the user does not click it twice wondering if it worked.
    const [copied, setCopied] = useState(false);
    // The right-click menu on a script row, or null when closed.
    const [menu, setMenu] = useState<MenuState | null>(null);

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

    // Opening the selected root is where the metadata arrives. It is a named
    // callback rather than inline in the effect so the git strip's refresh
    // button can re-read the same root.
    const loadRoot = useCallback(async (path: string) => {
        setBusy(`Reading ${path}…`);
        try {
            const next = await api.openRoot(path);
            setView(next);
            setError('');
        } catch (err) {
            fail('reading the root', err);
        } finally {
            setBusy('');
            void refreshStatus();
        }
    }, [fail, refreshStatus]);

    useEffect(() => {
        if (!selected) {
            setView(null);
            return;
        }
        void loadRoot(selected);
    }, [selected, loadRoot]);

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

    // Output arrives as events from the backend's runner, each tagged with the
    // id of the run it belongs to. A chunk is appended only while it matches
    // the active run, so events that race with the promise resolving cannot
    // smear into the wrong run.
    const handleRunOutput = useCallback((data: unknown) => {
        const record = data as {id?: unknown; chunk?: unknown};
        const id = String(record?.id ?? '');
        const chunk = String(record?.chunk ?? '');
        if (!id || id !== activeIdRef.current) {
            return;
        }
        setRun((current) => (current && current.id === id
            ? {...current, output: current.output + stripAnsi(chunk)}
            : current));
    }, []);

    const handleRunExit = useCallback((data: unknown) => {
        const record = data as {id?: unknown; code?: unknown; stopped?: unknown};
        const id = String(record?.id ?? '');
        if (!id || id !== activeIdRef.current) {
            return;
        }
        activeIdRef.current = null;
        setRun((current) => (current && current.id === id
            ? {
                ...current,
                status: record?.stopped ? 'stopped' : 'done',
                code: typeof record?.code === 'number' ? record.code : current.code,
            }
            : current));
    }, []);

    // The listeners are attached once. The callbacks read the active id through
    // a ref, so they are stable and do not need re-subscribing as the run
    // changes.
    useEffect(() => {
        const offOutput = EventsOn('run:output', (...args: unknown[]) => handleRunOutput(args[0]));
        const offExit = EventsOn('run:exit', (...args: unknown[]) => handleRunExit(args[0]));
        return () => {
            offOutput();
            offExit();
        };
    }, [handleRunOutput, handleRunExit]);

    const startRun = useCallback(async (script: types.ScriptView, extra: string, raised: boolean) => {
        setRun({
            id: '',
            scriptName: script.name,
            command: [],
            dir: '',
            status: 'running',
            output: '',
            origin: {script, extra, raised},
        });
        try {
            const view = await api.runScript(script.root, script.rel, splitArgs(extra), raised);
            activeIdRef.current = view.id;
            setRun({
                id: view.id,
                scriptName: script.name,
                command: view.command,
                dir: view.dir,
                warning: view.warning,
                status: 'running',
                output: '',
                blind: view.blind,
                origin: {script, extra, raised},
            });
        } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            setRun((current) => (current
                ? {...current, status: 'failed', error: message}
                : null));
        }
    }, []);

    // A line typed into the run panel is written to the script's input, which
    // answers prompts such as the one sudo shows for a password. A blind run
    // has no pipe to write to, so the input is hidden for it.
    const sendInput = useCallback(async (text: string) => {
        if (!run || run.id === '' || !text) {
            return;
        }
        try {
            await api.sendInput(run.id, text);
            setRunInput('');
        } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            setRun((current) => (current ? {...current, error: message} : null));
        }
    }, [run]);

    const stopRun = useCallback(async (id: string) => {
        try {
            await api.stopScript(id);
        } catch {
            // A run that ended between the click and the call is not something
            // the user needs to be told about; the exit event has already
            // updated the panel.
        }
    }, []);

    // Copying is a one-way trip to the system clipboard, which the browser
    // context the app runs in cannot reach on its own.
    const copyText = useCallback(async (text: string, what: string) => {
        try {
            await api.copyText(text);
            if (what === 'command') {
                setCopied(true);
            }
        } catch (err) {
            fail(`copying the ${what}`, err);
        }
    }, [fail]);

    const rerun = useCallback(() => {
        const origin = run?.origin;
        if (origin) {
            void startRun(origin.script, origin.extra, origin.raised);
        }
    }, [run, startRun]);

    // Closing the terminal stops an unfinished run first, then hides the pane.
    // It stays open until this button, which is the point: a finished run's
    // output remains on screen for reading, and a fresh run replaces it.
    const closeRun = useCallback(() => {
        activeIdRef.current = null;
        const id = run?.id;
        if (id) {
            void stopRun(id);
        }
        setRun(null);
    }, [run, stopRun]);

    // The "Copy command" label is trivially reset so it cannot stick.
    useEffect(() => {
        if (!copied) {
            return;
        }
        const timer = setTimeout(() => setCopied(false), 1500);
        return () => clearTimeout(timer);
    }, [copied]);

    // New output arrives faster than the eye can track, so the terminal keeps
    // its view pinned to the newest line rather than leaving the user hunting
    // through past output.
    useEffect(() => {
        const el = outputRef.current;
        if (el) {
            el.scrollTop = el.scrollHeight;
        }
    }, [run?.output]);

    // Closing the context menu is a window concern, so it is handled here: any
    // click elsewhere or Escape dismisses it. The handler is re-armed whenever
    // the menu (re)opens.
    useEffect(() => {
        if (!menu) {
            return;
        }
        const close = () => setMenu(null);
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                setMenu(null);
            }
        };
        window.addEventListener('mousedown', close);
        window.addEventListener('keydown', onKey);
        return () => {
            window.removeEventListener('mousedown', close);
            window.removeEventListener('keydown', onKey);
        };
    }, [menu]);

    // The context menu's items, in display order. An action is a one-line entry;
    // the menu's positioning and dismissal apply to all of them.
    const menus = useMemo(
        () => [
            {
                label: 'Run',
                run: (script: types.ScriptView) => {
                    void startRun(script, '', false);
                },
            },
            {
                label: 'Run (Administrator)',
                run: (script: types.ScriptView) => {
                    void startRun(script, '', true);
                },
            },
            {
                label: 'Star',
                run: (script: types.ScriptView) => {
                    void star(script);
                },
            },
            {
                label: 'Copy path',
                run: (script: types.ScriptView) => {
                    void copyText(script.path, 'path');
                },
            },
            {
                label: 'Open file location',
                run: (script: types.ScriptView) => {
                    api.revealFile(script.path).catch((err) => {
                        fail('opening the file location', err);
                    });
                },
            },
        ],
        [startRun, fail, star, copyText],
    );

    const missing = useMemo(
        () => tools.filter((tool) => !tool.available),
        [tools],
    );

    // The search box is a filter over what is already in memory, so the fields
    // the list shows are searched, plus the synopsis and parameter names a user
    // is likely to remember.
    const shown = useMemo(
        () => (view?.scripts ?? []).filter((script) => matches(script, query)),
        [view, query],
    );

    // Hoisted so the markup can branch on the count without repeating the
    // optional chain, which would leave TypeScript unable to narrow it.
    const detailParams = picked?.metadata.params ?? [];

    // A script can demand elevation in its metadata; the checkbox is then
    // checked and locked, so a user cannot accidentally run it unprivileged.
    const adminRequired = picked?.metadata.requirements?.runAsAdministrator ?? false;

    // The GitHub browse target, when the selected root has a GitHub remote.
    // Hoisted for the same reason the parameter count is: the strip branches on
    // it and then hands it to an event handler.
    const gitHub = view?.git ? gitHubUrl(view.git.remote) : null;

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

                {view?.git && (
                    <div className="git-strip" title="Repository state of this folder">
                        <span className="git-branch">
                            ⎇ {view.git.branch}
                            {view.git.detached && ' (detached)'}
                        </span>
                        <span className="git-counts">
                            {view.git.staged > 0 && <span className="git-item staged">{view.git.staged} staged</span>}
                            {view.git.unstaged > 0 && <span className="git-item unstaged">{view.git.unstaged} unstaged</span>}
                            {view.git.untracked > 0 && <span className="git-item untracked">{view.git.untracked} untracked</span>}
                            {view.git.conflicted > 0 && <span className="git-item conflicted">{view.git.conflicted} conflicted</span>}
                        </span>
                        {(view.git.ahead > 0 || view.git.behind > 0) && (
                            <span className="git-sync">↑ {view.git.ahead} · ↓ {view.git.behind}</span>
                        )}
                        {view.git.commit && (
                            <span
                                className="git-commit"
                                title={`${view.git.commit.author} · ${view.git.commit.date}`}
                            >
                                {view.git.commit.shortOid} {view.git.commit.subject}
                            </span>
                        )}
                        {gitHub && (
                            <button className="ghost" onClick={() => BrowserOpenURL(gitHub)}>
                                Open on GitHub
                            </button>
                        )}
                        <button
                            className="ghost"
                            title="Re-read this folder"
                            onClick={() => void loadRoot(selected)}
                        >
                            ↻
                        </button>
                    </div>
                )}

                {view && (
                    <label className="search">
                        <input
                            type="search"
                            placeholder="Search these scripts…"
                            value={query}
                            onChange={(e) => setQuery(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === 'Escape') {
                                    setQuery('');
                                }
                            }}
                        />
                        {query && (
                            <button
                                className="search-clear"
                                type="button"
                                aria-label="Clear search"
                                onClick={() => setQuery('')}
                            >
                                ×
                            </button>
                        )}
                    </label>
                )}

                {view && query.trim() && (
                    <p className="filter-count">
                        {shown.length} of {view.scripts.length} scripts
                    </p>
                )}

                {view && query.trim() && shown.length === 0 && (
                    <p className="empty">No scripts match “{query.trim()}”.</p>
                )}

                <ul className="scripts">
                    {shown.map((script) => {
                        const count = script.metadata.params?.length ?? 0;
                        return (
                        <li
                            key={script.rel}
                            onContextMenu={(e) => {
                                e.preventDefault();
                                setMenu({x: e.clientX, y: e.clientY, script});
                            }}
                        >
                            <button
                                className={script.rel === picked?.rel ? 'script current' : 'script'}
                                onClick={() => open(script)}
                            >
                                <span className="name">{highlight(script.name, query)}</span>
                                <span className="where">{script.dir || '.'}</span>
                                <span className="badges">
                                    <span className="badge lang">{script.lang}</span>
                                    {count > 0 && (
                                        <span className="badge params">{count} params</span>
                                    )}
                                    {script.cached && <span className="badge cached">cached</span>}
                                    {script.modified && <span className="badge modified">modified</span>}
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
                <div className="detail-body">
                {!picked && <p className="empty">Choose a script to see what it accepts.</p>}
                {picked && (
                    <>
                        <header>
                            <h2>{picked.name}</h2>
                            <p className="path">{picked.path}</p>
                            <div className="run-bar">
                                <input
                                    className="run-args"
                                    placeholder="Extra arguments…"
                                    value={extraArgs}
                                    disabled={run?.status === 'running'}
                                    onChange={(e) => setExtraArgs(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            void startRun(picked, extraArgs, adminRequired || runAsAdmin);
                                        }
                                    }}
                                />
                                <label
                                    className="run-admin"
                                    title={
                                        adminRequired
                                            ? 'This script asks to run as administrator'
                                            : 'Run with administrator privileges'
                                    }
                                >
                                    <input
                                        type="checkbox"
                                        checked={adminRequired || runAsAdmin}
                                        disabled={run?.status === 'running' || adminRequired}
                                        onChange={(e) => setRunAsAdmin(e.target.checked)}
                                    />
                                    Admin
                                </label>
                                <button
                                    className="primary"
                                    disabled={run?.status === 'running'}
                                    onClick={() => void startRun(picked, extraArgs, adminRequired || runAsAdmin)}
                                >
                                    ▶ Run
                                </button>
                                <button className="ghost" onClick={() => void star(picked)}>
                                    ☆ Star
                                </button>
                            </div>
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

                        {(detail?.dependencies && detail.dependencies.length > 0) ||
                        (detail?.unresolved && detail.unresolved.length > 0) ? (
                            <>
                                <h3>Needs</h3>
                                <p className="empty">
                                    A copy of this script has to bring these files with it.
                                </p>
                                <ul className="related">
                                    {detail?.dependencies?.map((file) => (
                                        <li key={file}>
                                            <code>{file}</code>
                                        </li>
                                    ))}
                                    {detail?.unresolved?.map((expression) => (
                                        <li key={expression}>
                                            <code>{expression}</code>
                                            <span className="reason">
                                                could not be resolved to a file
                                            </span>
                                        </li>
                                    ))}
                                </ul>
                            </>
                        ) : null}

                        {detail?.docs?.documents && detail.docs.documents.length > 0 && (
                            <>
                                <h3>ReadMe</h3>
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
                </div>

                {run && (
                    <div className="run-panel">
                        <div className="run-head">
                            <strong className="run-name">{run.scriptName}</strong>
                            {run.command.length > 0 && (
                                <code className="run-command" title={run.dir}>
                                    {run.command.join(' ')}
                                </code>
                            )}
                            <span className={`run-state ${run.status}`}>
                                {run.status === 'running' && 'Running'}
                                {run.status === 'done' && (
                                    `Exited${run.code !== undefined ? ` with code ${run.code}` : ''}`
                                )}
                                {run.status === 'stopped' && 'Stopped'}
                                {run.status === 'failed' && 'Could not start'}
                            </span>
                            {run.status === 'running' && !run.blind && (
                                <button className="ghost" onClick={() => void stopRun(run.id)}>
                                    Stop
                                </button>
                            )}
                            {run.status !== 'running' && run.origin && (
                                <button className="ghost" onClick={rerun} title="Run this again">
                                    Rerun
                                </button>
                            )}
                            {run.command.length > 0 && (
                                <button
                                    className="ghost"
                                    onClick={() => void copyText(run.command.join(' '), 'command')}
                                    title="Copy the command that was run"
                                >
                                    {copied ? 'Copied' : 'Copy command'}
                                </button>
                            )}
                            <button className="ghost" onClick={closeRun} title="Close terminal">
                                ×
                            </button>
                        </div>
                        {run.warning && <p className="warning">{run.warning}</p>}
                        {run.error && <p className="error">{run.error}</p>}
                        {run.blind && run.status === 'running' && (
                            <p className="notice">
                                Running in its own elevated window. This panel cannot see its
                                output, send it input, or stop it; the exit code arrives when it
                                closes.
                            </p>
                        )}
                        <pre className="run-output" ref={outputRef}>
                            {run.output || (run.status === 'running' ? 'Running…' : '(no output)')}
                        </pre>
                        {run.status === 'running' && !run.blind && (
                            <div className="run-input-row">
                                <input
                                    className="run-input"
                                    placeholder="Input for the running script…"
                                    value={runInput}
                                    onChange={(e) => setRunInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            void sendInput(runInput);
                                        }
                                    }}
                                />
                                <button
                                    className="primary"
                                    disabled={!runInput.trim()}
                                    onClick={() => void sendInput(runInput)}
                                >
                                    Send
                                </button>
                            </div>
                        )}
                    </div>
                )}
            </section>

            {menu && (
                <div
                    className="context-menu"
                    style={{
                        left: Math.min(menu.x, window.innerWidth - 150),
                        top: Math.min(menu.y, window.innerHeight - 60),
                    }}
                    onMouseDown={(e) => e.stopPropagation()}
                >
                    {menus.map((item) => (
                        <button
                            key={item.label}
                            className="context-item"
                            onClick={() => {
                                const target = menu.script;
                                setMenu(null);
                                item.run(target);
                            }}
                        >
                            {item.label}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}

// matches reports whether a script matches a search query. The search is a
// filter over the scripts already loaded for the selected root, so it looks at
// the fields the user can see in the list plus the synopsis and parameter
// names they are likely to remember.
function matches(script: types.ScriptView, query: string): boolean {
    const q = query.trim().toLowerCase();
    if (!q) {
        return true;
    }
    const hay = [
        script.name,
        script.rel,
        script.dir,
        script.lang,
        script.language,
        script.metadata?.help?.synopsis ?? '',
        ...(script.metadata?.params?.map((p) => p.name) ?? []),
    ];
    return hay.some((text) => text.toLowerCase().includes(q));
}

// highlight wraps the first case-insensitive query match in the text with a
// mark, so a filtered list shows why each row is there.
function highlight(text: string, query: string) {
    const q = query.trim();
    if (!q) {
        return text;
    }
    const idx = text.toLowerCase().indexOf(q.toLowerCase());
    if (idx < 0) {
        return text;
    }
    return (
        <>
            {text.slice(0, idx)}
            <mark>{text.slice(idx, idx + q.length)}</mark>
            {text.slice(idx + q.length)}
        </>
    );
}

// gitHubUrl turns a git remote URL into a GitHub browse URL, or null when the
// remote is not a GitHub one. Both the https and the git@ssh forms of a
// github.com remote arrive as a plain absolute URL on the wire.
function gitHubUrl(remote?: string): string | null {
    if (!remote) {
        return null;
    }
    let target = remote.trim().replace(/\.git$/, '');
    if (target.startsWith('git@github.com:')) {
        target = 'https://github.com/' + target.slice('git@github.com:'.length);
    }
    if (!target.startsWith('https://github.com/')) {
        return null;
    }
    return target;
}

// stripAnsi removes ANSI escape sequences, which a script can emit even with
// NO_COLOR set (progress bars and control prompts are not always hidden by it).
// The run panel is a plain text block; escape codes would render as garbage.
function stripAnsi(text: string): string {
    return text
        .replace(/\u001B\][^\u0007]*(?:\u0007|\u001B\\)/g, '')
        .replace(/\u001B\[[0-9;?]*[ -\/]*[@-~]/g, '');
}

// splitArgs turns a free-text argument string into a list. It splits on
// whitespace and honors double quotes, like a simple shell line; backslashes
// are passed through literally, because the strings are handed to the script
// as typed.
function splitArgs(text: string): string[] {
    const out: string[] = [];
    const pattern = /"([^"]*)"|(\S+)/g;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(text))) {
        out.push(match[1] ?? match[2]);
    }
    return out;
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
