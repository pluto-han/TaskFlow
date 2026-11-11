export type AccountLink = { kind: "verify" | "reset"; token: string };
// Call before React or any requests. Capabilities stay in memory; even invalid
// fragments are removed. Query-string capabilities are never accepted.
export function captureLink(location: Location, history: History) {
  const path = location.pathname;
  if (!["/invite", "/verify-email", "/reset-password"].includes(path))
    return {};
  const values = new URLSearchParams(location.hash.slice(1)).getAll("token");
  const token =
    values.length === 1 && /^[A-Za-z0-9_-]{43}$/.test(values[0])
      ? values[0]
      : "";
  history.replaceState(
    history.state,
    "",
    path + (path === "/invite" ? location.search : ""),
  );
  return path === "/invite"
    ? { invitationToken: token }
    : {
        accountLink: {
          kind: path === "/verify-email" ? "verify" : "reset",
          token,
        } as AccountLink,
      };
}
