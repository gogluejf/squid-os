# Message Building & Stacking Diagram

## Overview

This document provides a conceptual visualization of how messages are built and stacked at each chat cycle, including tokens, roles, and the message flow.

## Message Building Flow Diagram

```mermaid
flowchart TD
    subgraph User_Input
        A[User types message] --> B[User presses Enter]
    end

    subgraph Message_Creation
        B --> C[Create DisplayMessage with role=user]
        C --> D[Count input tokens]
        D --> E[Append to session.messages]
    end

    subgraph API_Preparation
        E --> F[BuildAPIMessages called]
        F --> G[Add system prompt as first message]
        G --> H[Convert DisplayMessage to ChatMessage]
        H --> I[Handle multimodal content if image attached]
    end

    subgraph Streaming_Cycle
        I --> J[Engine.Stream starts]
        J --> K[Receive StreamEvent chunks]
        K --> L[Accumulate text and thinking]
        L --> M[Count output tokens per chunk]
    end

    subgraph Message_Completion
        M --> N{Event.Done?}
        N -->|Yes| O[Create assistant DisplayMessage]
        N -->|No| K
        O --> P[Append assistant message to session]
        P --> Q[Save session to file]
    end

    subgraph Error_Handling
        K --> R{Event.Error?}
        R -->|Yes| S[Remove orphaned user message]
        R -->|No| L
        S --> T[Return to chat mode]
    end

    User_Input --> Message_Creation
    Message_Creation --> API_Preparation
    API_Preparation --> Streaming_Cycle
    Streaming_Cycle --> Message_Completion
```

## Message Stack Visualization (Cycle by Cycle)

```mermaid
quadrantChart
    title: Message Stack Growth Over Cycles
    x-axis: "Earlier in conversation" --> "Latest message"
    y-axis: "Lower stack position" --> "Top of stack"
    quadrant 1: "Recent messages"
    quadrant 2: "Oldest messages"
    quadrant 3: "Oldest messages"
    quadrant 4: "Recent messages"
    "Cycle 1 - System Prompt": [0.1, 0.2]
    "Cycle 1 - User Message": [0.3, 0.4]
    "Cycle 1 - Assistant Response": [0.5, 0.6]
    "Cycle 2 - User Message": [0.7, 0.5]
    "Cycle 2 - Assistant Response": [0.9, 0.7]
    "Cycle 3 - User Message": [0.85, 0.8]
```

## Message Structure Breakdown

```mermaid
classDiagram
    class DisplayMessage {
        +string ID
        +string Role
        +time.Time CreatedAt
        +string Text
        +string ThinkingText
        +string ImagePath
        +int InputTokens
        +int OutputTokens
        +float64 TokensPerSecond
        +int64 ResponseTimeMs
        +string StopReason
        +bool ThinkingExpanded
    }

    class ChatMessage {
        +string Role
        +interface{} Content
    }

    class StreamEvent {
        +string Text
        +string Thinking
        +bool InThinking
        +bool Done
        +string StopReason
        +error Error
    }

    DisplayMessage o-- ChatMessage : converts to
    ChatMessage o-- StreamEvent : receives from
```

## Token Flow Diagram

```mermaid
flowchart LR
    subgraph Input_Tokens
        A[User Message Text] --> B[countTokensApprox]
        B --> C[InputTokens field]
    end

    subgraph Output_Tokens
        D[StreamEvent chunks] --> E[Accumulate tokens]
        E --> F[OutputTokens field]
    end

    subgraph Token_Metrics
        C --> G[Total session tokens]
        F --> G
        G --> H[Tokens/Second calculation]
    end

    Input_Tokens --> Output_Tokens
    Output_Tokens --> Token_Metrics
```

## Role Assignment Flow

```mermaid
flowchart TD
    A[Message created] --> B{Determine Role}
    B -->|System prompt| C[Role = system]
    B -->|User input| D[Role = user]
    B -->|Assistant response| E[Role = assistant]
    
    C --> F[Add to message stack]
    D --> F
    E --> F
    
    F --> G[Stack order: system → user → assistant → user → assistant...]
```

## Complete Cycle Visualization

```mermaid
sequenceDiagram
    participant U as User
    participant App as App Layer
    participant Chat as Chat Engine
    participant API as LLM API

    U->>App: Type message + Enter
    App->>App: Create DisplayMessage (role=user)
    App->>App: Count input tokens
    App->>App: Append to session.messages
    App->>Chat: BuildAPIMessages()
    Chat->>Chat: Add system prompt
    Chat->>Chat: Convert messages to ChatMessage[]
    Chat->>API: POST /v1/chat/completions
    API-->>Chat: StreamEvent (text/thinking chunks)
    Chat-->>App: StreamEvent deltas
    App->>App: Accumulate text + thinking
    App->>App: Count output tokens
    API-->>Chat: StreamEvent{Done:true}
    Chat-->>App: Final StreamEvent
    App->>App: Create assistant DisplayMessage
    App->>App: Append to session.messages
    App->>App: Save session file
```

## Key Concepts

### Message Stack Order
1. **System Prompt** (always first, role=system)
2. **User Messages** (role=user, with InputTokens)
3. **Assistant Responses** (role=assistant, with OutputTokens)

### Token Tracking
- **InputTokens**: Counted when user message is created
- **OutputTokens**: Accumulated during streaming from StreamEvent chunks
- **TokensPerSecond**: Calculated as OutputTokens / (firstTokenTime - startTime)

### Thinking Content
- Separately tracked in `ThinkingText` field
- Parsed from `<think>` tags during streaming
- Can be expanded/collapsed in UI

### Error Handling
- On error: User message is removed (no orphaned messages)
- On cancel: Assistant message is not saved
- On success: Both user and assistant messages persist

## File Locations

- [`internal/chat/engine.go`](internal/chat/engine.go:276): `BuildAPIMessages()` function
- [`internal/app/stream.go`](internal/app/stream.go:59): `sendMessage()` and `handleStreamEvent()`
- [`internal/config/types.go`](internal/config/types.go:54): `Message` and `DisplayMessage` structs
- [`internal/app/chat_session.go`](internal/app/chat_session.go:36): `appendMsg()` for stacking
