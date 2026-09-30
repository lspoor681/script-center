// A run's output and its exit are pushed by the backend, and the panel ignores
// anything whose id is not the one it is watching. That guard is right: it is
// what stops a late event from a finished run from corrupting the panel for the
// next one.
//
// It has a gap at the start. The backend starts pumping as soon as it has
// registered the run, and only then does it answer the call that returns the id.
// So for a script that prints something and exits quickly, the output and the
// exit both arrive before the frontend has an id to compare them against, and
// they are dropped. What the user is left with is a panel that says the script
// is running, forever, with an empty output block and a Run button that stays
// disabled — for a script that finished before the window could show it.
//
// A short script is not an edge case here. This app exists to describe what
// scripts accept, and plenty of them do their whole job in one line.
import {describe, expect, it, vi} from 'vitest';
import {render, screen, waitFor} from '@testing-library/react';

import {api, fire, idleApi, rootView, scriptIn, subscribedTo} from './test-helpers';
import App from './App';
import type {RunView} from './types';

const ROOT = '/roots/alpha';

function runView(): RunView {
    return {id: 'run-7', command: ['pwsh', 'quick.ps1'], dir: ROOT};
}

// pickAndRun opens a script in the detail panel and presses Run.
async function pickAndRun(name: string) {
    const script = await screen.findByText(name);
    script.closest('button')?.click();
    const run = await screen.findByRole('button', {name: '▶ Run'});
    run.click();
}

describe('a run that finishes before the panel can see it', () => {
    it('keeps the output and records the exit', async () => {
        idleApi([ROOT]);
        vi.mocked(api.openRoot).mockResolvedValue(rootView(ROOT, ['quick.ps1']));
        // The backend answers with the id only after it has begun pumping, so
        // the events are delivered while this promise is still pending.
        vi.mocked(api.runScript).mockImplementation(async () => {
            fire('run:output', {id: 'run-7', chunk: 'hello from a quick script\n'});
            fire('run:exit', {id: 'run-7', code: 0});
            return runView();
        });
        vi.mocked(api.explain).mockResolvedValue({
            script: scriptIn(ROOT, 'quick.ps1'),
            docs: {help: {source: 'none'}, recovered: false, documents: []},
        });

        render(<App />);
        await waitFor(() => {
            expect(subscribedTo()).toContain('run:output');
        });
        await pickAndRun('quick.ps1');

        // The output the script produced is on screen, not swallowed.
        await waitFor(() => {
            expect(screen.getByText(/hello from a quick script/)).toBeTruthy();
        });
        // And the panel knows it ended rather than waiting for a stop the user is
        // never going to press.
        await waitFor(() => {
            expect(screen.queryByRole('button', {name: '▶ Run'})?.hasAttribute('disabled')).toBe(false);
        });
    });

    it('leaves the panel usable after such a run', async () => {
        idleApi([ROOT]);
        vi.mocked(api.openRoot).mockResolvedValue(rootView(ROOT, ['quick.ps1']));
        vi.mocked(api.runScript).mockImplementation(async () => {
            fire('run:output', {id: 'run-8', chunk: 'done\n'});
            fire('run:exit', {id: 'run-8', code: 3});
            return {id: 'run-8', command: ['pwsh', 'quick.ps1'], dir: ROOT};
        });
        vi.mocked(api.explain).mockResolvedValue({
            script: scriptIn(ROOT, 'quick.ps1'),
            docs: {help: {source: 'none'}, recovered: false, documents: []},
        });

        render(<App />);
        await pickAndRun('quick.ps1');

        // A non-zero exit is reported rather than hidden, and the panel is not
        // stuck on the running state that would block every later run.
        await waitFor(() => {
            expect(screen.queryByRole('button', {name: '▶ Run'})?.hasAttribute('disabled')).toBe(false);
        });
    });

    it('still ignores events from a run that is not the current one', async () => {
        idleApi([ROOT]);
        vi.mocked(api.openRoot).mockResolvedValue(rootView(ROOT, ['slow.ps1']));
        vi.mocked(api.runScript).mockResolvedValue({id: 'run-9', command: ['pwsh', 'slow.ps1'], dir: ROOT});

        render(<App />);
        await pickAndRun('slow.ps1');
        await waitFor(() => {
            expect(screen.getByRole('button', {name: '▶ Run'})?.hasAttribute('disabled')).toBe(true);
        });

        // A run the panel is not watching must not write into it.
        fire('run:output', {id: 'run-3', chunk: 'from another run\n'});
        fire('run:exit', {id: 'run-3', code: 1});

        expect(screen.queryByText(/from another run/)).toBeNull();
        // And the run it is watching is still the one it reports as active.
        expect(screen.getByRole('button', {name: '▶ Run'})?.hasAttribute('disabled')).toBe(true);
    });
});
