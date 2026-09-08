import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/**
 * The last line of defence against a render error blanking the console.
 *
 * Without one, an exception in any component unmounts the whole React tree and
 * leaves a white page: no navigation, no way back, nothing to report. That is
 * a worse outcome than almost any error it might be catching.
 *
 * What it deliberately does NOT do is show the error to the user.
 * `PLAN/10-THREAT-MODEL.md` § Information Disclosure treats a stack trace
 * returned to a caller as a disclosure bug, and the same reasoning holds in a
 * browser: a React error message routinely carries prop values, and in this
 * console those props are user records, role keys and organization structure.
 * The message goes to the console log for the operator; the screen gets a
 * recovery path.
 *
 * A class component because that is still the only way to implement
 * `componentDidCatch` — there is no hook equivalent.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Reported to the browser console now. P0-17 does not wire a frontend
    // error reporter; when one arrives it hooks in here, and it must carry the
    // same redaction discipline as the backend logger (P0-09) — an error
    // reporting service is a third party, and a component's props may include
    // a token.
    console.error("console: unhandled render error", error, info.componentStack);
  }

  private handleReload = () => {
    window.location.reload();
  };

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <div
        role="alert"
        className="flex min-h-screen flex-col items-center justify-center gap-4 bg-bg-base p-6 text-center"
      >
        <h1 className="text-heading-1 font-bold text-text-primary">Something went wrong</h1>

        <p className="max-w-prose text-body text-text-secondary">
          The console hit an unexpected error and stopped rendering this screen. Nothing you
          were doing has been saved. Reloading usually clears it; if it keeps happening,
          the details are in your browser&rsquo;s developer console.
        </p>

        <button
          type="button"
          onClick={this.handleReload}
          className="
            min-h-11 rounded bg-accent px-4 py-2
            text-body font-medium text-bg-surface
            transition-colors
            hover:opacity-90
          "
        >
          Reload the console
        </button>
      </div>
    );
  }
}
