# Async Process Ownership and Deterministic Lifecycle Tests

## Intent
Keep asynchronous work alive until its actual completion, with explicit cancellation ownership and tests that distinguish lifecycle bugs from timing noise.

## Rule
- A function returning an active process must transfer context cancellation to the process owner. Do not defer cancellation in the spawning function: its return is not the child's completion.
- After a successful Start, call Wait exactly once, even when stdin writing or closing fails. Close stdin to deliver EOF. Release timeout resources when the asynchronous operation finishes; clean them immediately on failed startup.
- Protect coordinator state consistently. Never reacquire the same non-reentrant mutex while holding it. Invoke reentrant policy callbacks outside the state lock and revalidate admission afterward.
- Drain accepted work FIFO, one successor per completion. Producer EOF closes admission; it does not cancel accepted work. Cancellation discards pending work and terminates active work. Do not confuse these transitions.
- Validate delayed callbacks against the exact active process identity before changing state. A superseded process must not clear newer playback.
- Test actual process readiness with a handshake or observable output, not arbitrary sleeps. Inject a delayed completion explicitly when testing stale callbacks.
- A deliberately hanging fixture needs explicit cancellation and cleanup. End-of-input is not a kill signal.
- Test OS process-group cancellation with a controlled parent and child, independently of live servers, installed audio clients, and sound hardware. On Linux, distinguish exited zombies awaiting reaping from running orphan processes.

## Context
An asynchronous subprocess returned successfully, but tests saw empty logs and waited until their polling deadlines. The spawning function deferred its timeout cancellation, killing the child immediately on return, often before the shell wrote anything. The empty log was a production lifecycle bug, not evidence that the test shell was broken.

After fixing that first defect, queue defects became visible: recursive locking, concurrent release of all successors, and treating producer completion as cancellation. Some tests also expected a deliberately hanging child to exit after producer EOF. Fix production ownership and fixture semantics separately; do not weaken assertions or increase timeouts to hide either problem.

## Example
```go
func startAsync(parent context.Context, executable string) (*exec.Cmd, <-chan error, error) {
    ctx, cancel := context.WithTimeout(parent, 10*time.Second)
    cmd := exec.CommandContext(ctx, executable)
    if err := cmd.Start(); err != nil {
        cancel()
        return nil, nil, err
    }
    done := make(chan error, 1)
    go func() {
        defer cancel()
        defer close(done)
        done <- cmd.Wait()
    }()
    return cmd, done, nil
}
```
The caller's context remains the cancellation mechanism; returning from the constructor does not cancel it. The goroutine owns completion and timeout cleanup. Process-group termination is an additional platform-specific responsibility when children spawn descendants.

## Anti-Pattern
```go
ctx, cancel := context.WithTimeout(parent, limit)
defer cancel() // WRONG when this function returns before playback finishes.
cmd := exec.CommandContext(ctx, executable)
if err := cmd.Start(); err != nil {
    return nil, err
}
return cmd, nil // Deferred cancellation kills the just-started child.
```

Do not repeatedly run an unchanged failing probe. Read the ownership path first: context creation, deferred cleanup, Start, stdin EOF, Wait, and callbacks. Make one hypothesis-driven change, run one focused regression with an explicit Go test timeout, then run the package suite and race detector. Preserve command exit status; piping a failing test through a successful formatter can conceal failure. If the result is unchanged, revise the hypothesis rather than rerunning indefinitely.
