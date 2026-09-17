# 03 - React Auth Provider & Hooks Specification

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify React context provider (`<AuthProvider/>`) and custom hooks (`useAuth()`, `useUser()`).

## Category Mandate

Enables simple, declarative authentication integration in React applications.

## Key Topics To Specify

- `<AuthProvider domain="..." clientId="...">` wrapper component.
- Hook `const { user, isAuthenticated, isLoading, login, logout } = useAuth();`.
- Hook `const { hasPermission } = usePermissions();`.

## Reference Architecture & Specification

React Usage Example:
```tsx
function App() {
  const { isAuthenticated, user } = useAuth();
  if (!isAuthenticated) return <LoginButton />;
  return <h1>Welcome {user.name}</h1>;
}
```

## Acceptance Criteria

- [x] React AuthProvider props defined.
- [x] React custom hooks documented.

## Open Questions

None.

## Related Documents

- `docs/SDK/02-TYPESCRIPT-SDK.md`
