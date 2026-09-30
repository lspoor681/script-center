// jsdom implements neither of these, and App.tsx reaches for both while it
// mounts. The focus event is the signal that triggers a git refresh, so leaving
// it undefined would make the window throw on every render.
import {afterEach, vi} from 'vitest';
import {cleanup} from '@testing-library/react';

// Both modules the component reaches out to are replaced here, in the setup
// file rather than in each test, for two reasons. Vitest's automocking would
// have to load the real module to learn its shape, and loading ./api means
// importing the generated Wails bindings, which call into window.go and have
// nothing to talk to under jsdom; and vi.mock has to be evaluated before the
// module it replaces is imported, which a helper imported by a test file cannot
// guarantee. Naming every export also means one added to ./api shows up as a
// missing mock rather than a silent undefined call.
vi.mock('./api', () => ({
    status: vi.fn(),
    workspace: vi.fn(),
    addRoot: vi.fn(),
    removeRoot: vi.fn(),
    chooseRoot: vi.fn(),
    toggleFavorite: vi.fn(),
    openRoot: vi.fn(),
    refreshGit: vi.fn(),
    explain: vi.fn(),
    renderDocument: vi.fn(),
    invalidate: vi.fn(),
    runScript: vi.fn(),
    sendInput: vi.fn(),
    revealFile: vi.fn(),
    copyText: vi.fn(),
    stopScript: vi.fn(),
    toolchains: vi.fn(),
    openFile: vi.fn(),
    detectEditors: vi.fn(),
    getPreferredEditor: vi.fn(),
    setWorkspacePreferredEditor: vi.fn(),
    setGlobalPreferredEditor: vi.fn(),
    getGlobalEditorConfig: vi.fn(),
    saveGlobalEditorConfig: vi.fn(),
}));

// The component subscribes to run:output, run:exit and read:stage as it mounts,
// and there is no window event bus under jsdom, so the generated runtime is
// replaced too. EventsOn hands back an unsubscribe function, which the component
// calls on cleanup, so the mock has to as well.
vi.mock('../wailsjs/runtime/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    BrowserOpenURL: vi.fn(),
}));


afterEach(() => {
    cleanup();
    window.getSelection()?.removeAllRanges();
});

// jsdom has no layout engine, so anything measuring an element gets zero. The
// hover menu in particular needs a non-zero height to decide it has room, or
// every branch of it takes the "no room" path and the tests would pass without
// exercising anything.
Object.defineProperty(HTMLElement.prototype, 'getBoundingClientRect', {
    configurable: true,
    value: () => ({
        x: 0,
        y: 0,
        top: 0,
        left: 0,
        bottom: 400,
        right: 400,
        width: 400,
        height: 400,
        toJSON: () => ({}),
    }),
});

if (!window.matchMedia) {
    window.matchMedia = ((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
    })) as unknown as typeof window.matchMedia;
}
