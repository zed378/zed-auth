import { Link } from "react-router-dom";

export function NotFoundPage() {
  return (
    <div className="flex flex-col items-start gap-4">
      <h1 className="text-heading-1 font-bold text-text-primary">Page not found</h1>

      <p className="max-w-prose text-body text-text-secondary">
        There is no console screen at this address.
      </p>

      <Link to="/" className="text-body font-medium text-accent underline">
        Back to the overview
      </Link>
    </div>
  );
}
