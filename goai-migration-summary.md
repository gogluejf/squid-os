;# GoAI Migration Summary

## Current Situation

We spent this session debugging the current custom streaming stack:

- `ProviderSettings -> provider -> adapter -> engine -> app stream state -> tool execution`
- The biggest instability is in the custom adapter/engine path for tool-call streaming.
- OpenAI/Codex multi-tool turns are especially fragile.
- Tool calls can:
  - render during stream,
  - disappear after completion,
  - merge incorrectly,
  - or produce malformed arguments.

## Main Problems Identified

### 1. Custom tool-call streaming assembly is too fragile
Current flow has too many transformation stages:

- raw SSE event
- adapter event
- engine buffer
- partial tool state
- streaming render state
- final tool entry
- execution entry

That creates identity/order bugs, especially for Codex multi-tool responses.

### 2. Codex / Responses API shape does not fit our current assumptions well
Codex tool calls use `call_id` and SSE item events rather than the older chat-completions-style indexed deltas.

We found that:

- names can arrive separately from arguments,
- argument deltas need identity preservation,
- multiple tool calls in one turn are easy to corrupt,
- current custom buffering logic became hard to trust.

### 3. Partial stream state became too important
Right now the app still depends on transient stream-built tool state.

Even when final flushed tool state exists, too much logic still depends on intermediate streamed assembly.

## Decision

We will replace the provider streaming/parsing implementation with **GoAI**.


## New Direction

### Core architecture

New target shape:

`ProviderSettings -> Engine (GoAI) -> our StreamEvent -> our app/tool execution`

In other words:

- keep our `ProviderSettings`
- keep our app layer
- keep our tool execution logic
- replace custom provider/adapters/stream parsing with GoAI

## What We Keep

### 1. Our tool execution loop
We are **not** handing tool execution to GoAI.

We still need our custom logic for:

- authorization prompts
- destructive tool approval
- preview behavior
- file validation / changed-file protection
- working directory handling
- skill/tool-specific behavior
- message/session persistence
- UI rendering and state

### 2. Our app stream layer
We still emit and consume our own `StreamEvent` model in the app.

GoAI will feed that layer, but not replace it.

### 3. Provider settings persistence
We keep our own saved provider configuration model (`ProviderSettings`).

## What We Remove / Simplify

### 1. Adapter layer goes away
Current adapter layer should be removed:

- `ChatCompletionsAdapter`
- `CodexAdapter`
- custom SSE parsing in adapter package

### 2. Most custom engine streaming complexity goes away
The engine should no longer do:

- provider-specific SSE parsing
- tool-call delta reconstruction
- `call_id -> idx` hacks
- partial tool arg accumulation
- flush reconciliation logic

### 3. Provider URL resolution likely goes away
If GoAI provider construction already handles endpoint/model/auth setup, we likely no longer need:

- `ResolveChatURL`
- dialect-specific URL switching in our provider layer
- manual Responses-vs-ChatCompletions transport logic

We may still need a thin compatibility layer for custom OpenAI-compatible endpoints if GoAI expects explicit base URLs.

## Stream Simplification Goal

### Old goal
We were assembling tool entries from messy partial/delta state.

### New goal
GoAI should give us cleaner chunk semantics.

We only want to capture a few things from GoAI and translate them into our own stream events:

- text
- thinking
- tool
- error
- done / maybe step-finish

### Important simplification
For tools, we want:

- tool call arrives as a complete object/chunk,
- build a pending tool entry directly,
- no more `partialTools` assembly from deltas,
- no more `stream tool -> partial -> final instruction entry` chain.

So instead of:

`messy event -> partial -> streaming tool -> instruction entry`

we want:

`GoAI chunk -> StreamEvent -> pending tool entry`

## Thinking / Qwen Consideration

One thing still needs attention:

- current Qwen handling includes hacks around `</think>` parsing to detect reasoning out of stream take
- GoAI may not preserve or expose that in the exact same way
- we may still need our own text-level think parsing in the stream layer

So:

- GoAI may simplify provider streaming,
- but we may still keep our own thinking parser for models like Qwen.

## Likely Clean Setup

### Provider construction
Keep a thin factory:

- input: `ProviderSettings`
- output: GoAI provider/model instance configured for that provider

So conceptually:

- `provider.GetProvider(settings)`
- engine uses that GoAI-backed provider/model

### Engine responsibility
The new engine should mostly:

- create/use GoAI model/provider from settings
- call GoAI stream/generation APIs
- translate GoAI events/chunks into our `StreamEvent`
- stop there

### App responsibility
The app should:

- consume our `StreamEvent`
- build tool entries directly from clean tool events
- run our own tool execution loop
- keep current authorization and persistence behavior

## Summary of Final Decision

### We are moving to:

`our settings -> engine [GoAI] -> our stream`

### We are not moving to:

- GoAI tool execution
- GoAI replacing our app/tool/session logic

### Main objective
Use GoAI to eliminate the unstable custom provider/adapter/stream parsing layer, while keeping our own higher-level tool orchestration and UI behavior.
