package component

import (
	tea "github.com/charmbracelet/bubbletea"
)

// SequenceStep defines a single step in a multi-step wizard.
type SequenceStep struct {
	Key       string    // unique key for looking up this step and storing results
	Component Component // child component to run
	OnEnter   func(ctx any, result map[string]any)      // called when the step becomes active (before first render)
	OnAdvance func(ctx any, result map[string]any) string // returns the key of the next step; "" = done
}

// Sequence is a Component that chains steps as a state machine.
// Steps is a map keyed by step name.  Each step's result is stored in a
// shared Result map keyed by its Key.  After a step confirms its OnAdvance
// callback (a middleware / router) inspects the accumulated results and
// returns the *key* of the next step.  A return value of "" means the
// sequence is complete.
//
// Child components remain standalone — they don't know about Sequence.
type Sequence struct {
	Steps       map[string]SequenceStep
	StartKey    string // the first step key to run
	Current     string // key of the active step
	Result      map[string]any
	OnDone      func(any, map[string]any) tea.Cmd
	OnCancel    func(any) tea.Cmd
	initialized bool
}

// Init initializes the Sequence — starts at StartKey.
// Idempotent: safe to call multiple times (e.g. from recalcLayout).
func (s *Sequence) Init(ctx any) {
	if s.initialized {
		return
	}
	if s.Result == nil {
		s.Result = make(map[string]any)
	}
	if len(s.Steps) == 0 || s.StartKey == "" {
		return
	}
	s.Current = s.StartKey
	step := s.Steps[s.Current]
	if step.OnEnter != nil {
		step.OnEnter(ctx, s.Result)
		s.Steps[s.Current] = step
	}
	step = s.Steps[s.Current] // re-read in case OnEnter mutated it
	s.wrapStep(&step, ctx)
	step.Component.Init(ctx)
	s.initialized = true
}

// wrapStep replaces the child component's OnConfirm/OnCancel with wrappers
// that capture the result, call OnAdvance, and navigate to the next step.
func (s *Sequence) wrapStep(step *SequenceStep, ctx any) {
	// Wrap Prompt
	if p, ok := step.Component.(*Prompt); ok {
		origConfirm := p.OnConfirm
		origCancel := p.OnCancel
		p.OnConfirm = func(value string, ctx any) tea.Cmd {
			s.Result[step.Key] = value
			if origConfirm != nil {
				return tea.Sequence(
					func() tea.Msg {
						_ = origConfirm(value, ctx)
						return nil
					},
					s.advance(*step, ctx),
				)
			}
			return s.advance(*step, ctx)
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
			if origConfirm != nil {
				return tea.Sequence(
					func() tea.Msg {
						_ = origConfirm(item, ctx)
						return nil
					},
					s.advance(*step, ctx),
				)
			}
			return s.advance(*step, ctx)
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
			if origConfirm != nil {
				return tea.Sequence(
					func() tea.Msg {
						_ = origConfirm(selection, instructions, ctx)
						return nil
					},
					s.advance(*step, ctx),
				)
			}
			return s.advance(*step, ctx)
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

// advance calls the step's OnAdvance middleware to determine the next step key,
// then navigates to it (or finishes).
func (s *Sequence) advance(step SequenceStep, ctx any) tea.Cmd {
	var nextKey string
	if step.OnAdvance != nil {
		nextKey = step.OnAdvance(ctx, s.Result)
	} else {
		// No OnAdvance — sequence ends
		nextKey = ""
	}

	if nextKey == "" {
		// Sequence complete
		return s.doDone(ctx)
	}

	if next, ok := s.Steps[nextKey]; ok {
		s.Current = nextKey
		s.wrapStep(&next, ctx)
		// Call OnEnter so the step can mutate its component (description, etc.)
		// before the first render.  Pass by value — the caller re-reads from
		// the map on the next Render/HandleKey call.
		if next.OnEnter != nil {
			next.OnEnter(ctx, s.Result)
			// Store the (potentially mutated) step back into the map
			s.Steps[nextKey] = next
		}
		next.Component.Init(ctx)
		// If the next step is a Question, return its blink cmd
		if q, ok := next.Component.(*Question); ok {
			return q.BlinkCmd()
		}
	} else {
		// Unknown key — just finish
		return s.doDone(ctx)
	}
	return nil
}

// doDone fires OnDone with ctx and the accumulated results.
func (s *Sequence) doDone(ctx any) tea.Cmd {
	if s.OnDone != nil {
		return s.OnDone(ctx, s.Result)
	}
	return nil
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
	if s.Current == "" {
		return nil
	}
	step, ok := s.Steps[s.Current]
	if !ok {
		return nil
	}
	return step.Component.Update(msg, ctx)
}

// HandleKey delegates to the current child component.
func (s *Sequence) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	if s.Current == "" {
		return nil
	}
	step, ok := s.Steps[s.Current]
	if !ok {
		return nil
	}
	return step.Component.HandleKey(msg, ctx)
}

// Render draws the current step.
func (s *Sequence) Render(width int) string {
	if s.Current == "" {
		return "Sequence complete"
	}
	step, ok := s.Steps[s.Current]
	if !ok {
		return "Sequence complete"
	}
	return step.Component.Render(width)
}

// RenderHeight returns the height needed for the current step.
func (s *Sequence) RenderHeight() int {
	if s.Current == "" {
		return 0
	}
	step, ok := s.Steps[s.Current]
	if !ok {
		return 0
	}
	return step.Component.RenderHeight()
}

// IsDone returns true if the sequence has finished.
func (s *Sequence) IsDone() bool {
	return s.Current == ""
}

// Done fires OnDone with ctx and the accumulated results, marking the sequence as complete.
func (s *Sequence) Done(ctx any) tea.Cmd {
	s.Current = ""
	if s.OnDone != nil {
		return s.OnDone(ctx, s.Result)
	}
	return nil
}
