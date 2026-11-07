export type FieldErrors = Partial<
  Record<"email" | "password" | "confirm", string>
>;

export function validate(
  email: string,
  password: string,
  confirm?: string,
): FieldErrors {
  const errors: FieldErrors = {};
  const normalized = email.trim();
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalized) || normalized.length > 254)
    errors.email = "Enter a valid email address.";
  if (confirm === undefined) {
    if (!password) errors.password = "Enter your password.";
  } else {
    const length = Array.from(password).length;
    if (length < 15 || length > 128 || !password.trim())
      errors.password = "Use 15–128 characters, with more than just spaces.";
    if (confirm !== password) errors.confirm = "The passwords do not match.";
  }
  if (new TextEncoder().encode(password).length > 512)
    errors.password = "This password is too long. Please use fewer characters.";
  return errors;
}
