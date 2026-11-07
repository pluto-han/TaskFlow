// datetime-local has no timezone. Read/write calendar components in the user's
// local timezone; never put an ISO UTC substring straight into this control.
export function toLocalInput(iso: string | null): string {
  if (!iso) return "";
  const date = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}
export function toUTC(value: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  // Also reject non-existent local wall times during a DST spring-forward gap.
  if (
    !Number.isFinite(date.getTime()) ||
    date.getFullYear() < 1 ||
    date.getFullYear() > 9999 ||
    toLocalInput(date.toISOString()).slice(0, value.length) !== value
  )
    throw new Error(
      "Enter a valid local date and time. Times skipped by daylight saving are not available.",
    );
  return date.toISOString();
}
export const displayDate = (value: string) =>
  new Intl.DateTimeFormat("en-GB", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
