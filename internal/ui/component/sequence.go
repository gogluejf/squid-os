package component

import (
	tea "github.com/charmbracelet/bubbletea"
)

// SequenceStep defines a single step in a multi-step wizard.
type SequenceStep struct {
	Key       string     // name for the result in shared results
	Component Component  // child component to run
}

// Sequence is a Component that chains multiple steps.
// Each step's result is stored in a shared Result map keyed by its Key.
// Child components remain standalone — they don't know about Sequence.
type Sequence struct {
	Steps    []SequenceStep
	Current  int
	Result   map[string]any
	OnDone   func(any, map[string]any) tea.Cmd
	OnCancel func(any) tea.Cmd
	initialized bool // guard against re-wrapping on repeated Init calls
}

// Init initializes the Sequence — starts the first step.
// Idempotent: safe to call multiple times (e.g. from recalcLayout).
func (s *Sequence) Init(ctx any) {
	if s.initialized {
		return
	}
	if s.Result == nil {
		s.Result = make(map[string]any)
	}
	if len(s.Steps) == 0 {
		return
	}
	s.Current = 0
	step := &s.Steps[0]
	s.wrapStep(step, ctx)
	step.Component.Init(ctx)
	s.initialized = true
}

// wrapStep replaces the child component's OnConfirm/OnCancel with wrappers
// that capture the result and advance the sequence.
func (s *Sequence) wrapStep(step *SequenceStep, ctx any) {
	// Wrap Prompt
	if p, ok := step.Component.(*Prompt); ok {
		origConfirm := p.OnConfirm
		origCancel := p.OnCancel
		p.OnConfirm = func(value string, ctx any) tea.Cmd {
			s.Result[step.Key] = value
			s.advance(ctx)
			if origConfirm != nil {
				return origConfirm(value, ctx)
			}
			return nil
		}
		p.OnCancel = func(ctx any) tea.Cmd {
			if origCancel != nil {
				return origCancel(ctx)
			}
			return s.doCancel(ctx)
		}
	}

	// Wrap Picker
	if p, ok := step.Component.(*Picker); ok {
		origConfirm := p.OnConfirm
		origCancel := p.OnCancel
		p.OnConfirm = func(item PickerItem, ctx any) tea.Cmd {
			s.Result[step.Key] = item
			s.advance(ctx)
			if origConfirm != nil {
				return origConfirm(item, ctx)
			}
			return nil
		}
		p.OnCancel = func(ctx any) tea.Cmd {
			if origCancel != nil {
				return origCancel(ctx)
			}
			return s.doCancel(ctx)
		}
	}

	// Wrap Question
	if q, ok := step.Component.(*Question); ok {
		origConfirm := q.OnConfirm
		origCancel := q.OnCancel
		q.OnConfirm = func(selection int, instructions string, ctx any) tea.Cmd {
			s.Result[step.Key] = QuestionResult{Selection: selection, Instructions: instructions}
			s.advance(ctx)
			if origConfirm != nil {
				return origConfirm(selection, instructions, ctx)
			}
			return nil
		}
		q.OnCancel = func(ctx any) tea.Cmd {
			if origCancel != nil {
				return origCancel(ctx)
			}
			return s.doCancel(ctx)
		}
	}
}

// QuestionResult is the extracted result from a Question component.
type QuestionResult struct {
	Selection    int
	Instructions string
}

// advance moves to the next step.
func (s *Sequence) advance(ctx any) {
	s.Current++
	if s.Current < len(s.Steps) {
		next := &s.Steps[s.Current]
		s.wrapStep(next, ctx)
		next.Component.Init(ctx)
	}
}

// doCancel calls the top-level OnCancel with ctx.
func (s *Sequence) doCancel(ctx any) tea.Cmd {
	if s.OnCancel != nil {
		return s.OnCancel(ctx)
	}
	return nil
}

// Update delegates to the current child component.
func (s *Sequence) Update(msg tea.Msg, ctx any) tea.Cmd {
	if s.Current >= len(s.Steps) {
		return nil
	}
	step := &s.Steps[s.Current]
	cmd := step.Component.Update(msg, ctx)
	return cmd
}

// HandleKey delegates to the current child component.
func (s *Sequence) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	if s.Current >= len(s.Steps) {
		return nil
	}
	step := &s.Steps[s.Current]
	cmd := step.Component.HandleKey(msg, ctx)

	// After handling the key, check if all steps completed
	if s.Current >= len(s.Steps) && s.OnDone != nil {
		doneCmd := s.OnDone(ctx, s.Result)
		// Run original cmd first, then doneCmd
		if cmd != nil {
			return func() tea.Msg {
				// Execute cmd first
				go func() {
					cmd() // fire and forget the original
					doneCmd() // then fire done
				}()
				return nil
			}
		}
		return doneCmd
	}

	return cmd
}

// Render draws the current step.
func (s *Sequence) Render(width int) string {
	if s.Current >= len(s.Steps) {
		return "Sequence complete"
	}
	return s.Steps[s.Current].Component.Render(width)
}

// RenderHeight returns the height needed for the current step.
func (s *Sequence) RenderHeight() int {
	if s.Current >= len(s.Steps) {
		return 0
	}
	return s.Steps[s.Current].Component.RenderHeight()
}

// IsDone returns true if all steps have been completed.
func (s *Sequence) IsDone() bool {
	return s.Current >= len(s.Steps)
}

// Done fires OnDone with ctx and the accumulated results, marking the sequence as complete.
func (s *Sequence) Done(ctx any) tea.Cmd {
	s.Current = len(s.Steps)
	if s.OnDone != nil {
		return s.OnDone(ctx, s.Result)
	}
	return nil
}