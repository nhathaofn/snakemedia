# 0002. Production Modular Architecture and Dynamic Full Scaffolding

- **Status:** Accepted
- **Date:** 2026-10-20
- **Spec:** —

## Context
The application skeleton needed to scale into a dynamic, production-ready desktop architecture. The codebase required separation between system infrastructure, business logic, global state management, IPC event communication, and multi-view routing.

## Decision
1. **Backend Layering (`internal/` & `pkg/`)**:
   - `internal/config`: Dynamic configuration loading and persistence with fallback defaults.
   - `internal/logger`: Structured logging using standard library `log/slog`.
   - `internal/events`: IPC event names and Wails runtime event dispatcher.
   - `internal/models`: Generic standardized API response envelope (`models.Response[T]`).
   - `internal/services`: Domain services (`SystemService`).
   - `pkg/utils`: Cross-cutting helpers (file utilities, byte formatters).

2. **Frontend Layering (`frontend/src/`)**:
   - `styles/`: CSS custom properties design tokens (`tokens.css`) and global reset (`globals.css`) supporting dynamic dark/light theme switching.
   - `stores/`: Global client state management using `zustand` (`useAppStore`).
   - `routes/`: Multi-screen navigation with `react-router-dom` using `createHashRouter` for desktop asset server compatibility.
   - `components/`: Clean decomposition into `ui/` (atoms), `layout/` (TitleBar, Sidebar, Header, AppLayout), and `feedback/` (ErrorBoundary, Toaster).
   - `features/`: Domain-oriented view modules (`home/`, `settings/`).
   - `services/`: API bridge wrapping Wails Go methods with error boundaries and browser preview fallbacks.
   - `hooks/`: Custom hooks (`useWailsEvent`, `useDebounce`).

3. **Continuous Integration**:
   - Automated testing and build validation workflow in `.github/workflows/ci.yml`.

## Consequences
- High maintainability: Adding new backend services or frontend features does not alter the core application shell.
- Production readiness: Built-in error boundary, structured logging, persistent configuration, and automated unit tests.
- Extensibility: Routing and navigation are dynamic and data-driven.
