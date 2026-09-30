// The one place where generated bindings meet hand-written types.
//
// Wails' generator names its model fields after the Go identifiers, which for
// some packages differ in case from the JSON tags actually sent. Rather than
// depend on names the payload does not have, every call is asserted into the
// wire types from ./types at this boundary. The cast is deliberate and
// localized: everywhere else in the frontend the shapes are the real ones, and a
// change to the Go side that breaks this shows up as a type error in one file.
import {
    AddRoot as addRootBinding,
    BuildArgv as buildArgvBinding,
    ChooseRoot as chooseRootBinding,
    CopyText as copyTextBinding,
    DetectEditors as detectEditorsBinding,
    Explain as explainBinding,
    GetGlobalEditorConfig as getGlobalEditorConfigBinding,
    GetPreferredEditor as getPreferredEditorBinding,
    Invalidate as invalidateBinding,
    OpenFile as openFileBinding,
    OpenRoot as openRootBinding,
    RefreshGit as refreshGitBinding,
    RemoveRoot as removeRootBinding,
    RenderDocument as renderDocumentBinding,
    RevealFile as revealFileBinding,
    RunScript as runScriptBinding,
    SaveGlobalEditorConfig as saveGlobalEditorConfigBinding,
    SendInput as sendInputBinding,
    SetGlobalPreferredEditor as setGlobalPreferredEditorBinding,
    SetWorkspacePreferredEditor as setWorkspacePreferredEditorBinding,
    Status as statusBinding,
    StopScript as stopScriptBinding,
    ToggleFavorite as toggleFavoriteBinding,
    Toolchains as toolchainsBinding,
    Workspace as workspaceBinding,
} from '../wailsjs/go/main/App';
import type {
    DocumentView,
    Editor,
    EditorConfig,
    ExplainView,
    GitRefresh,
    PreferredEditorResult,
    RootView,
    RunView,
    ScriptView,
    Status,
    Toolchain,
    WorkspaceState,
} from './types';

const wire = <T>(value: unknown): T => value as T;

export function status(): Promise<Status> {
    return statusBinding() as Promise<Status>;
}

export function workspace(): Promise<WorkspaceState> {
    return workspaceBinding() as Promise<WorkspaceState>;
}

export function addRoot(path: string, name: string): Promise<WorkspaceState> {
    return addRootBinding(path, name) as Promise<WorkspaceState>;
}

export function removeRoot(path: string): Promise<WorkspaceState> {
    return removeRootBinding(path) as Promise<WorkspaceState>;
}

export function chooseRoot(): Promise<WorkspaceState> {
    return chooseRootBinding() as Promise<WorkspaceState>;
}

export function toggleFavorite(root: string, rel: string): Promise<WorkspaceState> {
    return toggleFavoriteBinding(root, rel) as Promise<WorkspaceState>;
}

export function openRoot(root: string): Promise<RootView> {
    return wire<Promise<RootView>>(openRootBinding(root));
}

export function refreshGit(root: string): Promise<GitRefresh> {
    return wire<Promise<GitRefresh>>(refreshGitBinding(root));
}

export function explain(root: string, rel: string): Promise<ExplainView> {
    return wire<Promise<ExplainView>>(explainBinding(root, rel));
}

export function renderDocument(
    root: string,
    rel: string,
    path: string,
): Promise<DocumentView> {
    return wire<Promise<DocumentView>>(renderDocumentBinding(root, rel, path));
}

export function invalidate(root: string, rel: string): Promise<ScriptView> {
    return wire<Promise<ScriptView>>(invalidateBinding(root, rel));
}

export function buildArgv(
    root: string,
    rel: string,
    values: Record<string, any>,
): Promise<string[]> {
    return wire<Promise<string[]>>(buildArgvBinding(root, rel, values));
}

export function runScript(
    root: string,
    rel: string,
    extra: string[],
    runAsAdmin: boolean,
): Promise<RunView> {
    return wire<Promise<RunView>>(runScriptBinding(root, rel, extra, runAsAdmin));
}

export function sendInput(id: string, text: string): Promise<void> {
    return sendInputBinding(id, text);
}

export function revealFile(path: string): Promise<void> {
    return revealFileBinding(path);
}

export function copyText(text: string): Promise<void> {
    return copyTextBinding(text);
}

export function stopScript(id: string): Promise<void> {
    return stopScriptBinding(id);
}

export function toolchains(): Promise<Toolchain[]> {
    return toolchainsBinding() as Promise<Toolchain[]>;
}

export function openFile(path: string, editorId: string): Promise<void> {
    return openFileBinding(path, editorId);
}

export function detectEditors(): Promise<Editor[]> {
    return detectEditorsBinding() as Promise<Editor[]>;
}

export function getPreferredEditor(ext: string): Promise<PreferredEditorResult> {
    return getPreferredEditorBinding(ext) as Promise<PreferredEditorResult>;
}

export function setWorkspacePreferredEditor(ext: string, editorId: string): Promise<void> {
    return setWorkspacePreferredEditorBinding(ext, editorId);
}

export function setGlobalPreferredEditor(ext: string, editorId: string): Promise<void> {
    return setGlobalPreferredEditorBinding(ext, editorId);
}

export function getGlobalEditorConfig(): Promise<EditorConfig> {
    return getGlobalEditorConfigBinding() as Promise<EditorConfig>;
}

export function saveGlobalEditorConfig(cfg: EditorConfig): Promise<void> {
    // The Wails binding expects the generated EditorConfig class which has convertValues method.
    // We pass it as unknown to satisfy TypeScript since our interface is compatible.
    return saveGlobalEditorConfigBinding(cfg as unknown as Parameters<typeof saveGlobalEditorConfigBinding>[0]);
}
