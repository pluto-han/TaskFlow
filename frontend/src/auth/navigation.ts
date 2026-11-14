// Return targets contain navigation state only, never capabilities or credentials.
export function returnTarget(value: string | null): string {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    /[\\#\s]/.test(value)
  )
    return "/tasks";
  const url = new URL(value, "https://taskflow.invalid");
  if (
    url.origin !== "https://taskflow.invalid" ||
    !/^\/(?:tasks(?:\/(?:today|unassigned|recurring))?|projects\/[A-Za-z0-9_-]+\/(?:tasks|settings)|settings(?:\/(?:account|security|integrations))?|profile)$/.test(
      url.pathname,
    ) ||
    [...url.searchParams.keys()].some(
      (key) => !["completed", "priority", "plan_date", "task"].includes(key),
    )
  )
    return "/tasks";
  return url.pathname + url.search;
}

export function loginTarget(path: string): string {
  const target = returnTarget(path);
  return target === "/tasks"
    ? "/login"
    : `/login?returnTo=${encodeURIComponent(target)}`;
}
