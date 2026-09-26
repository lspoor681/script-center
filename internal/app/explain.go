package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/lspoor/script-center/internal/deps"
	"github.com/lspoor/script-center/internal/docs"
)

// Explain returns a script's documentation along with the scripts related to it
// and the files it needs to run.
func (s *Service) Explain(ctx context.Context, root, rel string) (*ExplainView, error) {
	view, err := s.cachedRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	script, ok := findScript(view, rel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoScript, rel)
	}

	explanation, err := docs.Explain(script.Path, view.Root, script.Metadata)
	if err != nil {
		return nil, err
	}

	out := &ExplainView{Script: *script, Docs: explanation}
	for _, suggestion := range s.relatedFor(view, script) {
		out.Related = append(out.Related, Suggestion{
			Path:   suggestion.Entry.Path,
			Root:   suggestion.Entry.Root,
			Rel:    suggestion.Entry.Rel,
			Name:   suggestion.Entry.Name,
			Reason: suggestion.Reason,
		})
	}
	out.Dependencies, out.Unresolved = s.dependencies(view, script)
	return out, nil
}

// dependencies resolves what a script dot-sources, as paths relative to the root
// so they read as files within the project rather than as absolute locations.
//
// Only the harvested dot-sources are considered. Resolving the whole of a
// script's file system behavior is not something a listing can do honestly, and
// a partial answer with the unresolved expressions named is more useful than a
// confident wrong one.
func (s *Service) dependencies(view *RootView, script *ScriptView) (files, unresolved []string) {
	dotSources := script.Metadata.DotSources
	if len(dotSources) == 0 {
		return nil, nil
	}
	closure, err := deps.ResolveOnce(script.Path, dotSources)
	if err != nil {
		// The dot-sources could not be walked at all. The script's own report
		// still carries what it asked for, so this is reported rather than
		// silently producing an empty list that reads as "needs nothing".
		return nil, dotSources
	}
	entry := filepath.Clean(script.Path)
	for _, file := range closure.Files {
		if filepath.Clean(file) == entry {
			continue
		}
		if relative, err := filepath.Rel(view.Root, file); err == nil {
			files = append(files, filepath.ToSlash(relative))
			continue
		}
		// A file outside the root cannot be named relative to it. The absolute
		// path is still worth showing, because "this reaches outside the
		// project" is the interesting fact.
		files = append(files, file)
	}
	return files, closure.Unresolved
}

// RenderDocument returns one of a script's discovered documents, rendered to
// sanitized HTML.
//
// The path is checked against what discovery actually found for this script.
// Without that check the binding would read any file the user account can read,
// which is a much larger capability than "show me the readme next to this
// script" and is not one a script browser should hand to a web view.
func (s *Service) RenderDocument(ctx context.Context, root, rel, path string) (*DocumentView, error) {
	view, err := s.cachedRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	script, ok := findScript(view, rel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoScript, rel)
	}

	documents, err := docs.Discover(script.Path, view.Root)
	if err != nil {
		return nil, err
	}
	document, ok := findDocument(documents, path)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoDocument, path)
	}

	out := &DocumentView{
		Path:  document.Path,
		Kind:  string(document.Kind),
		Title: document.Title,
	}
	// A readme that has a section about this script is rendered as that section
	// alone. A project readme that happens to be an ancestor is often about
	// something else, and rendering all of it would bury the few lines that
	// answer the question.
	out.HTML, err = renderDocument(document)
	if err != nil {
		// A document that cannot be rendered is still worth listing, so the
		// failure is carried in the view instead of returned as an error that
		// would hide the other documents that did work.
		out.Error = err.Error()
	}
	return out, nil
}

// renderDocument renders a discovered document, honoring its section excerpt.
func renderDocument(document docs.Document) (string, error) {
	if document.Excerpted {
		return docs.Render(filepath.Ext(document.Path), []byte(document.Section))
	}
	return docs.RenderFile(document.Path)
}

// findDocument locates a discovered document by path. The comparison is on
// cleaned paths so that a frontend passing an equivalent form still matches.
func findDocument(documents []docs.Document, path string) (docs.Document, bool) {
	want := filepath.Clean(path)
	for _, document := range documents {
		if filepath.Clean(document.Path) == want {
			return document, true
		}
	}
	return docs.Document{}, false
}
