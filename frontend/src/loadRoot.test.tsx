// The script list is driven by whichever root read finished last, not by
// whichever root the sidebar has selected. Two roots can be read at once
// because selecting the second does not cancel the first, and the second answer
// is not necessarily the second to arrive: a folder on a spinning disk or a
// network share can take seconds while a local one takes milliseconds, so
// clicking fast through roots lands the older read last and paints a list that
// belongs to a root the user has already moved off.
//
// The same shape of bug applies to any call that writes the shared `view`, so
// each is checked by resolving the older request after the newer one and asking
// whether the panel still shows what the user actually selected.
import {describe, expect, it, vi} from 'vitest';
import {render, screen, waitFor} from '@testing-library/react';

import {api, deferred, idleApi, rootView, scriptIn} from './test-helpers';
import App from './App';

const ALPHA = '/roots/alpha';
const BETA = '/roots/beta';

async function select(name: string) {
    const button = await screen.findByRole('button', {name});
    await waitFor(() => expect(button).toBeTruthy());
    button.click();
}

// openRootDeferreds hands out one deferred per call, so a test decides both
// which root each read belongs to and the order the answers come back in.
function openRootDeferreds() {
    const calls: Array<{path: string; gate: ReturnType<typeof deferred<ReturnType<typeof rootView>>>}> = [];
    vi.mocked(api.openRoot).mockImplementation((path) => {
        const gate = deferred<ReturnType<typeof rootView>>();
        calls.push({path, gate});
        return gate.promise;
    });
    return calls;
}

describe('loading a root', () => {
    it('keeps the newer root when the older read finishes last', async () => {
        idleApi([ALPHA, BETA]);
        const calls = openRootDeferreds();

        render(<App />);
        await select('alpha');
        await select('beta');

        // Both reads are in flight. beta is what the user is looking at.
        expect(calls.map((c) => c.path)).toEqual([ALPHA, BETA]);

        const beta = calls[1];
        const alpha = calls[0];

        beta.gate.resolve(rootView(BETA, ['beta-script.ps1']));
        await screen.findByText('beta-script.ps1');

        // The straggler for alpha lands now, after the user has already moved on.
        alpha.gate.resolve(rootView(ALPHA, ['alpha-script.ps1']));

        await waitFor(() => {
            expect(screen.queryByText('alpha-script.ps1')).toBeNull();
        });
        expect(screen.getByText('beta-script.ps1')).toBeTruthy();
    });

    it('does not show a second read of the same root twice', async () => {
        idleApi([ALPHA]);
        const calls = openRootDeferreds();

        render(<App />);
        await select('alpha');

        expect(calls.length).toBe(1);
        calls[0].gate.resolve(rootView(ALPHA, ['only.ps1']));
        await screen.findByText('only.ps1');
    });
});

describe('re-reading a script', () => {
    it('replaces only the script that was re-read', async () => {
        idleApi([ALPHA]);
        vi.mocked(api.openRoot).mockResolvedValue(rootView(ALPHA, ['a.ps1', 'b.ps1']));
        const fresh = {...scriptIn(ALPHA, 'a.ps1'), size: 999, cached: false};
        vi.mocked(api.invalidate).mockResolvedValue(fresh);

        render(<App />);
        await screen.findByText('a.ps1');

        const rereadButton = screen.getAllByTitle('Read this script again')[0];
        rereadButton.click();

        await waitFor(() => {
            // A fresh read is no longer served from the cache, so the badge goes.
            const row = screen.getByText('a.ps1').closest('li');
            expect(row?.textContent).not.toContain('cached');
        });
        // The sibling is untouched: it was never read.
        const other = screen.getByText('b.ps1').closest('li');
        expect(other?.textContent).toContain('cached');
    });
});
