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
    ChooseRoot as chooseRootBinding,
    Explain as explainBinding,
    Invalidate as invalidateBinding,
    OpenRoot as openRootBinding,
    RemoveRoot as removeRootBinding,
    RenderDocument as renderDocumentBinding,
    RunScript as runScriptBinding,
    Status as statusBinding,
    StopScript as stopScriptBinding,
    ToggleFavorite as toggleFavoriteBinding,
    Toolchains as toolchainsBinding,
    Workspace as workspaceBinding,
} from '../wailsjs/go/main/App';
import type {
    DocumentView,
    ExplainView,
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

export function runScript(root: string, rel: string, extra: string[]): Promise<RunView> {
    return wire<Promise<RunView>>(runScriptBinding(root, rel, extra));
}

export function stopScript(id: string): Promise<void> {
    return stopScriptBinding(id);
}

export function toolchains(): Promise<Toolchain[]> {
    return toolchainsBinding() as Promise<Toolchain[]>;
}
