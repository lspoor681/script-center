// Two windows into one script list, and both of them are written to by a call
// that cannot be cancelled from the frontend.
//
// Explaining a script is a second read of the same file, and clicking through a
// list starts one per click, so two are in flight whenever the user moves faster
// than the backend answers. Explaining a large script, or one whose parameters
// need a toolchain that has to start, takes long enough to lose this race
// routinely. When the older answer lands last it puts one script's documentation
// under another script's name, which reads as the app having described the wrong
// file.
//
// Re-reading is worse in kind. The answer identifies the script it re-read, and
// the panel applies it to whichever root the view currently holds, matching on
// the relative path alone. Two folders holding a deploy.ps1 is ordinary rather
// than an accident, so the wrong root's script gets replaced with a script from
// somewhere else, and the list stops matching the folder on disk.
import {describe, expect, it, vi} from 'vitest';
import {render, screen, waitFor} from '@testing-library/react';

import {api, deferred, idleApi, rootView, scriptIn} from './test-helpers';
import type {ExplainView} from './types';
import App from './App';

const ALPHA = '/roots/alpha';
const BETA = '/roots/beta';

// explainDeferreds hands out one deferred per call so the test chooses both what
// each answer describes and the order the answers arrive in.
function explainDeferreds() {
    const calls: Array<{rel: string; gate: ReturnType<typeof deferred<ExplainView>>}> = [];
    vi.mocked(api.explain).mockImplementation(async (root, rel) => {
        const gate = deferred<ExplainView>();
        calls.push({rel, gate});
        return gate.promise;
    });
    return calls;
}

// The description is the one piece of an explanation the panel prints verbatim,
// so it is what these tests look for: it makes the answer identifiable at a
// glance without depending on the rest of the detail layout.
const explainSaying = (root: string, rel: string, text: string): ExplainView => ({
    script: scriptIn(root, rel),
    docs: {help: {source: 'none', description: text}, recovered: false, documents: []},
});

describe('explaining a script', () => {
    it('shows the explanation for the script the user is looking at', async () => {
        idleApi([ALPHA]);
        vi.mocked(api.openRoot).mockResolvedValue(rootView(ALPHA, ['first.ps1', 'second.ps1']));
        const calls = explainDeferreds();

        render(<App />);
        await screen.findByText('first.ps1');
        await screen.findByText('second.ps1');

        screen.getByText('first.ps1').closest('button')?.click();
        screen.getByText('second.ps1').closest('button')?.click();

        expect(calls.map((c) => c.rel)).toEqual(['first.ps1', 'second.ps1']);

        // The answer for the script now on screen arrives first.
        calls[1].gate.resolve(explainSaying(ALPHA, 'second.ps1', 'about second'));

        await waitFor(() => {
            expect(screen.getByText(/about second/i)).toBeTruthy();
        });

        // The first click's explanation turns up late and is about a different
        // script entirely.
        calls[0].gate.resolve(explainSaying(ALPHA, 'first.ps1', 'about first'));

        await waitFor(() => {
            expect(screen.queryByText(/about first/i)).toBeNull();
        });
        expect(screen.getByText(/about second/i)).toBeTruthy();
        // And the heading still names the script the user clicked last.
        expect(screen.getByRole('heading', {name: 'second.ps1'})).toBeTruthy();
    });
});

describe('re-reading a script', () => {
    it('does not put a script into a different root that shares its name', async () => {
        idleApi([ALPHA, BETA]);
        // Both roots hold a deploy.ps1, which is the case the root comparison
        // exists for.
        vi.mocked(api.openRoot).mockImplementation(async (root) =>
            rootView(root, ['deploy.ps1', `only-in-${root.split('/').pop()}.ps1`]),
        );
        vi.mocked(api.explain).mockResolvedValue(explainSaying(ALPHA, 'deploy.ps1', 'alpha deploy'));

        const rereadGate = deferred<ReturnType<typeof scriptIn>>();
        vi.mocked(api.invalidate).mockReturnValue(rereadGate.promise);

        render(<App />);
        await screen.findByText('deploy.ps1');
        await screen.findByText('only-in-alpha.ps1');

        // Ask for a re-read in alpha, then move to beta before it comes back.
        const rereadButton = screen.getAllByTitle('Read this script again')[0];
        rereadButton.click();

        screen.getByRole('button', {name: 'beta'}).click();
        await screen.findByText('only-in-beta.ps1');

        // The re-read of alpha's deploy.ps1 lands now, while beta's list is up.
        // It is marked with a language no script in beta has, so if it lands in
        // the wrong list the difference is visible rather than inferred.
        rereadGate.resolve({...scriptIn(ALPHA, 'deploy.ps1'), lang: 'FROM ALPHA'});

        // The re-read's own progress label is cleared only after it has applied
        // its answer, so waiting for it to go is what makes the assertion below
        // look at the settled list rather than the one still being painted.
        await waitFor(() => {
            expect(screen.queryByText('Re-reading…')).toBeNull();
        });

        // beta's own deploy.ps1 must still be the one beta has on disk.
        await waitFor(() => {
            const row = screen.getByText('deploy.ps1').closest('li');
            expect(row?.textContent).toContain('PowerShell');
            expect(row?.textContent).not.toContain('FROM ALPHA');
        });
        expect(screen.getByText('only-in-beta.ps1')).toBeTruthy();
        expect(screen.queryByText('only-in-alpha.ps1')).toBeNull();
    });
});
