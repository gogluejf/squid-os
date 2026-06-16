# Code Conventions

## DDD Architecture
- **Adapt to the project's existing layering convention.** Place new code alongside similar existing code. DDD principles (dependencies point inward, single responsibility) apply regardless of directory names.

## SOLID Principles
- **Single Responsibility**: Each type does one thing well.
- **Open/Closed**: Extend behavior via composition, not modification.
- **Liskov Substitution**: Implementations are interchangeable via their interface.
- **Interface Segregation**: Small, focused interfaces.
- **Dependency Inversion**: Depend on abstractions, not concretions.

## Naming
- Describe intent: `UserByEmailRepository` not `UserRepo2`.
- Domain terms match the ubiquitous language.
- Acronyms fully spelled out (e.g. `httpClient` not `httpClient`).

## Structure
- **Single Responsibility per File**: Each file should contain one coherent concept or type. If a file exceeds ~300-400 lines, ask whether it's doing too much — split by responsibility, not by arbitrary line count.
- Tests co-located with source (`*_test.go` or equivalent).
- No circular dependencies between packages.
